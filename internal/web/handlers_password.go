package web

import (
	"errors"
	"net/http"

	"shengen/internal/auth"
	"shengen/internal/tenant"
)

// validateNewPassword applies the same rule as the "add user" form.
func validateNewPassword(password, repeat string) error {
	if len([]rune(password)) < minPasswordLen {
		return errors.New("Nova lozinka mora imati najmanje 10 znakova.")
	}
	if len(password) > maxPasswordBytes {
		return errors.New("Nova lozinka je predugačka (najviše 72 bajta).")
	}
	if password != repeat {
		return errors.New("Ponovljena lozinka se ne poklapa sa novom.")
	}
	return nil
}

func (s *Server) handlePasswordPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, "podesavanja_lozinka.html", s.base(r, "lozinka"))
}

func (s *Server) renderPasswordError(w http.ResponseWriter, r *http.Request, msg string, status int) {
	data := s.base(r, "lozinka")
	data.FlashErr = msg
	w.WriteHeader(status)
	s.render(w, "podesavanja_lozinka.html", data)
}

// handlePasswordChange lets a signed-in user change his own password. It
// asks for the current one, so a browser left signed in is not enough to
// lock the owner out. Afterwards every session of the user ends and his
// Telegram is unlinked; this browser gets a fresh session.
func (s *Server) handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	u, _ := auth.UserFromContext(r.Context())

	// Guesses at the current password count against the same per-account
	// limit as the login form.
	if !s.loginAccountLimiter.Take(u.Email) {
		s.renderPasswordError(w, r, "Previše pokušaja. Pokušajte ponovo za nekoliko minuta.", http.StatusTooManyRequests)
		return
	}
	if !auth.VerifyPassword(u.PasswordHash, r.FormValue("current_password")) {
		s.renderPasswordError(w, r, "Trenutna lozinka nije tačna.", http.StatusUnprocessableEntity)
		return
	}
	s.loginAccountLimiter.Reset(u.Email)

	newPassword := r.FormValue("new_password")
	if err := validateNewPassword(newPassword, r.FormValue("new_password_repeat")); err != nil {
		s.renderPasswordError(w, r, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		s.serverError(w, r)
		return
	}
	if err := s.Store.SetUserPassword(r.Context(), t.ID, u.ID, hash); err != nil {
		s.serverError(w, r)
		return
	}
	s.auditUser(r, t.ID, u.ID, u.ID, "password_change", nil,
		map[string]any{"sessions_revoked": true, "telegram_unlinked": u.TelegramChatID != nil})

	token, err := s.Auth.Login(r.Context(), w, t.ID, u.ID)
	if err != nil {
		s.serverError(w, r)
		return
	}
	msg := "Lozinka je promenjena. Odjavljeni ste sa svih drugih uređaja."
	if u.TelegramChatID != nil {
		msg += " Telegram je otkačen; povežite ga ponovo u podešavanjima."
	}
	if err := s.Store.SetSessionFlash(r.Context(), token, msg, ""); err != nil {
		s.serverError(w, r)
		return
	}
	http.Redirect(w, r, "/pregled", http.StatusSeeOther)
}
