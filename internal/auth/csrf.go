package auth

import (
	"crypto/subtle"
	"net/http"

	"shengen/internal/store"
)

const CSRFFieldName = "csrf_token"

// VerifyCSRF implements the synchronizer-token pattern: each session carries
// a server-side secret (store.Session.CSRFSecret); every state-changing
// form must echo it back in a hidden field. A mismatch (or a missing field)
// rejects the request.
func VerifyCSRF(r *http.Request, sess *store.Session) bool {
	submitted := r.FormValue(CSRFFieldName)
	if submitted == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(submitted), []byte(sess.CSRFSecret)) == 1
}
