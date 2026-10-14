package web

import (
	"errors"
	"net/http"
	"strings"

	"shengen/internal/auth"
	"shengen/internal/store"
)

type loginPageData struct {
	baseData
	Email string
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, "login.html", loginPageData{baseData: s.base(r, "login")})
}

func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	password := r.FormValue("password")

	limitKey := auth.ClientIP(r) + "|" + email
	if !s.loginLimiter.Allow(limitKey) {
		s.renderLoginError(w, r, email, "Previše pokušaja prijave. Pokušajte ponovo za nekoliko minuta.")
		return
	}

	// The email alone decides the firm: everyone signs in at the same address.
	u, err := s.Store.GetUserByEmail(r.Context(), email)
	if errors.Is(err, store.ErrNotFound) {
		auth.SpendPasswordCheck(password)
		s.loginLimiter.RecordFailure(limitKey)
		s.renderLoginError(w, r, email, "Pogrešan email ili lozinka.")
		return
	}
	if err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}

	if !auth.VerifyPassword(u.PasswordHash, password) {
		s.loginLimiter.RecordFailure(limitKey)
		s.renderLoginError(w, r, email, "Pogrešan email ili lozinka.")
		return
	}

	s.loginLimiter.Reset(limitKey)
	if err := s.Auth.Login(r.Context(), w, u.TenantID, u.ID); err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/pregled", http.StatusSeeOther)
}

func (s *Server) renderLoginError(w http.ResponseWriter, r *http.Request, email, msg string) {
	data := loginPageData{baseData: s.base(r, "login"), Email: email}
	data.FlashErr = msg
	w.WriteHeader(http.StatusUnauthorized)
	s.render(w, "login.html", data)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.Auth.Logout(r.Context(), w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
