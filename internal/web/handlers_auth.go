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
	// Err is shown next to the fields it is about, not only at the top.
	Err string
	// ResetEnabled: "Zaboravili ste lozinku?" leads to a working reset.
	ResetEnabled bool
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	// Someone still signed in who follows "Prijava" from dani90.rs goes
	// straight in. A session whose firm or user is gone is ended by
	// RequireAuth on /pregled, which sends the browser back here.
	if _, err := s.Auth.SessionFromRequest(r); err == nil {
		http.Redirect(w, r, "/pregled", http.StatusSeeOther)
		return
	}
	s.render(w, "login.html", loginPageData{baseData: s.base(r, "login"), ResetEnabled: s.MailReady})
}

func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	password := r.FormValue("password")

	// Both limits are taken before the password is checked: the IP limit
	// stops one address spraying a password over many accounts, the account
	// limit stops many addresses guessing one account.
	if !s.loginIPLimiter.Take(auth.ClientIP(r, s.TrustedProxy)) || !s.loginAccountLimiter.Take(email) {
		s.renderLoginError(w, r, email, "Previše pokušaja prijave. Pokušajte ponovo za nekoliko minuta.", http.StatusTooManyRequests)
		return
	}

	// The email alone decides the firm: everyone signs in at the same address.
	u, err := s.Store.GetUserByEmail(r.Context(), email)
	if errors.Is(err, store.ErrNotFound) {
		auth.SpendPasswordCheck(password)
		s.renderLoginError(w, r, email, "Pogrešan email ili lozinka.", http.StatusUnauthorized)
		return
	}
	if err != nil {
		s.serverError(w, r)
		return
	}

	if !auth.VerifyPassword(u.PasswordHash, password) {
		s.renderLoginError(w, r, email, "Pogrešan email ili lozinka.", http.StatusUnauthorized)
		return
	}
	// Said only after the right password, so it reveals nothing to a guesser.
	if !u.Active {
		s.renderLoginError(w, r, email, "Nalog je deaktiviran. Obratite se vlasniku naloga u firmi.", http.StatusForbidden)
		return
	}

	s.loginAccountLimiter.Reset(email)
	if _, err := s.Auth.Login(r.Context(), w, u.TenantID, u.ID); err != nil {
		s.serverError(w, r)
		return
	}
	http.Redirect(w, r, "/pregled", http.StatusSeeOther)
}

func (s *Server) renderLoginError(w http.ResponseWriter, r *http.Request, email, msg string, status int) {
	data := loginPageData{baseData: s.base(r, "login"), Email: email, Err: msg, ResetEnabled: s.MailReady}
	w.WriteHeader(status)
	s.render(w, "login.html", data)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.Auth.Logout(r.Context(), w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
