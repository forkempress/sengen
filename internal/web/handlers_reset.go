package web

import (
	"errors"
	"log"
	"net/http"
	netmail "net/mail"
	"net/url"
	"time"

	"shengen/internal/auth"
	"shengen/internal/clock"
	"shengen/internal/idgen"
	"shengen/internal/mail"
	"shengen/internal/store"
)

// A reset link is short-lived: whoever gets hold of it (a forwarded email, a
// shared inbox) can take the account over.
const (
	resetTokenTTL  = time.Hour
	resetValidText = "1 sat"
)

// --- "Zaboravili ste lozinku?" ---

type forgotPageData struct {
	baseData
	Email     string
	Err       string
	Sent      bool
	Turnstile *turnstileWidget
	// Unavailable: the server cannot send email, so there is no form.
	Unavailable bool
}

const turnstileActionReset = "reset"

func (s *Server) handleForgotPage(w http.ResponseWriter, r *http.Request) {
	s.renderForgot(w, r, forgotPageData{}, http.StatusOK)
}

func (s *Server) renderForgot(w http.ResponseWriter, r *http.Request, data forgotPageData, status int) {
	data.baseData = s.base(r, "")
	data.Unavailable = !s.MailReady
	if !data.Sent && !data.Unavailable {
		data.Turnstile = s.turnstileWidget(w, turnstileActionReset)
	}
	w.WriteHeader(status)
	s.render(w, "zaboravljena_lozinka.html", data)
}

// handleForgotSubmit answers the same way whether or not the address has an
// account, and the email goes out after the reply, so neither the page nor
// its timing tells a stranger who uses Dani90.
func (s *Server) handleForgotSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.MailReady {
		s.renderForgot(w, r, forgotPageData{}, http.StatusServiceUnavailable)
		return
	}
	email := store.NormalizeEmail(r.FormValue("email"))
	retry := forgotPageData{Email: email}
	if addr, err := netmail.ParseAddress(email); err != nil || addr.Address != email || len(email) > store.MaxEmailLen {
		retry.Err = "Upišite ispravnu email adresu."
		s.renderForgot(w, r, retry, http.StatusUnprocessableEntity)
		return
	}
	tooMany := func() {
		retry.Err = "Previše zahteva. Pokušajte ponovo za sat vremena."
		s.renderForgot(w, r, retry, http.StatusTooManyRequests)
	}
	if !s.resetIPLimiter.Take(s.clientIP(r)) {
		tooMany()
		return
	}
	if err := s.checkTurnstile(r, turnstileActionReset); err != nil {
		log.Printf("reset: %v", err)
		retry.Err = "Provera da niste robot nije uspela. Pokušajte ponovo."
		s.renderForgot(w, r, retry, http.StatusBadRequest)
		return
	}
	// Counted only after the challenge: otherwise anyone could use up the
	// address's requests and keep its owner from resetting for an hour.
	if !s.resetEmailLimiter.Take(email) {
		tooMany()
		return
	}

	u, err := s.Store.GetUserByEmail(r.Context(), email)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// Nothing to send; the reply below is the same.
	case err != nil:
		s.serverError(w, r)
		return
	case u.Active:
		if err := s.issueResetLink(r, u); err != nil {
			s.serverError(w, r)
			return
		}
	}
	s.renderForgot(w, r, forgotPageData{Email: email, Sent: true}, http.StatusOK)
}

func (s *Server) issueResetLink(r *http.Request, u *store.User) error {
	token, err := idgen.RandomToken(32)
	if err != nil {
		return err
	}
	tenantID, userID := u.TenantID, u.ID
	if err := s.Store.CreateEmailToken(r.Context(), token, store.EmailToken{
		Purpose: store.EmailTokenReset, Email: u.Email, TenantID: &tenantID, UserID: &userID,
	}, resetTokenTTL); err != nil {
		return err
	}
	msg, err := mail.Render("reset", u.Email, mail.ResetData{
		Name: u.Name, URL: s.link("/nova-lozinka?t=" + url.QueryEscape(token)), ValidFor: resetValidText,
	})
	if err != nil {
		return err
	}
	s.sendMailAsync(&tenantID, msg)
	return nil
}

// --- the link from the email ---

type resetPageData struct {
	baseData
	Token string
	Email string
	Err   string
	// Invalid holds why the link cannot be used; the form is not shown.
	Invalid string
}

// tokenPageHeaders keep a page whose URL holds a token out of caches and
// out of the Referer of any link on it.
func tokenPageHeaders(w http.ResponseWriter) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
}

func invalidLinkText(err error) string {
	switch {
	case errors.Is(err, store.ErrTokenUsed):
		return "Ovaj link je već iskorišćen."
	case errors.Is(err, store.ErrTokenExpired):
		return "Ovaj link je istekao. Link za novu lozinku važi " + resetValidText + "."
	default:
		return "Ovaj link ne važi. Ako ste tražili novu lozinku više puta, važi samo link iz poslednjeg mejla."
	}
}

