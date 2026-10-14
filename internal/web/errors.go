package web

import (
	"log"
	"net/http"
)

type errorPageData struct {
	baseData
	Code    int
	Title   string
	Message string
}

// notFound answers with the branded "Ova stranica ne postoji" page instead
// of Go's English "404 page not found" (UX-21).
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusNotFound, "Ova stranica ne postoji",
		"Link je možda zastareo, ili vozač ili boravak više ne postoji.")
}

// serverError answers with the branded error page. The cause is logged by
// the caller (or already was); the page never shows it.
func (s *Server) serverError(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusInternalServerError, "Nešto nije u redu",
		"Došlo je do greške na serveru. Pokušajte ponovo za minut; ako se ponavlja, javite nam se.")
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, code int, title, msg string) {
	t, ok := s.templates["greska.html"]
	if !ok {
		http.Error(w, title, code)
		return
	}
	data := errorPageData{baseData: s.base(r, ""), Code: code, Title: title, Message: msg}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	if err := t.ExecuteTemplate(w, "layout.html", data); err != nil {
		log.Printf("render greska.html: %v", err)
	}
}
