package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"unicode/utf8"

	"shengen/internal/auth"
	"shengen/internal/store"
	"shengen/internal/tenant"
)

var assignableRoles = map[string]string{
	"owner":      "vlasnik",
	"dispatcher": "dispečer",
}

// validateNewUser checks the "add user" form. It returns the cleaned values
// so the handler stores exactly what was validated.
func validateNewUser(name, email, password, role string) (cleanName, cleanEmail string, err error) {
	cleanName = strings.TrimSpace(name)
	cleanEmail = store.NormalizeEmail(email)

	if cleanName == "" {
		return "", "", errors.New("Ime je obavezno.")
	}
	if utf8.RuneCountInString(cleanName) > store.MaxNameLen {
		return "", "", fmt.Errorf("Ime je predugačko (najviše %d znakova).", store.MaxNameLen)
	}
	if len(cleanEmail) > store.MaxEmailLen {
		return "", "", errors.New("Email adresa nije ispravna.")
	}
	if addr, perr := mail.ParseAddress(cleanEmail); perr != nil || addr.Address != cleanEmail {
		return "", "", errors.New("Email adresa nije ispravna.")
	}
	if err := auth.CheckNewPassword(password, cleanEmail, cleanName); err != nil {
		return "", "", err
	}
	if _, ok := assignableRoles[role]; !ok {
		return "", "", errors.New("Nepoznata uloga.")
	}
	return cleanName, cleanEmail, nil
}

type usersPageData struct {
	baseData
	Users     []store.User
	RoleNames map[string]string
	// Form values are echoed back after a validation error so the owner
	// doesn't retype everything; the password never is.
	FormName  string
	FormEmail string
	FormRole  string
}

func (s *Server) handleUsersPage(w http.ResponseWriter, r *http.Request) {
	s.renderUsersPage(w, r, usersPageData{})
}

func (s *Server) renderUsersPage(w http.ResponseWriter, r *http.Request, data usersPageData) {
	t, _ := tenant.FromContext(r.Context())
	users, err := s.Store.ListUsers(r.Context(), t.ID)
	if err != nil {
		s.serverError(w, r)
		return
	}
	// A validation error from this request wins over a waiting flash.
	flashErr := data.FlashErr
	data.baseData = s.base(r, "korisnici")
	if flashErr != "" {
		data.FlashErr = flashErr
	}
	data.Users = users
	data.RoleNames = assignableRoles
	if data.FormRole == "" {
		data.FormRole = "dispatcher"
	}
	s.render(w, "podesavanja_korisnici.html", data)
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	actor, _ := auth.UserFromContext(r.Context())

	rawName, rawEmail, role := r.FormValue("name"), r.FormValue("email"), r.FormValue("role")
	retry := usersPageData{FormName: rawName, FormEmail: rawEmail, FormRole: role}

	name, email, err := validateNewUser(rawName, rawEmail, r.FormValue("password"), role)
	if err != nil {
		retry.FlashErr = err.Error()
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.renderUsersPage(w, r, retry)
		return
	}

	hash, err := auth.HashPassword(r.FormValue("password"))
	if err != nil {
		s.serverError(w, r)
		return
	}
	u, err := s.Store.CreateUser(r.Context(), t.ID, email, hash, role, name)
	var verr *store.ValidationError
	if errors.As(err, &verr) {
		retry.FlashErr = "Korisnik sa tim emailom već postoji."
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.renderUsersPage(w, r, retry)
		return
	}
	if err != nil {
		s.serverError(w, r)
		return
	}
	s.Store.RecordAudit(r.Context(), t.ID, "user", &actor.ID, "user", u.ID, "create", nil, nil)

	msg := "Dodat korisnik " + u.Name + " (" + assignableRoles[role] + "). Javite mu email i lozinku — prijavljuje se na istoj adresi."
	s.redirectOk(w, r, "/podesavanja/korisnici", msg)
}

const usersPath = "/podesavanja/korisnici"

