package auth

import (
	"context"
	"net/http"

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

// RequireAuth loads the session cookie, verifies it belongs to the current
// tenant (resolved by tenant.Middleware upstream), loads the user, and
// redirects to /login otherwise.
func (svc *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := svc.SessionFromRequest(r)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		t, ok := tenant.FromContext(r.Context())
		if !ok || sess.TenantID != t.ID {
			svc.Logout(r.Context(), w, r)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		user, err := svc.Store.GetUser(r.Context(), t.ID, sess.UserID)
		if err != nil {
			svc.Logout(r.Context(), w, r)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		ctx := withUser(withSession(r.Context(), sess), user)
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
			sess, ok := SessionFromContext(r.Context())
			if !ok || !VerifyCSRF(r, sess) {
				http.Error(w, "nevažeći CSRF token", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
