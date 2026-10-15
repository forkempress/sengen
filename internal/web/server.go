// Package web renders the server-side UI (html/template + htmx) and holds
// the HTTP handlers for every page and form action.
package web

import (
	"net/http"
	"sync"
	"time"

	"shengen/internal/auth"
	"shengen/internal/docread"
	"shengen/internal/legal"
	"shengen/internal/mail"
	"shengen/internal/store"
	"shengen/internal/telegram"
)

type Server struct {
	Store *store.Store
	Auth  *auth.Service
	Bot   *telegram.Bot
	// TrustedProxy decides which header carries the client address for the
	// login rate limit; the zero value trusts none.
	TrustedProxy auth.TrustedProxy

	// Mailer sends the app's email; the default only logs it.
	Mailer mail.Mailer
	// MailReady is false when nothing can be sent (a server without SMTP):
	// the pages that promise an email are then not offered at all, rather
	// than saying "we sent you a link" when nothing will arrive.
	MailReady bool
	// MailDailyLimit caps messages sent per 24 hours (see config).
	MailDailyLimit int
	// PublicURL starts every link the app puts in an email. It comes from
	// configuration, never from the request's Host header: otherwise anyone
	// could ask for a reset of someone else's account with their own host
	// in the link and collect the token when the victim clicks.
	PublicURL string
	// FeedbackEmail is shown to signed-in users as the place for remarks.
	FeedbackEmail string
	// Turnstile guards the public forms; nil turns the check off.
	Turnstile *Turnstile
	// DocReader reads scanned histories (PDF, photo) on the import page;
	// nil hides that option.
	DocReader docread.Reader

	templates           templateSet
	loginIPLimiter      *auth.LoginLimiter
	loginAccountLimiter *auth.LoginLimiter
	resetIPLimiter      *auth.LoginLimiter
	resetEmailLimiter   *auth.LoginLimiter
	signupIPLimiter     *auth.LoginLimiter
	signupEmailLimiter  *auth.LoginLimiter
	docReadLimiter      *auth.LoginLimiter

	// mailWG tracks messages still being sent in the background.
	mailWG sync.WaitGroup
}

// Login limits, per 15 minutes. The IP limit is looser because a firm's
// dispatchers may share one office address.
const (
	loginLimitWindow     = 15 * time.Minute
	loginLimitPerIP      = 20
	loginLimitPerAccount = 10
)

// Password reset requests, per hour: each one may send an email.
const (
	resetLimitWindow   = time.Hour
	resetLimitPerIP    = 5
	resetLimitPerEmail = 3
)

const defaultMailDailyLimit = 250