// loadUserOr404 reads the {id} path value and loads that user from the
// signed-in firm only; another firm's user is "not found", exactly like a
// user that doesn't exist.
func (s *Server) loadUserOr404(w http.ResponseWriter, r *http.Request) (*store.User, bool) {
	t, _ := tenant.FromContext(r.Context())
	userID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return nil, false
	}
	u, err := s.Store.GetUser(r.Context(), t.ID, userID)
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r)
		return nil, false
	}
	if err != nil {
		s.serverError(w, r)
		return nil, false
	}
	return u, true
}

func (s *Server) handleUserDeactivate(w http.ResponseWriter, r *http.Request) {
	s.setUserActive(w, r, false)
}

func (s *Server) handleUserActivate(w http.ResponseWriter, r *http.Request) {
	s.setUserActive(w, r, true)
}

func (s *Server) setUserActive(w http.ResponseWriter, r *http.Request, active bool) {
	t, _ := tenant.FromContext(r.Context())
	actor, _ := auth.UserFromContext(r.Context())
	target, ok := s.loadUserOr404(w, r)
	if !ok {
		return
	}
	if !active && target.ID == actor.ID {
		s.redirectErr(w, r, usersPath, "Ne možete deaktivirati sopstveni nalog.")
		return
	}

	before, err := s.Store.SetUserActive(r.Context(), t.ID, target.ID, active)
	if s.userChangeFailed(w, r, err) {
		return
	}
	action, msg := "activate", "Korisnik "+target.Name+" je ponovo aktivan. Prijavljuje se sa istom lozinkom; Telegram povezuje ponovo."
	if !active {
		action, msg = "deactivate", "Korisnik "+target.Name+" je deaktiviran: odjavljen je svuda i Telegram više ne prima njegove komande."
	}
	s.auditUser(r, t.ID, actor.ID, target.ID, action,
		map[string]any{"active": before.Active, "telegram_linked": before.TelegramChatID != nil},
		map[string]any{"active": active, "telegram_linked": active && before.TelegramChatID != nil})
	s.redirectOk(w, r, usersPath, msg)
}

func (s *Server) handleUserRole(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	actor, _ := auth.UserFromContext(r.Context())
	target, ok := s.loadUserOr404(w, r)
	if !ok {
		return
	}
	role := r.FormValue("role")
	if _, known := assignableRoles[role]; !known {
		s.redirectErr(w, r, usersPath, "Nepoznata uloga.")
		return
	}
	if role == target.Role {
		s.redirectOk(w, r, usersPath, "Uloga nije promenjena.")
		return
	}

	before, err := s.Store.SetUserRole(r.Context(), t.ID, target.ID, role)
	if s.userChangeFailed(w, r, err) {
		return
	}
	s.auditUser(r, t.ID, actor.ID, target.ID, "role_change",
		map[string]any{"role": before.Role}, map[string]any{"role": role})

	// An owner who made himself a dispatcher can no longer open this page.
	if target.ID == actor.ID {
		s.redirectOk(w, r, "/pregled", "Vaša uloga je sada "+assignableRoles[role]+".")
		return
	}
	s.redirectOk(w, r, usersPath, "Korisnik "+target.Name+" je sada "+assignableRoles[role]+".")
}

// userChangeFailed reports a store error to the owner and returns true if
// there was one.
func (s *Server) userChangeFailed(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	var verr *store.ValidationError
	switch {
	case errors.As(err, &verr):
		s.redirectErr(w, r, usersPath, verr.Msg)
	case errors.Is(err, store.ErrNotFound):
		s.notFound(w, r)
	default:
		s.serverError(w, r)
	}
	return true
}

func (s *Server) auditUser(r *http.Request, tenantID, actorID, userID int64, action string, before, after map[string]any) {
	var beforeJSON, afterJSON *string
	if before != nil {
		b, _ := json.Marshal(before)
		str := string(b)
		beforeJSON = &str
	}
	if after != nil {
		b, _ := json.Marshal(after)
		str := string(b)
		afterJSON = &str
	}
	if err := s.Store.RecordAudit(r.Context(), tenantID, "user", &actorID, "user", userID, action, beforeJSON, afterJSON); err != nil {
		log.Printf("audit user %d %s: %v", userID, action, err)
	}
}
