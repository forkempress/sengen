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
	// TermsPending: the firm has not accepted the current terms yet (shown
	// to dispatchers, who cannot accept them).
	TermsPending bool
	// FeedbackEmail is where signed-in users send remarks (MAIL_REPLY_TO);
	// "" hides the link.
	FeedbackEmail string
	// DocImport: scanned histories can be read (a Claude API key is set),
	// so pages offer "upload a scan" next to manual entry and CSV.
	DocImport bool
}

// base also takes the session's waiting flash message, so the page being
// rendered is the one that shows it.
func (s *Server) base(r *http.Request, active string) baseData {
	u, _ := auth.UserFromContext(r.Context())
	sess, _ := auth.SessionFromContext(r.Context())
	d := baseData{User: u, Active: active, TermsPending: termsPending(r.Context())}
	if u != nil {
		// Only to signed-in users: a public page would hand the address
		// to every scraper.
		d.FeedbackEmail = s.FeedbackEmail
		d.DocImport = s.DocReader != nil
	}
	if sess != nil {
		d.CSRFToken = sess.CSRFSecret
		ok, errMsg, err := s.Store.TakeSessionFlash(r.Context(), sess.Token)
		if err != nil {
			log.Printf("flash: %v", err)
		}
		d.Flash, d.FlashErr = ok, errMsg
	}
	return d
}

// redirectOk and redirectErr redirect after a form, leaving the message on
// the session. Messages never travel in the URL: a link someone sends cannot
// make the app show their text (SEC-16).
func (s *Server) redirectOk(w http.ResponseWriter, r *http.Request, path, msg string) {
	s.redirectFlash(w, r, path, msg, "")
}

func (s *Server) redirectErr(w http.ResponseWriter, r *http.Request, path, msg string) {
	s.redirectFlash(w, r, path, "", msg)
}

func (s *Server) redirectFlash(w http.ResponseWriter, r *http.Request, path, ok, errMsg string) {
	if sess, found := auth.SessionFromContext(r.Context()); found {
		if err := s.Store.SetSessionFlash(r.Context(), sess.Token, ok, errMsg); err != nil {
			log.Printf("flash: %v", err)
		}
	}
	http.Redirect(w, r, path, http.StatusSeeOther)
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
