package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"shengen/internal/store"
	"shengen/internal/tenant"
)

type ctxKey int

const (
	userCtxKey ctxKey = iota
	sessionCtxKey
)

func withUser(ctx context.Context, u *store.User) context.Context {
	return context.WithValue(ctx, userCtxKey, u)
}

func UserFromContext(ctx context.Context) (*store.User, bool) {
	u, ok := ctx.Value(userCtxKey).(*store.User)
	return u, ok
}

func withSession(ctx context.Context, s *store.Session) context.Context {
	return context.WithValue(ctx, sessionCtxKey, s)
}

func SessionFromContext(ctx context.Context) (*store.Session, bool) {
	s, ok := ctx.Value(sessionCtxKey).(*store.Session)
	return s, ok
}

// RequireAuth loads the session cookie, then the firm and the user that
// session belongs to, and puts all three in the request context; anything
// else redirects to /login.
//
// This is the only place a request's firm is decided, and it comes solely
// from the server-side session row — never from the URL, a header or a form
// field — so a signed-in user cannot reach another firm's data by editing a
// request. Every store query is then scoped by that firm's id.
func (svc *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := svc.SessionFromRequest(r)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		t, err := svc.Store.GetTenant(r.Context(), sess.TenantID)
		if err != nil {
			svc.Logout(r.Context(), w, r)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		// GetUser is itself scoped by firm, so a session whose user has
		// since moved or been removed is rejected rather than reattached.
		// Deactivation deletes the user's sessions; refusing an inactive
		// user here as well keeps that true whatever path created one.
		user, err := svc.Store.GetUser(r.Context(), t.ID, sess.UserID)
		if err == nil && !user.Active {
			err = store.ErrNotFound
		}
		if err != nil {
			svc.Logout(r.Context(), w, r)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		ctx := tenant.WithTenant(withUser(withSession(r.Context(), sess), user), t)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole rejects the request with 403 unless the authenticated user has
// one of the given roles. Must run after RequireAuth.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := UserFromContext(r.Context())
			if !ok || !allowed[u.Role] {
				http.Error(w, "nemate dozvolu za ovu akciju", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CSRFProtect rejects state-changing requests (POST/PUT/PATCH/DELETE) whose
// csrf_token field doesn't match the session's server-side secret. Must run
// after RequireAuth so a session is present in context.
func CSRFProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			// A body over the server's cap fails to parse; say so instead of
			// reporting it as a missing CSRF token.
			if tooLarge(parseForm(r)) {
				http.Error(w, "zahtev je prevelik", http.StatusRequestEntityTooLarge)
				return
			}
			sess, ok := SessionFromContext(r.Context())
			if !ok || !VerifyCSRF(r, sess) {
				http.Error(w, "nevažeći CSRF token", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// parseForm parses the body the way r.FormValue would, but keeps the error.
func parseForm(r *http.Request) error {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return r.ParseMultipartForm(32 << 20)
	}
	return r.ParseForm()
}

func tooLarge(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}
