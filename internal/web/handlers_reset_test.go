package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"shengen/internal/auth"
	"shengen/internal/mail"
	"shengen/internal/store"
	"shengen/internal/telegram"
)

type resetApp struct {
	*testApp
	srv    *Server
	outbox *mail.Recorder
}

func newResetApp(t *testing.T) *resetApp {
	t.Helper()
	base := newTestApp(t)
	srv := New(base.st, auth.New(base.st, true), telegram.New(base.st, telegram.Config{}))
	outbox := &mail.Recorder{}
	srv.Mailer = outbox
	srv.PublicURL = "https://app.dani90.rs"
	app := &resetApp{testApp: &testApp{t: t, handler: srv.Routes(), st: base.st}, srv: srv, outbox: outbox}
	return app
}

// sent waits for background sends and returns everything sent so far.
func (a *resetApp) sent() []mail.Message {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.srv.WaitMail(ctx)
	return a.outbox.Sent()
}

func (a *resetApp) forgot(email string, extra ...string) *httptest.ResponseRecorder {
	form := url.Values{"email": {email}}
	for i := 0; i+1 < len(extra); i += 2 {
		form.Set(extra[i], extra[i+1])
	}
	return a.request(http.MethodPost, "/zaboravljena-lozinka", form, nil)
}

var resetLinkRe = regexp.MustCompile(`https://app\.dani90\.rs/nova-lozinka\?t=([A-Za-z0-9_-]+)`)

func resetTokenFrom(t *testing.T, m mail.Message) string {
	t.Helper()
	match := resetLinkRe.FindStringSubmatch(m.Text)
	if match == nil {
		t.Fatalf("no reset link in mail:\n%s", m.Text)
	}
	return match[1]
}

func setNewPassword(a *resetApp, token, password string) *httptest.ResponseRecorder {
	return a.request(http.MethodPost, "/nova-lozinka", url.Values{
		"t": {token}, "new_password": {password}, "new_password_repeat": {password},
	}, nil)
}

func TestReset_FullFlow(t *testing.T) {
	a := newResetApp(t)
	f := a.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	other := a.addFirm("Druga Firma", "druga", "vlasnik@druga.rs", "Bora", 30)
	oldSession := a.mustLogin(f)

	w := a.forgot("Vlasnik@Prva.rs")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Ako postoji nalog") {
		t.Fatalf("request: %d\n%s", w.Code, w.Body.String())
	}
	msgs := a.sent()
	if len(msgs) != 1 || msgs[0].To != "vlasnik@prva.rs" || msgs[0].Tag != "reset" {
		t.Fatalf("outbox = %+v", msgs)
	}
	token := resetTokenFrom(t, msgs[0])

	page := a.request(http.MethodGet, "/nova-lozinka?t="+token, nil, nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `name="new_password"`) {
		t.Fatalf("link page: %d\n%s", page.Code, page.Body.String())
	}
	if got := page.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy on a page with a token = %q", got)
	}

	const newPassword = "konj-trava-sto-9"
	w = setNewPassword(a, token, newPassword)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/pregled" {
		t.Fatalf("set password: %d %s\n%s", w.Code, w.Header().Get("Location"), w.Body.String())
	}
	var fresh *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == auth.SessionCookieName && c.Value != "" {
			fresh = c
		}
	}
	if fresh == nil {
		t.Fatal("no session after setting the password")
	}
	if got := (&session{app: a.testApp, cookie: fresh}).get("/pregled"); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "Nova lozinka je postavljena") {
		t.Errorf("first page after reset: %d, flash missing", got.Code)
	}

	// The browser signed in with the old password is signed out.
	if got := oldSession.get("/pregled"); got.Code != http.StatusSeeOther {
		t.Errorf("old session still works after reset: %d", got.Code)
	}
	if s, _ := a.login(f.email, f.password); s != nil {
		t.Error("old password still signs in")
	}
	if s, _ := a.login(f.email, newPassword); s == nil {
		t.Error("new password does not sign in")
	}
	// The other firm's owner is untouched.
	a.mustLogin(other)

	// One link, one use.
	if w := setNewPassword(a, token, "jos-jedna-nova-9"); w.Code != http.StatusGone || !strings.Contains(w.Body.String(), "već iskorišćen") {
		t.Errorf("second use: %d\n%s", w.Code, w.Body.String())
	}
	if w := a.request(http.MethodGet, "/nova-lozinka?t="+token, nil, nil); w.Code != http.StatusGone {
		t.Errorf("opening a used link: %d", w.Code)
	}

	msgs = a.sent()
	if len(msgs) != 2 || msgs[1].Tag != "reset_done" || msgs[1].To != "vlasnik@prva.rs" {
		t.Errorf("after reset the owner should get a notice; outbox = %+v", msgs)
	}
}

