package auth

import (
	"context"
	"net/http"
	"time"

	"shengen/internal/idgen"
	"shengen/internal/store"
)

const (
	SessionCookieName = "shengen_session"
	sessionDuration   = 14 * 24 * time.Hour
	// SessionIdleTimeout ends a session nobody has used for this long, even
	// within its 14 days: a browser left signed in on a shared office PC
	// does not stay usable for two weeks.
	SessionIdleTimeout = 24 * time.Hour
	// touchInterval limits last-seen writes to one per session per minute.
	touchInterval = time.Minute
)

type Service struct {
	Store           *store.Store
	InsecureCookies bool // true only for local http:// dev
}

func New(st *store.Store, insecureCookies bool) *Service {
	return &Service{Store: st, InsecureCookies: insecureCookies}
}

// Login creates a new session for userID on tenantID and sets the session
// cookie on the response.
func (svc *Service) Login(ctx context.Context, w http.ResponseWriter, tenantID, userID int64) error {
	token, err := idgen.RandomToken(32)
	if err != nil {
		return err
	}
	csrfSecret, err := idgen.RandomToken(32)
	if err != nil {
		return err
	}
	expires := time.Now().Add(sessionDuration)
	if err := svc.Store.CreateSession(ctx, token, tenantID, userID, csrfSecret, expires); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   !svc.InsecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (svc *Service) Logout(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookieName); err == nil {
		svc.Store.DeleteSession(ctx, c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   !svc.InsecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// SessionFromRequest reads the session cookie and loads the session row, if
// valid and not expired.
func (svc *Service) SessionFromRequest(r *http.Request) (*store.Session, error) {
	c, err := r.Cookie(SessionCookieName)
	if err != nil {
		return nil, store.ErrNotFound
	}
	sess, err := svc.Store.GetSession(r.Context(), c.Value)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if now.After(sess.ExpiresAt) || now.Sub(sess.LastSeenAt) > SessionIdleTimeout {
		svc.Store.DeleteSession(r.Context(), c.Value)
		return nil, store.ErrNotFound
	}
	if now.Sub(sess.LastSeenAt) > touchInterval {
		if err := svc.Store.TouchSession(r.Context(), c.Value, now); err != nil {
			return nil, err
		}
		sess.LastSeenAt = now
	}
	return sess, nil
}
