package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Turnstile checks Cloudflare's bot challenge on the public forms (password
// reset now, sign-up next). A nil *Turnstile on the server means no keys are
// configured and the forms work without it — local development.
type Turnstile struct {
	SiteKey string
	Secret  string
	// Hostname, when set, must match the one the challenge was solved on.
	Hostname string
	// Endpoint and Client are overridden in tests.
	Endpoint string
	Client   *http.Client
}

const turnstileEndpoint = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// turnstileFormField is the hidden field the widget fills in.
const turnstileFormField = "cf-turnstile-response"

// errTurnstile is any failed check: missing, expired, reused or forged.
var errTurnstile = errors.New("turnstile check failed")

type turnstileResult struct {
	Success    bool     `json:"success"`
	Hostname   string   `json:"hostname"`
	Action     string   `json:"action"`
	ErrorCodes []string `json:"error-codes"`
}

// Verify asks Cloudflare whether the token in the form is a fresh solve for
// this action. Cloudflare being unreachable is a failure too: the form is
// refused rather than left open.
func (t *Turnstile) Verify(ctx context.Context, token, remoteIP, action string) error {
	if token == "" {
		return errTurnstile
	}
	form := url.Values{"secret": {t.Secret}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	endpoint := t.Endpoint
	if endpoint == "" {
		endpoint = turnstileEndpoint
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := t.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", errTurnstile, err)
	}
	defer resp.Body.Close()
	var res turnstileResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&res); err != nil {
		return fmt.Errorf("%w: decode: %v", errTurnstile, err)
	}
	switch {
	case !res.Success:
		return fmt.Errorf("%w: %v", errTurnstile, res.ErrorCodes)
	case isTurnstileTestSecret(t.Secret):
		// Cloudflare's published test secrets answer without action or
		// hostname; they only accept tokens from the test site keys.
		return nil
	case res.Action != action:
		// A token solved on another form cannot be replayed here.
		return fmt.Errorf("%w: action %q, want %q", errTurnstile, res.Action, action)
	case t.Hostname != "" && res.Hostname != t.Hostname:
		return fmt.Errorf("%w: hostname %q", errTurnstile, res.Hostname)
	}
	return nil
}

// isTurnstileTestSecret recognises Cloudflare's dummy secrets
// (1x…AA always passes, 2x…AA always fails, 3x…AA reports a reused token).
func isTurnstileTestSecret(secret string) bool {
	return len(secret) == 35 && strings.HasSuffix(secret, "AA") &&
		strings.Trim(secret[2:33], "0") == "" && strings.Contains("123", secret[:1]) && secret[1] == 'x'
}

// turnstileCSP is the policy for pages that show the widget: the app's own
// policy plus Cloudflare's script and the frame it draws the challenge in.
var turnstileCSP = strings.Replace(
	strings.Replace(contentSecurityPolicy, "script-src 'self'", "script-src 'self' https://challenges.cloudflare.com", 1),
	"frame-ancestors", "frame-src https://challenges.cloudflare.com; frame-ancestors", 1)

// turnstileWidget is what a form template needs to draw the challenge.
type turnstileWidget struct {
	SiteKey string
	Action  string
}

// widget prepares the page for the challenge, or returns nil when Turnstile
// is off. It loosens the page's policy only when the widget is really shown.
func (s *Server) turnstileWidget(w http.ResponseWriter, action string) *turnstileWidget {
	if s.Turnstile == nil {
		return nil
	}
	w.Header().Set("Content-Security-Policy", turnstileCSP)
	return &turnstileWidget{SiteKey: s.Turnstile.SiteKey, Action: action}
}

// checkTurnstile verifies the form's challenge; with Turnstile off it passes.
func (s *Server) checkTurnstile(r *http.Request, action string) error {
	if s.Turnstile == nil {
		return nil
	}
	return s.Turnstile.Verify(r.Context(), r.FormValue(turnstileFormField), s.clientIP(r), action)
}
