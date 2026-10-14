package web

import "net/http"

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