// Whether an address has an account, has none or is deactivated, the page
// says the same and only an active account gets an email.
func TestReset_NoEnumeration(t *testing.T) {
	a := newResetApp(t)
	f := a.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	ctx := context.Background()
	disp, err := a.st.CreateUser(ctx, f.tenant.ID, "bivsi@prva.rs", "x", "dispatcher", "Bivši")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.st.SetUserActive(ctx, f.tenant.ID, disp.ID, false); err != nil {
		t.Fatal(err)
	}

	normalise := func(body, email string) string { return strings.ReplaceAll(body, email, "EMAIL") }
	var bodies []string
	for _, email := range []string{"vlasnik@prva.rs", "nepoznat@nigde.rs", "bivsi@prva.rs"} {
		w := a.forgot(email)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d", email, w.Code)
		}
		bodies = append(bodies, normalise(w.Body.String(), email))
	}
	if bodies[0] != bodies[1] || bodies[0] != bodies[2] {
		t.Error("the reply differs between an existing, an unknown and a deactivated address")
	}
	msgs := a.sent()
	if len(msgs) != 1 || msgs[0].To != "vlasnik@prva.rs" {
		t.Errorf("outbox = %+v, want one mail to the active owner only", msgs)
	}
}

func TestReset_ExpiredAndForeignLinks(t *testing.T) {
	a := newResetApp(t)
	f := a.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	ctx := context.Background()
	owner, _ := a.st.GetUserByEmail(ctx, f.email)
	tenantID, userID := f.tenant.ID, owner.ID
	if err := a.st.CreateEmailToken(ctx, "star-link", store.EmailToken{
		Purpose: store.EmailTokenReset, Email: f.email, TenantID: &tenantID, UserID: &userID,
	}, -time.Minute); err != nil {
		t.Fatal(err)
	}
	if w := a.request(http.MethodGet, "/nova-lozinka?t=star-link", nil, nil); w.Code != http.StatusGone || !strings.Contains(w.Body.String(), "istekao") {
		t.Errorf("expired link: %d", w.Code)
	}
	if w := setNewPassword(a, "star-link", "konj-trava-sto-9"); w.Code != http.StatusGone {
		t.Errorf("expired link accepted a password: %d", w.Code)
	}
	if w := setNewPassword(a, "izmisljen", "konj-trava-sto-9"); w.Code != http.StatusGone {
		t.Errorf("made-up token: %d", w.Code)
	}
	// A token issued for another purpose is not a reset link.
	if err := a.st.CreateEmailToken(ctx, "poziv", store.EmailToken{
		Purpose: store.EmailTokenInvite, Email: f.email, TenantID: &tenantID, UserID: &userID,
	}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if w := setNewPassword(a, "poziv", "konj-trava-sto-9"); w.Code != http.StatusGone {
		t.Errorf("invite token used as reset: %d", w.Code)
	}
	if s, _ := a.login(f.email, f.password); s == nil {
		t.Error("password changed by an invalid link")
	}
}

// A link that was emailed and then the account deactivated no longer works.
func TestReset_DeactivatedAfterMail(t *testing.T) {
	a := newResetApp(t)
	f := a.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	ctx := context.Background()
	disp, _ := a.st.CreateUser(ctx, f.tenant.ID, "disp@prva.rs", "x", "dispatcher", "Disp")
	a.forgot("disp@prva.rs")
	msgs := a.sent()
	if len(msgs) != 1 {
		t.Fatalf("outbox = %+v", msgs)
	}
	a.st.SetUserActive(ctx, f.tenant.ID, disp.ID, false)
	if w := setNewPassword(a, resetTokenFrom(t, msgs[0]), "konj-trava-sto-9"); w.Code != http.StatusGone {
		t.Errorf("deactivated user reset his password: %d", w.Code)
	}
}

// A rejected password does not use the link up.
func TestReset_WeakPasswordKeepsLink(t *testing.T) {
	a := newResetApp(t)
	f := a.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	a.forgot(f.email)
	token := resetTokenFrom(t, a.sent()[0])
	for _, weak := range []string{"kratka", "1234567890", "vlasnik-2026"} {
		if w := setNewPassword(a, token, weak); w.Code != http.StatusUnprocessableEntity {
			t.Errorf("%q: status %d, want 422", weak, w.Code)
		}
	}
	mismatch := a.request(http.MethodPost, "/nova-lozinka", url.Values{
		"t": {token}, "new_password": {"konj-trava-sto-9"}, "new_password_repeat": {"konj-trava-sto-8"},
	}, nil)
	if mismatch.Code != http.StatusUnprocessableEntity {
		t.Errorf("mismatch: %d", mismatch.Code)
	}
	if w := setNewPassword(a, token, "konj-trava-sto-9"); w.Code != http.StatusSeeOther {
		t.Errorf("good password after rejected ones: %d", w.Code)
	}
}

// Password reset poisoning: the link must not follow the Host header.
func TestReset_LinkIgnoresHostHeader(t *testing.T) {
	a := newResetApp(t)
	f := a.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	r := httptest.NewRequest(http.MethodPost, "/zaboravljena-lozinka", strings.NewReader(url.Values{"email": {f.email}}.Encode()))
	r.Host = "napadac.example"
	r.Header.Set("X-Forwarded-Host", "napadac.example")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	a.handler.ServeHTTP(httptest.NewRecorder(), r)
	msgs := a.sent()
	if len(msgs) != 1 {
		t.Fatalf("outbox = %+v", msgs)
	}
	if strings.Contains(msgs[0].Text+msgs[0].HTML, "napadac") || !resetLinkRe.MatchString(msgs[0].Text) {
		t.Errorf("link built from the request host:\n%s", msgs[0].Text)
	}
}

func TestReset_RateLimits(t *testing.T) {
	a := newResetApp(t)
	f := a.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	for i := 0; i < resetLimitPerEmail; i++ {
		if w := a.forgot(f.email); w.Code != http.StatusOK {
			t.Fatalf("request %d: %d", i+1, w.Code)
		}
	}
	if w := a.forgot(f.email); w.Code != http.StatusTooManyRequests {
		t.Errorf("request over the per-address limit: %d", w.Code)
	}
	if n := len(a.sent()); n != resetLimitPerEmail {
		t.Errorf("%d mails, want %d", n, resetLimitPerEmail)
	}

	// Per IP: one address asking for many accounts.
	b := newResetApp(t)
	limited := false
	for i := 0; i < resetLimitPerIP+2; i++ {
		if w := b.forgot("osoba" + string(rune('a'+i)) + "@firma.rs"); w.Code == http.StatusTooManyRequests {
			limited = true
			if i < resetLimitPerIP {
				t.Errorf("limited after %d requests, want %d", i, resetLimitPerIP)
			}
			break
		}
	}
	if !limited {
		t.Error("one IP was never limited")
	}
}

func TestReset_DailyMailLimit(t *testing.T) {
	a := newResetApp(t)
	a.srv.MailDailyLimit = 1
	a.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	a.addFirm("Druga Firma", "druga", "vlasnik@druga.rs", "Bora", 30)
	a.forgot("vlasnik@prva.rs")
	a.sent()
	a.forgot("vlasnik@druga.rs")
	if msgs := a.sent(); len(msgs) != 1 {
		t.Errorf("%d mails sent with a daily limit of 1", len(msgs))
	}
	var overLimit int
	a.st.DB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM email_log WHERE status = 'over_limit'`).Scan(&overLimit)
	if overLimit != 1 {
		t.Errorf("over_limit rows = %d, want 1", overLimit)
	}
}

// Like login, the public form cannot be posted from another site.
func TestReset_CrossOriginRefused(t *testing.T) {
	a := newResetApp(t)
	f := a.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	r := httptest.NewRequest(http.MethodPost, "/zaboravljena-lozinka", strings.NewReader(url.Values{"email": {f.email}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	a.handler.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("cross-site post: %d, want 403", w.Code)
	}
	if len(a.sent()) != 0 {
		t.Error("cross-site post sent a mail")
	}
}

func TestReset_Turnstile(t *testing.T) {
	var reply turnstileResult
	var gotSecret, gotToken string
	cf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		gotSecret, gotToken = r.FormValue("secret"), r.FormValue("response")
		json.NewEncoder(w).Encode(reply)
	}))
	defer cf.Close()

	a := newResetApp(t)
	a.srv.Turnstile = &Turnstile{SiteKey: "site-key", Secret: "tajni-kljuc", Hostname: "app.dani90.rs", Endpoint: cf.URL, Client: cf.Client()}
	f := a.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	// Five tries from one address and for one email: lift the limits.
	a.srv.resetIPLimiter = auth.NewLoginLimiter(100, time.Hour)

	page := a.request(http.MethodGet, "/zaboravljena-lozinka", nil, nil)
	if !strings.Contains(page.Body.String(), `data-sitekey="site-key"`) || !strings.Contains(page.Header().Get("Content-Security-Policy"), "frame-src https://challenges.cloudflare.com") {
		t.Errorf("widget or CSP missing on the form page")
	}
	if strings.Contains(a.request(http.MethodGet, "/login", nil, nil).Header().Get("Content-Security-Policy"), "challenges.cloudflare.com") {
		t.Error("CSP loosened on a page without the widget")
	}

	cases := []struct {
		name   string
		token  string
		reply  turnstileResult
		wantOK bool
	}{
		{"no token", "", turnstileResult{Success: true, Action: "reset", Hostname: "app.dani90.rs"}, false},
		{"refused by Cloudflare", "t1", turnstileResult{Success: false, ErrorCodes: []string{"invalid-input-response"}}, false},
		{"solved on another form", "t2", turnstileResult{Success: true, Action: "signup", Hostname: "app.dani90.rs"}, false},
		{"solved on another site", "t3", turnstileResult{Success: true, Action: "reset", Hostname: "drugi.example"}, false},
		{"valid", "t4", turnstileResult{Success: true, Action: "reset", Hostname: "app.dani90.rs"}, true},
	}
	for _, tc := range cases {
		reply = tc.reply
		before := len(a.sent())
		w := a.forgot(f.email, turnstileFormField, tc.token)
		sent := len(a.sent()) - before
		if tc.wantOK != (w.Code == http.StatusOK && sent == 1) {
			t.Errorf("%s: status %d, %d mails", tc.name, w.Code, sent)
		}
	}
	if gotSecret != "tajni-kljuc" || gotToken != "t4" {
		t.Errorf("siteverify got secret %q token %q", gotSecret, gotToken)
	}
}

func TestTurnstileTestSecrets(t *testing.T) {
	for secret, want := range map[string]bool{
		"1x0000000000000000000000000000000AA": true,
		"2x0000000000000000000000000000000AA": true,
		"0x4AAAAAAAB-pravi-tajni-kljuc-12345": false,
		"1x0000000000000000000000000000001AA": false,
	} {
		if got := isTurnstileTestSecret(secret); got != want {
			t.Errorf("isTurnstileTestSecret(%q) = %v", secret, got)
		}
	}
}

// A server that cannot send email does not offer a reset it cannot deliver.
func TestReset_OffWithoutMail(t *testing.T) {
	a := newResetApp(t)
	a.srv.MailReady = false
	f := a.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	if strings.Contains(a.request(http.MethodGet, "/login", nil, nil).Body.String(), `href="/zaboravljena-lozinka"`) {
		t.Error("login links to a reset that cannot send email")
	}
	if w := a.request(http.MethodGet, "/zaboravljena-lozinka", nil, nil); strings.Contains(w.Body.String(), `name="email"`) {
		t.Error("reset form shown without email")
	}
	w := a.forgot(f.email)
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "poslali smo") {
		t.Errorf("post without email: %d", w.Code)
	}
	var tokens int
	a.st.DB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM email_tokens`).Scan(&tokens)
	if tokens != 0 || len(a.sent()) != 0 {
		t.Errorf("%d tokens issued, %d mails", tokens, len(a.sent()))
	}
}

// Failed challenges do not count against the address, so a stranger cannot
// lock its owner out of resetting.
func TestReset_FailedChallengesDoNotUseUpTheAddress(t *testing.T) {
	ok := false
	cf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(turnstileResult{Success: ok, Action: "reset"})
	}))
	defer cf.Close()
	a := newResetApp(t)
	a.srv.Turnstile = &Turnstile{SiteKey: "k", Secret: "s", Endpoint: cf.URL, Client: cf.Client()}
	a.srv.resetIPLimiter = auth.NewLoginLimiter(100, time.Hour)
	f := a.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	for i := 0; i < resetLimitPerEmail+3; i++ {
		a.forgot(f.email, turnstileFormField, "lose")
	}
	ok = true
	if w := a.forgot(f.email, turnstileFormField, "dobro"); w.Code != http.StatusOK || len(a.sent()) != 1 {
		t.Errorf("owner locked out after failed challenges: %d", w.Code)
	}
}
