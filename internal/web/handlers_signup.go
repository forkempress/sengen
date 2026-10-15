package web

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	netmail "net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"shengen/internal/auth"
	"shengen/internal/clock"
	"shengen/internal/idgen"
	"shengen/internal/legal"
	"shengen/internal/mail"
	"shengen/internal/store"
)

// Sign-up: the form, then a 6-digit code (typed in the same tab) or a link
// (from the email) confirms the address; only then are the firm and its
// owner created, on a 30-day trial.
const (
	signupTokenTTL  = 24 * time.Hour
	signupValidText = "24 sata"
	trialDays       = 30

	// signupCookie ties the code to the browser that sent the form: a code
	// read off someone else's screen, or a guessed one, is useless elsewhere.
	signupCookie = "d90_signup"

	turnstileActionSignup = "signup"
)

// Sign-up limits, per hour. Every accepted form sends an email.
const (
	signupLimitWindow   = time.Hour
	signupLimitPerIP    = 5
	signupLimitPerEmail = 3
)

type signupPageData struct {
	baseData
	Firm, Name, Email string
	Err               string
	ErrField          string // "firm", "name", "email", "password", "terms"
	Turnstile         *turnstileWidget
	Unavailable       bool
}

type signupCodeData struct {
	baseData
	Email string
	Err   string
	// Link confirmation page: the sign-up behind the link and its token.
	Confirm *store.PendingSignup
	Token   string
	// Invalid: why the code or link cannot be used; Done: confirmed in
	// another browser, so the person signs in with their password.
	Invalid string
	Done    bool
}

// --- the form ---

func (s *Server) handleSignupPage(w http.ResponseWriter, r *http.Request) {
	s.renderSignup(w, r, signupPageData{}, http.StatusOK)
}

func (s *Server) renderSignup(w http.ResponseWriter, r *http.Request, data signupPageData, status int) {
	data.baseData = s.base(r, "")
	data.Unavailable = !s.MailReady
	if !data.Unavailable {
		data.Turnstile = s.turnstileWidget(w, turnstileActionSignup)
	}
	w.WriteHeader(status)
	s.render(w, "registracija.html", data)
}

// validateSignup checks the form and returns the cleaned values.
func validateSignup(firm, name, email, password string, accepted bool) (signupPageData, error) {
	d := signupPageData{Firm: strings.TrimSpace(firm), Name: strings.TrimSpace(name), Email: store.NormalizeEmail(email)}
	fail := func(field, msg string) (signupPageData, error) {
		d.ErrField = field
		return d, errors.New(msg)
	}
	switch {
	case d.Firm == "":
		return fail("firm", "Upišite naziv firme.")
	case utf8.RuneCountInString(d.Firm) > store.MaxNameLen:
		return fail("firm", fmt.Sprintf("Naziv firme je predugačak (najviše %d znakova).", store.MaxNameLen))
	case d.Name == "":
		return fail("name", "Upišite ime i prezime.")
	case utf8.RuneCountInString(d.Name) > store.MaxNameLen:
		return fail("name", fmt.Sprintf("Ime je predugačko (najviše %d znakova).", store.MaxNameLen))
	}
	if addr, err := netmail.ParseAddress(d.Email); err != nil || addr.Address != d.Email || len(d.Email) > store.MaxEmailLen {
		return fail("email", "Upišite ispravnu email adresu.")
	}
	if err := auth.CheckNewPassword(password, d.Email, d.Name, d.Firm); err != nil {
		return fail("password", err.Error())
	}
	if !accepted {
		return fail("terms", "Za registraciju označite da prihvatate uslove korišćenja i ugovor o obradi podataka.")
	}
	return d, nil
}

