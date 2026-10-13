package web

import (
	"log"
	"net/http"

	"shengen/internal/auth"
	"shengen/internal/store"
)

// baseData holds the fields every page template needs (nav highlight, user
// box, CSRF token for embedded forms). Page-specific data structs embed this
// so html/template's field promotion makes .User / .CSRFToken etc. reachable
// from layout.html regardless of which page is being rendered.
type baseData struct {
	User      *store.User
	CSRFToken string
	Active    string
	Flash     string
	FlashErr  string
}

func (s *Server) base(r *http.Request, active string) baseData {
	u, _ := auth.UserFromContext(r.Context())
	sess, _ := auth.SessionFromContext(r.Context())
	token := ""
	if sess != nil {
		token = sess.CSRFSecret
	}
	return baseData{User: u, CSRFToken: token, Active: active}
}

func (s *Server) render(w http.ResponseWriter, page string, data any) {
	t, ok := s.templates[page]
	if !ok {
		http.Error(w, "nepoznat template: "+page, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout.html", data); err != nil {
		log.Printf("render %s: %v", page, err)
	}
}
