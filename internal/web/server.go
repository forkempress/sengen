// Package web renders the server-side UI (html/template + htmx) and holds
// the HTTP handlers for every page and form action.
package web

import (
	"io/fs"
	"net/http"

	"shengen/internal/auth"
	"shengen/internal/store"
	"shengen/internal/telegram"
)

type Server struct {
	Store        *store.Store
	Auth         *auth.Service
	Bot          *telegram.Bot
	templates    templateSet
	loginLimiter *auth.LoginLimiter
}

func New(st *store.Store, authSvc *auth.Service, bot *telegram.Bot) *Server {
	return &Server{
		Store:        st,
		Auth:         authSvc,
		Bot:          bot,
		templates:    loadTemplates(),
		loginLimiter: auth.NewLoginLimiter(),
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLoginSubmit)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("POST /telegram/webhook", s.Bot.HandleWebhook)
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // embedded at build time; a failure here means the binary is broken
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticSub)))

	protected := http.NewServeMux()
	protected.HandleFunc("GET /{$}", s.handleRoot)
	protected.HandleFunc("GET /pregled", s.handlePregled)
	protected.HandleFunc("GET /pomoc", s.handleHelp)

	protected.HandleFunc("GET /vozaci/{id}", s.handleDriverDetail)
	protected.HandleFunc("POST /vozaci/{id}/vratio-se", s.handleDriverReturned)
	protected.HandleFunc("POST /vozaci/{id}/boravci", s.handleStayCreate)
	protected.HandleFunc("POST /vozaci/{id}/boravci/{stay_id}", s.handleStayUpdate)
	protected.HandleFunc("POST /vozaci/{id}/boravci/{stay_id}/obrisi", s.handleStayDelete)

	protected.HandleFunc("GET /podesavanja/vozaci", s.handleSettingsDriversPage)
	protected.HandleFunc("POST /podesavanja/vozaci", s.handleSettingsDriverCreate)
	protected.HandleFunc("POST /podesavanja/vozaci/{id}/deaktiviraj", s.handleSettingsDriverDeactivate)
	protected.HandleFunc("POST /podesavanja/vozaci/{id}/aktiviraj", s.handleSettingsDriverActivate)
	protected.HandleFunc("POST /podesavanja/vozaci/{id}/telegram-link", s.handleDriverTelegramLink)
	protected.HandleFunc("POST /podesavanja/telegram/poveži-nalog", s.handleTelegramLinkSelf)

	protected.Handle("GET /podesavanja/telegram", auth.RequireRole("owner")(http.HandlerFunc(s.handleTelegramSettingsPage)))
	protected.Handle("POST /podesavanja/telegram", auth.RequireRole("owner")(http.HandlerFunc(s.handleTelegramSettingsUpdate)))
	protected.Handle("POST /podesavanja/telegram/upozorenja-na-moj", auth.RequireRole("owner")(http.HandlerFunc(s.handleAlertsToMe)))

	protected.Handle("GET /podesavanja/korisnici", auth.RequireRole("owner")(http.HandlerFunc(s.handleUsersPage)))
	protected.Handle("POST /podesavanja/korisnici", auth.RequireRole("owner")(http.HandlerFunc(s.handleUserCreate)))

	protected.HandleFunc("GET /import", s.handleImportPage)
	protected.HandleFunc("POST /import/preview", s.handleImportPreview)
	protected.HandleFunc("POST /import/potvrdi", s.handleImportConfirm)

	mux.Handle("/", s.Auth.RequireAuth(auth.CSRFProtect(protected)))

	return mux
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/pregled", http.StatusSeeOther)
}