// handleSignupSubmit answers the same way whether or not the address already
// has an account; the owner of the address learns which by email. The same
// work (bcrypt, one message sent after the reply) is done either way, so
// timing does not tell a stranger either.
func (s *Server) handleSignupSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.MailReady {
		s.renderSignup(w, r, signupPageData{}, http.StatusServiceUnavailable)
		return
	}
	password := r.FormValue("password")
	data, err := validateSignup(r.FormValue("firm"), r.FormValue("name"), r.FormValue("email"), password, r.FormValue("terms") == "da")
	if err != nil {
		// A typo costs nothing: only forms that would send an email count.
		data.Err = err.Error()
		s.renderSignup(w, r, data, http.StatusUnprocessableEntity)
		return
	}
	if !s.signupIPLimiter.Take(s.clientIP(r)) {
		data.Err = "Previše registracija sa ove adrese. Pokušajte ponovo za sat vremena."
		s.renderSignup(w, r, data, http.StatusTooManyRequests)
		return
	}
	if err := s.checkTurnstile(r, turnstileActionSignup); err != nil {
		log.Printf("signup: %v", err)
		data.Err, data.ErrField = "Provera da niste robot nije uspela. Pokušajte ponovo.", ""
		s.renderSignup(w, r, data, http.StatusBadRequest)
		return
	}
	// After the challenge, so a stranger cannot use up an address's tries.
	if !s.signupEmailLimiter.Take(data.Email) {
		data.Err, data.ErrField = "Previše pokušaja za ovu adresu. Pokušajte ponovo za sat vremena.", ""
		s.renderSignup(w, r, data, http.StatusTooManyRequests)
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		s.serverError(w, r)
		return
	}
	browser, err := idgen.RandomToken(32)
	if err != nil {
		s.serverError(w, r)
		return
	}

	_, err = s.Store.GetUserByEmail(r.Context(), data.Email)
	switch {
	case err == nil:
		msg, rerr := mail.Render("signup_exists", data.Email, mail.SignupExistsData{
			LoginURL: s.link("/login"), ResetURL: s.link("/zaboravljena-lozinka"),
		})
		if rerr != nil {
			s.serverError(w, r)
			return
		}
		s.sendMailAsync(nil, msg)
	case errors.Is(err, store.ErrNotFound):
		if err := s.issueSignup(r, data, hash, browser); err != nil {
			s.serverError(w, r)
			return
		}
	default:
		s.serverError(w, r)
		return
	}

	// The cookie is set in both cases, so the reply looks the same.
	s.setSignupCookie(w, browser, signupTokenTTL)
	s.renderSignupCode(w, r, signupCodeData{Email: data.Email}, http.StatusOK)
}

func (s *Server) issueSignup(r *http.Request, data signupPageData, passwordHash, browser string) error {
	token, err := idgen.RandomToken(32)
	if err != nil {
		return err
	}
	code, err := sixDigitCode()
	if err != nil {
		return err
	}
	form := store.SignupForm{
		FirmName: data.Firm, Name: data.Name, PasswordHash: passwordHash,
		TermsVersion: legal.TermsVersion, TermsAcceptedAt: time.Now(),
	}
	if err := s.Store.CreateSignup(r.Context(), token, code, browser, data.Email, form, signupTokenTTL); err != nil {
		return err
	}
	msg, err := mail.Render("signup", data.Email, mail.SignupData{
		Name: data.Name, Firm: data.Firm, Code: code,
		URL: s.link("/registracija/potvrdi?t=" + url.QueryEscape(token)), ValidFor: signupValidText,
	})
	if err != nil {
		return err
	}
	s.sendMailAsync(nil, msg)
	return nil
}

func sixDigitCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func (s *Server) setSignupCookie(w http.ResponseWriter, value string, ttl time.Duration) {
	c := &http.Cookie{
		Name: signupCookie, Value: value, Path: "/registracija",
		HttpOnly: true, Secure: !s.Auth.InsecureCookies, SameSite: http.SameSiteLaxMode,
	}
	if ttl > 0 {
		c.MaxAge = int(ttl.Seconds())
	} else {
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
}

func signupBrowser(r *http.Request) string {
	if c, err := r.Cookie(signupCookie); err == nil {
		return c.Value
	}
	return ""
}

func (s *Server) renderSignupCode(w http.ResponseWriter, r *http.Request, data signupCodeData, status int) {
	tokenPageHeaders(w)
	data.baseData = s.base(r, "")
	w.WriteHeader(status)
	s.render(w, "registracija_potvrda.html", data)
}

// --- the code, typed in the same tab ---

func (s *Server) handleSignupCode(w http.ResponseWriter, r *http.Request) {
	email := store.NormalizeEmail(r.FormValue("email"))
	if !s.loginIPLimiter.Take(s.clientIP(r)) {
		s.renderSignupCode(w, r, signupCodeData{Email: email, Err: "Previše pokušaja. Pokušajte ponovo za nekoliko minuta."}, http.StatusTooManyRequests)
		return
	}
	code := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, r.FormValue("code"))
	p, err := s.Store.ConsumeSignupCode(r.Context(), signupBrowser(r), code)
	switch {
	case errors.Is(err, store.ErrWrongCode):
		s.renderSignupCode(w, r, signupCodeData{Email: email, Err: "Kod nije tačan. Proverite poslednji mejl od Dani90."}, http.StatusUnprocessableEntity)
		return
	case isTokenError(err):
		s.renderSignupCode(w, r, signupCodeData{Invalid: "Ovaj kod više ne važi: istekao je, upisan je pogrešno više puta, ili je stigao novi mejl. Registrujte se ponovo."}, http.StatusGone)
		return
	case err != nil:
		s.serverError(w, r)
		return
	}
	s.openFirm(w, r, p)
}

