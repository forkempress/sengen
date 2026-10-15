package web

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"shengen/internal/auth"
	"shengen/internal/legal"
	"shengen/internal/store"
	"shengen/internal/tenant"
)

// --- public legal pages ---

func (s *Server) handlePrivacyPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, "privatnost.html", s.base(r, "privatnost"))
}

func (s *Server) handleTermsPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, "uslovi.html", s.base(r, "uslovi"))
}

func (s *Server) handleDPAPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, "obrada_podataka.html", s.base(r, "obrada-podataka"))
}

// --- accepting the terms ---

type termsPendingKey struct{}

// termsPending reports whether the firm has not yet accepted the current
// terms (set by requireTerms for users who are let through anyway).
func termsPending(ctx context.Context) bool {
	pending, _ := ctx.Value(termsPendingKey{}).(bool)
	return pending
}

// requireTerms sends the firm's owner to /prihvatanje until the firm has
// accepted the current version of the terms of use and the data processing
// agreement. Dispatchers cannot accept for the firm: they keep working and
// see a notice, so a change of terms never stops dispatch.
func (s *Server) requireTerms(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/prihvatanje" {
			next.ServeHTTP(w, r)
			return
		}
		t, _ := tenant.FromContext(r.Context())
		ts, err := s.Store.GetTermsStatus(r.Context(), t.ID)
		if err != nil {
			s.serverError(w, r)
			return
		}
		if ts.Version == legal.TermsVersion {
			next.ServeHTTP(w, r)
			return
		}
		if u, _ := auth.UserFromContext(r.Context()); u != nil && u.Role == "owner" {
			http.Redirect(w, r, "/prihvatanje", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), termsPendingKey{}, true)))
	})
}

type acceptData struct {
	baseData
	IsOwner  bool
	Accepted store.TermsStatus
	Current  bool
}

func (s *Server) handleAcceptPage(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	ts, err := s.Store.GetTermsStatus(r.Context(), t.ID)
	if err != nil {
		s.serverError(w, r)
		return
	}
	data := acceptData{baseData: s.base(r, ""), Accepted: ts, Current: ts.Version == legal.TermsVersion}
	data.IsOwner = data.User != nil && data.User.Role == "owner"
	s.render(w, "prihvatanje.html", data)
}

func (s *Server) handleAcceptSubmit(w http.ResponseWriter, r *http.Request) {
	t, _ := tenant.FromContext(r.Context())
	u, _ := auth.UserFromContext(r.Context())
	if r.FormValue("prihvatam") != "da" {
		s.redirectErr(w, r, "/prihvatanje", "Za nastavak označite da prihvatate uslove korišćenja i ugovor o obradi podataka.")
		return
	}
	if err := s.Store.AcceptTerms(r.Context(), t.ID, u.ID, legal.TermsVersion); err != nil {
		s.serverError(w, r)
		return
	}
	after, _ := json.Marshal(map[string]string{"verzija": legal.TermsVersion})
	afterStr := string(after)
	if err := s.Store.RecordAudit(r.Context(), t.ID, "user", &u.ID, "tenant", t.ID, "accept_terms", nil, &afterStr); err != nil {
		log.Printf("audit accept_terms: %v", err)
	}
	s.redirectOk(w, r, "/pregled", "Uslovi i ugovor o obradi podataka su prihvaćeni.")
}
