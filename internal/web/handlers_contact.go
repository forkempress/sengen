package web

import (
	"net/http"
	"net/url"
	"strings"
)

// contactSubject and contactBody prefill the email a visitor of the public
// site starts with the "Pišite nam" button, so the first message already
// says who is asking.
const (
	contactSubject = "Dani90 — pitanje"
	contactBody    = "Firma:\r\nBroj vozača:\r\nTelefon (ako želite da vas pozovemo):\r\n\r\nPitanje:\r\n"
)

// handleContact sends the public site's email button to MAIL_REPLY_TO: the
// static site cannot read the server's settings, so it links here and the
// address lives in one place. Without the setting there is nowhere to send
// the visitor, so the address does not exist.
func (s *Server) handleContact(w http.ResponseWriter, r *http.Request) {
	if s.FeedbackEmail == "" {
		s.notFound(w, r)
		return
	}
	// mailto wants %20 for spaces, not the + of form encoding.
	q := "subject=" + url.QueryEscape(contactSubject) + "&body=" + url.QueryEscape(contactBody)
	q = strings.ReplaceAll(q, "+", "%20")
	http.Redirect(w, r, "mailto:"+s.FeedbackEmail+"?"+q, http.StatusFound)
}