func (s *Server) renderReset(w http.ResponseWriter, r *http.Request, data resetPageData, status int) {
	tokenPageHeaders(w)
	data.baseData = s.base(r, "")
	w.WriteHeader(status)
	s.render(w, "nova_lozinka.html", data)
}

// loadResetTarget checks the link and the account it is for, without using
// the link up: a mail scanner opening it, or a typo in the new password,
// must not cost the user their link.
func (s *Server) loadResetTarget(r *http.Request, token string) (*store.User, *store.Tenant, error) {
	et, err := s.Store.PeekEmailToken(r.Context(), token, store.EmailTokenReset)
	if err != nil {
		return nil, nil, err
	}
	u, err := s.Store.GetUser(r.Context(), *et.TenantID, *et.UserID)
	if err != nil {
		return nil, nil, err
	}
	// Deactivated since the email was sent, or the address has changed:
	// the link no longer belongs to this account.
	if !u.Active || u.Email != et.Email {
		return nil, nil, store.ErrNotFound
	}
	t, err := s.Store.GetTenant(r.Context(), u.TenantID)
	if err != nil {
		return nil, nil, err
	}
	return u, t, nil
}

func (s *Server) handleResetPage(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("t")
	u, _, err := s.loadResetTarget(r, token)
	if err != nil {
		if !isTokenError(err) {
			s.serverError(w, r)
			return
		}
		s.renderReset(w, r, resetPageData{Invalid: invalidLinkText(err)}, http.StatusGone)
		return
	}
	s.renderReset(w, r, resetPageData{Token: token, Email: u.Email}, http.StatusOK)
}

func isTokenError(err error) bool {
	return errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrTokenUsed) || errors.Is(err, store.ErrTokenExpired)
}

func (s *Server) handleResetSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.loginIPLimiter.Take(s.clientIP(r)) {
		s.renderReset(w, r, resetPageData{Invalid: "Previše pokušaja. Pokušajte ponovo za nekoliko minuta."}, http.StatusTooManyRequests)
		return
	}
	token := r.FormValue("t")
	u, t, err := s.loadResetTarget(r, token)
	if err != nil {
		if !isTokenError(err) {
			s.serverError(w, r)
			return
		}
		s.renderReset(w, r, resetPageData{Invalid: invalidLinkText(err)}, http.StatusGone)
		return
	}

	password := r.FormValue("new_password")
	if err := validateNewPassword(password, r.FormValue("new_password_repeat"), u.Email, u.Name, t.Name); err != nil {
		s.renderReset(w, r, resetPageData{Token: token, Email: u.Email, Err: err.Error()}, http.StatusUnprocessableEntity)
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.serverError(w, r)
		return
	}
	// Only now is the link used up; of two tabs submitting it, one wins.
	if _, err := s.Store.ConsumeEmailToken(r.Context(), token, store.EmailTokenReset); err != nil {
		if !isTokenError(err) {
			s.serverError(w, r)
			return
		}
		s.renderReset(w, r, resetPageData{Invalid: invalidLinkText(err)}, http.StatusGone)
		return
	}
	// Ends every session and unlinks Telegram, as a password change does.
	if err := s.Store.SetUserPassword(r.Context(), t.ID, u.ID, hash); err != nil {
		s.serverError(w, r)
		return
	}
	s.auditUser(r, t.ID, u.ID, u.ID, "password_reset", nil,
		map[string]any{"sessions_revoked": true, "telegram_unlinked": u.TelegramChatID != nil})
	s.loginAccountLimiter.Reset(u.Email)

	if notice, err := mail.Render("reset_done", u.Email, mail.ResetDoneData{
		Name: u.Name, When: clock.Now().Format("02.01.2006. u 15:04"), ResetURL: s.link("/zaboravljena-lozinka"),
	}); err != nil {
		log.Printf("reset_done mail: %v", err)
	} else {
		tenantID := t.ID
		s.sendMailAsync(&tenantID, notice)
	}

	sessToken, err := s.Auth.Login(r.Context(), w, t.ID, u.ID)
	if err != nil {
		s.serverError(w, r)
		return
	}
	msg := "Nova lozinka je postavljena. Odjavljeni ste sa svih drugih uređaja."
	if u.TelegramChatID != nil {
		msg += " Telegram je otkačen; povežite ga ponovo u podešavanjima."
	}
	if err := s.Store.SetSessionFlash(r.Context(), sessToken, msg, ""); err != nil {
		log.Printf("flash: %v", err)
	}
	http.Redirect(w, r, "/pregled", http.StatusSeeOther)
}