func New(st *store.Store, authSvc *auth.Service, bot *telegram.Bot) *Server {
	return &Server{
		Store:               st,
		Auth:                authSvc,
		Bot:                 bot,
		templates:           loadTemplates(),
		loginIPLimiter:      auth.NewLoginLimiter(loginLimitPerIP, loginLimitWindow),
		loginAccountLimiter: auth.NewLoginLimiter(loginLimitPerAccount, loginLimitWindow),
		resetIPLimiter:      auth.NewLoginLimiter(resetLimitPerIP, resetLimitWindow),
		resetEmailLimiter:   auth.NewLoginLimiter(resetLimitPerEmail, resetLimitWindow),
		signupIPLimiter:     auth.NewLoginLimiter(signupLimitPerIP, signupLimitWindow),
		signupEmailLimiter:  auth.NewLoginLimiter(signupLimitPerEmail, signupLimitWindow),
		docReadLimiter:      auth.NewLoginLimiter(docReadLimitPerTenant, docReadLimitWindow),
		Mailer:              mail.LogMailer{},
		MailReady:           true,
		MailDailyLimit:      defaultMailDailyLimit,
		PublicURL:           legal.PublicBaseURL,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /login", s.handleLoginPage)
	// There is no session yet to carry a CSRF token, so login relies on the
	// browser's Sec-Fetch-Site (or Origin): another site cannot sign a
	// visitor in to the attacker's account (SEC-21).
	mux.Handle("POST /login", http.NewCrossOriginProtection().Handler(http.HandlerFunc(s.handleLoginSubmit)))
	mux.HandleFunc("POST /logout", s.handleLogout)
	// Password reset: public, so, like login, guarded by the browser's
	// Sec-Fetch-Site instead of a session's CSRF token.
	mux.HandleFunc("GET /zaboravljena-lozinka", s.handleForgotPage)
	mux.Handle("POST /zaboravljena-lozinka", http.NewCrossOriginProtection().Handler(http.HandlerFunc(s.handleForgotSubmit)))
	// Sign-up, public like the reset.
	mux.HandleFunc("GET /registracija", s.handleSignupPage)
	mux.Handle("POST /registracija", http.NewCrossOriginProtection().Handler(http.HandlerFunc(s.handleSignupSubmit)))
	mux.Handle("POST /registracija/kod", http.NewCrossOriginProtection().Handler(http.HandlerFunc(s.handleSignupCode)))
	mux.HandleFunc("GET /registracija/potvrdi", s.handleSignupLinkPage)
	mux.Handle("POST /registracija/potvrdi", http.NewCrossOriginProtection().Handler(http.HandlerFunc(s.handleSignupLinkConfirm)))
	mux.HandleFunc("GET /nova-lozinka", s.handleResetPage)
	mux.Handle("POST /nova-lozinka", http.NewCrossOriginProtection().Handler(http.HandlerFunc(s.handleResetSubmit)))
	mux.HandleFunc("POST /telegram/webhook", s.Bot.HandleWebhook)
	mux.Handle("GET /static/", staticHandler())
	mux.HandleFunc("GET /privatnost", s.handlePrivacyPage)
	mux.HandleFunc("GET /uslovi", s.handleTermsPage)
	mux.HandleFunc("GET /obrada-podataka", s.handleDPAPage)

	protected := http.NewServeMux()
	protected.HandleFunc("GET /{$}", s.handleRoot)
	// Everything no other route claims gets the branded 404.
	protected.HandleFunc("/", s.notFound)
	protected.HandleFunc("GET /pregled", s.handlePregled)
	protected.HandleFunc("GET /pomoc", s.handleHelp)

	protected.HandleFunc("GET /vozaci/{id}", s.handleDriverDetail)
	protected.HandleFunc("POST /vozaci/{id}/vratio-se", s.handleDriverReturned)
	protected.HandleFunc("POST /vozaci/{id}/boravci", s.handleStayCreate)
	protected.HandleFunc("POST /vozaci/{id}/boravci/{stay_id}", s.handleStayUpdate)
	protected.HandleFunc("POST /vozaci/{id}/boravci/{stay_id}/obrisi", s.handleStayDelete)
	protected.HandleFunc("POST /vozaci/{id}/planovi", s.handlePlanCreate)
	protected.HandleFunc("POST /vozaci/{id}/planovi/{plan_id}", s.handlePlanUpdate)
	protected.HandleFunc("POST /vozaci/{id}/planovi/{plan_id}/obrisi", s.handlePlanDelete)
	protected.HandleFunc("GET /planer", s.handlePlanner)
	protected.HandleFunc("GET /prihvatanje", s.handleAcceptPage)
	protected.Handle("POST /prihvatanje", auth.RequireRole("owner")(http.HandlerFunc(s.handleAcceptSubmit)))

	protected.HandleFunc("GET /podesavanja/vozaci", s.handleSettingsDriversPage)
	protected.HandleFunc("POST /podesavanja/vozaci", s.handleSettingsDriverCreate)
	protected.HandleFunc("POST /podesavanja/vozaci/{id}/deaktiviraj", s.handleSettingsDriverDeactivate)
	protected.HandleFunc("POST /podesavanja/vozaci/{id}/aktiviraj", s.handleSettingsDriverActivate)
	protected.HandleFunc("POST /podesavanja/vozaci/{id}/telegram-link", s.handleDriverTelegramLink)
	protected.Handle("GET /vozaci/{id}/izvoz.csv", auth.RequireRole("owner")(http.HandlerFunc(s.handleDriverExport)))
	protected.Handle("POST /vozaci/{id}/obrisi-trajno", auth.RequireRole("owner")(http.HandlerFunc(s.handleDriverDeletePermanent)))
	protected.HandleFunc("POST /podesavanja/telegram/poveži-nalog", s.handleTelegramLinkSelf)

	protected.Handle("GET /podesavanja/telegram", auth.RequireRole("owner")(http.HandlerFunc(s.handleTelegramSettingsPage)))
	protected.Handle("POST /podesavanja/telegram/grupa", auth.RequireRole("owner")(http.HandlerFunc(s.handleAlertGroup)))
	protected.Handle("POST /podesavanja/telegram/pragovi", auth.RequireRole("owner")(http.HandlerFunc(s.handleThresholds)))
	protected.Handle("POST /podesavanja/telegram/upozorenja-na-moj", auth.RequireRole("owner")(http.HandlerFunc(s.handleAlertsToMe)))

	protected.Handle("GET /podesavanja/korisnici", auth.RequireRole("owner")(http.HandlerFunc(s.handleUsersPage)))
	protected.Handle("POST /podesavanja/korisnici", auth.RequireRole("owner")(http.HandlerFunc(s.handleUserCreate)))
	protected.Handle("POST /podesavanja/korisnici/{id}/deaktiviraj", auth.RequireRole("owner")(http.HandlerFunc(s.handleUserDeactivate)))
	protected.Handle("POST /podesavanja/korisnici/{id}/aktiviraj", auth.RequireRole("owner")(http.HandlerFunc(s.handleUserActivate)))
	protected.Handle("POST /podesavanja/korisnici/{id}/uloga", auth.RequireRole("owner")(http.HandlerFunc(s.handleUserRole)))

	protected.HandleFunc("GET /podesavanja/lozinka", s.handlePasswordPage)
	protected.HandleFunc("POST /podesavanja/lozinka", s.handlePasswordChange)

	protected.HandleFunc("GET /import", s.handleImportPage)
	protected.HandleFunc("GET /import/primer.csv", s.handleImportSample)
	protected.HandleFunc("POST /import/preview", s.handleImportPreview)
	protected.HandleFunc("POST /import/potvrdi", s.handleImportConfirm)
	protected.HandleFunc("POST /import/dokument", s.handleImportDocument)

	mux.Handle("/", s.Auth.RequireAuth(auth.CSRFProtect(s.requireTerms(protected))))

	return securityHeaders(limitBody(mux))
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/pregled", http.StatusSeeOther)
}
