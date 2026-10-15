package web

import (
	"net/http"
	"strings"
	"time"

	"shengen/internal/docread"
)

// Request body caps. Forms are a few kilobytes; only the CSV import (the
// upload and the confirm form that echoes its rows) needs more.
const (
	maxBodyBytes       = 1 << 20
	maxImportBodyBytes = 2 << 20
	// A scanned PDF or photo: the file limit plus room for the form.
	maxDocumentBodyBytes = docread.MaxFileBytes + 64<<10
)

// limitBody caps every request body, so neither an upload nor a form field
// can make the server buffer or store an unbounded amount of data. Reading
// past the cap fails, which the handlers already treat as a bad form.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int64(maxBodyBytes)
		switch {
		case r.URL.Path == "/import/dokument":
			limit = maxDocumentBodyBytes
			// The upload is bigger and the model call slow: give this one
			// route longer than the server's 15 s read / 30 s write
			// timeouts. Set here, before the CSRF check reads the body.
			rc := http.NewResponseController(w)
			rc.SetReadDeadline(time.Now().Add(docReadDeadline))
			rc.SetWriteDeadline(time.Now().Add(docReadDeadline))
		case strings.HasPrefix(r.URL.Path, "/import/"):
			limit = maxImportBodyBytes
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		next.ServeHTTP(w, r)
	})
}

// contentSecurityPolicy allows only the app's own files: fonts, htmx and
// app.js are served from /static, so no page load reaches a third party.
// Inline scripts and <style> elements are refused; style attributes are
// allowed (style-src-attr) because templates set computed widths with them,
// and a style attribute cannot run script.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self'; style-src-attr 'unsafe-inline'; " +
	"font-src 'self'; img-src 'self' data:; connect-src 'self'; " +
	"form-action 'self'; frame-ancestors 'none'; base-uri 'self'; object-src 'none'"

// securityHeaders sets the headers every response carries. frame-ancestors
// (and X-Frame-Options for old browsers) stops another site from framing
// the app and tricking a signed-in user into clicking "Obriši".
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Frame-Options", "DENY")
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