// --- the link from the email ---

func (s *Server) handleSignupLinkPage(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("t")
	p, err := s.Store.PeekSignupByLink(r.Context(), token)
	if err != nil {
		if !isTokenError(err) {
			s.serverError(w, r)
			return
		}
		s.renderSignupCode(w, r, signupCodeData{Invalid: signupLinkInvalid(err)}, http.StatusGone)
		return
	}
	s.renderSignupCode(w, r, signupCodeData{Confirm: p, Token: token}, http.StatusOK)
}

func signupLinkInvalid(err error) string {
	switch {
	case errors.Is(err, store.ErrTokenUsed):
		return "Ovaj link je već iskorišćen — nalog je potvrđen. Prijavite se."
	case errors.Is(err, store.ErrTokenExpired):
		return "Ovaj link je istekao (važi " + signupValidText + "). Registrujte se ponovo."
	default:
		return "Ovaj link ne važi. Ako ste se registrovali više puta, važi samo link iz poslednjeg mejla."
	}
}

func (s *Server) handleSignupLinkConfirm(w http.ResponseWriter, r *http.Request) {
	if !s.loginIPLimiter.Take(s.clientIP(r)) {
		s.renderSignupCode(w, r, signupCodeData{Invalid: "Previše pokušaja. Pokušajte ponovo za nekoliko minuta."}, http.StatusTooManyRequests)
		return
	}
	p, err := s.Store.ConsumeSignupLink(r.Context(), r.FormValue("t"), signupBrowser(r))
	if err != nil {
		if !isTokenError(err) {
			s.serverError(w, r)
			return
		}
		s.renderSignupCode(w, r, signupCodeData{Invalid: signupLinkInvalid(err)}, http.StatusGone)
		return
	}
	s.openFirm(w, r, p)
}

// openFirm creates the confirmed sign-up's firm and owner. In the browser
// that sent the form the owner is signed in; anywhere else the address is
// confirmed but the password is still needed, so whoever opens a link sent
// to someone else's mailbox does not get a session out of it.
func (s *Server) openFirm(w http.ResponseWriter, r *http.Request, p *store.PendingSignup) {
	t, u, err := s.Store.CreateFirmWithOwner(r.Context(), p.Email, p.Form, clock.Today().AddDate(0, 0, trialDays))
	if errors.Is(err, store.ErrEmailTaken) {
		s.renderSignupCode(w, r, signupCodeData{Invalid: store.ErrEmailTaken.Msg}, http.StatusConflict)
		return
	}
	if err != nil {
		s.serverError(w, r)
		return
	}
	after, _ := json.Marshal(map[string]string{"uslovi": p.Form.TermsVersion})
	afterStr := string(after)
	if err := s.Store.RecordAudit(r.Context(), t.ID, "user", &u.ID, "tenant", t.ID, "signup", nil, &afterStr); err != nil {
		log.Printf("audit signup: %v", err)
	}
	s.setSignupCookie(w, "", 0)

	if !p.FromBrowser {
		s.renderSignupCode(w, r, signupCodeData{Done: true, Email: u.Email}, http.StatusOK)
		return
	}
	token, err := s.Auth.Login(r.Context(), w, t.ID, u.ID)
	if err != nil {
		s.serverError(w, r)
		return
	}
	msg := fmt.Sprintf("Dobrodošli! Firma %s je otvorena, probni period traje %d dana. Prvi korak: dodajte vozača.", t.Name, trialDays)
	if err := s.Store.SetSessionFlash(r.Context(), token, msg, ""); err != nil {
		log.Printf("flash: %v", err)
	}
	http.Redirect(w, r, "/pregled", http.StatusSeeOther)
}
