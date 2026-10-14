package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"unicode/utf8"

	"shengen/internal/auth"
	"shengen/internal/store"
	"shengen/internal/tenant"
)

const minPasswordLen = 10

// bcrypt silently refuses anything longer than 72 bytes, so the limit is
// enforced here with a readable message instead of a server error.
const maxPasswordBytes = 72

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
	if len([]rune(password)) < minPasswordLen {
		return "", "", errors.New("Lozinka mora imati najmanje 10 znakova.")
	}
	if len(password) > maxPasswordBytes {
		return "", "", errors.New("Lozinka je predugačka (najviše 72 bajta).")
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
	s.renderUsersPage(w, r, usersPageData{baseData: baseData{
		FlashErr: r.URL.Query().Get("err"),
		Flash:    r.URL.Query().Get("ok"),
	}})
}

func (s *Server) renderUsersPage(w http.ResponseWriter, r *http.Request, data usersPageData) {
	t, _ := tenant.FromContext(r.Context())
	users, err := s.Store.ListUsers(r.Context(), t.ID)
	if err != nil {
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}
	flash, flashErr := data.Flash, data.FlashErr
	data.baseData = s.base(r, "korisnici")
	data.Flash, data.FlashErr = flash, flashErr
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
		http.Error(w, "greška servera", http.StatusInternalServerError)
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
		http.Error(w, "greška servera", http.StatusInternalServerError)
		return
	}
	s.Store.RecordAudit(r.Context(), t.ID, "user", &actor.ID, "user", u.ID, "create", nil, nil)

	msg := "Dodat korisnik " + u.Name + " (" + assignableRoles[role] + "). Javite mu email i lozinku — prijavljuje se na istoj adresi."
	http.Redirect(w, r, "/podesavanja/korisnici?ok="+url.QueryEscape(msg), http.StatusSeeOther)
}
