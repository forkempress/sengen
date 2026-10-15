package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"shengen/internal/auth"
	"shengen/internal/clock"
	"shengen/internal/legal"
	"shengen/internal/mail"
	"shengen/internal/store"
)

const signupPassword = "konj-trava-sto-9"

func signupForm(firm, name, email string) url.Values {
	return url.Values{"firm": {firm}, "name": {name}, "email": {email}, "password": {signupPassword}, "terms": {"da"}}
}

// testBrowser is one browser: it keeps the cookies the sign-up sets.
type testBrowser struct {
	app     *resetApp
	cookies map[string]*http.Cookie
}

func (a *resetApp) browser() *testBrowser {
	return &testBrowser{app: a, cookies: map[string]*http.Cookie{}}
}

func (b *testBrowser) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	r := httptest.NewRequest(method, path, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range b.cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	b.app.handler.ServeHTTP(w, r)
	for _, c := range w.Result().Cookies() {
		if c.MaxAge < 0 || c.Value == "" {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c
		}
	}
	return w
}

func (b *testBrowser) session() *session {
	if c, ok := b.cookies[auth.SessionCookieName]; ok {
		return &session{app: b.app.testApp, cookie: c}
	}
	return nil
}

var (
	signupCodeRe = regexp.MustCompile(`(?m)^\s+(\d{6})\s*$`)
	signupLinkRe = regexp.MustCompile(`https://app\.dani90\.rs/registracija/potvrdi\?t=([A-Za-z0-9_-]+)`)
)

func signupMail(t *testing.T, msgs []mail.Message, to string) (code, token string) {
	t.Helper()
	for _, m := range msgs {
		if m.To == to && m.Tag == "signup" {
			c := signupCodeRe.FindStringSubmatch(m.Text)
			l := signupLinkRe.FindStringSubmatch(m.Text)
			if c == nil || l == nil {
				t.Fatalf("no code or link in:\n%s", m.Text)
			}
			return c[1], l[1]
		}
	}
	t.Fatalf("no signup mail to %s in %+v", to, msgs)
	return "", ""
}

func countTenants(t *testing.T, st *store.Store) int {
	t.Helper()
	all, err := st.ListAllTenants(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return len(all)
}

func TestSignup_WithCode(t *testing.T) {
	a := newResetApp(t)
	other := a.addFirm("Druga Firma", "druga", "vlasnik@druga.rs", "Bora", 30)
	b := a.browser()

	if w := b.do(http.MethodGet, "/registracija", nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `name="firm"`) {
		t.Fatalf("form: %d", w.Code)
	}
	w := b.do(http.MethodPost, "/registracija", signupForm("Prevoz Nova", "Ana Novak", "Ana@Nova.rs"))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Proverite mejl") {
		t.Fatalf("submit: %d\n%s", w.Code, w.Body.String())
	}
	if countTenants(t, a.st) != 1 {
		t.Fatal("a firm exists before the email is confirmed")
	}
	code, _ := signupMail(t, a.sent(), "ana@nova.rs")

	w = b.do(http.MethodPost, "/registracija/kod", url.Values{"code": {code[:3] + " " + code[3:]}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/pregled" {
		t.Fatalf("code: %d %s\n%s", w.Code, w.Header().Get("Location"), w.Body.String())
	}
	sess := b.session()
	if sess == nil {
		t.Fatal("not signed in after the code")
	}
	page := sess.get("/pregled")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Dobrodošli") {
		t.Fatalf("first page: %d", page.Code)
	}
	// The new firm sees its own empty data, nothing of the other firm.
	if strings.Contains(page.Body.String(), other.driver.Name) {
		t.Error("new firm sees another firm's driver")
	}
	if w := sess.get("/vozaci/" + strconv.FormatInt(other.driver.ID, 10)); w.Code != http.StatusNotFound {
		t.Errorf("other firm's driver: %d, want 404", w.Code)
	}

	u, err := a.st.GetUserByEmail(context.Background(), "ana@nova.rs")
	if err != nil || u.Role != "owner" || u.Name != "Ana Novak" {
		t.Fatalf("owner = %+v (%v)", u, err)
	}
	ov, _ := a.st.GetTenantOverview(context.Background(), u.TenantID)
	wantUntil := clock.Today().AddDate(0, 0, trialDays).Format("2006-01-02")
	if ov.Name != "Prevoz Nova" || ov.Plan != store.PlanTrial || ov.TrialUntil == nil || ov.TrialUntil.Format("2006-01-02") != wantUntil {
		t.Errorf("firm = %s plan %s trial %v, want trial until %s", ov.Name, ov.Plan, ov.TrialUntil, wantUntil)
	}
	ts, _ := a.st.GetTermsStatus(context.Background(), u.TenantID)
	if ts.Version != legal.TermsVersion || ts.AcceptedBy == nil || *ts.AcceptedBy != u.ID {
		t.Errorf("terms = %+v", ts)
	}
	// Signed in with the chosen password.
	if s, _ := a.login("ana@nova.rs", signupPassword); s == nil {
		t.Error("the chosen password does not sign in")
	}
	// The code is used up.
	if w := b.do(http.MethodPost, "/registracija/kod", url.Values{"code": {code}}); w.Code != http.StatusGone {
		t.Errorf("code reused: %d", w.Code)
	}
}

func TestSignup_LinkInSameAndOtherBrowser(t *testing.T) {
	a := newResetApp(t)

	// Same browser: confirmed and signed in.
	b := a.browser()
	b.do(http.MethodPost, "/registracija", signupForm("Prva Nova", "Ana", "ana@prva-nova.rs"))
	_, token := signupMail(t, a.sent(), "ana@prva-nova.rs")
	page := b.do(http.MethodGet, "/registracija/potvrdi?t="+token, nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Prva Nova") {
		t.Fatalf("link page: %d", page.Code)
	}
	if countTenants(t, a.st) != 0 {
		t.Fatal("opening the link (GET) opened the firm")
	}
	if w := b.do(http.MethodPost, "/registracija/potvrdi", url.Values{"t": {token}}); w.Code != http.StatusSeeOther || b.session() == nil {
		t.Fatalf("confirm in same browser: %d, session %v", w.Code, b.session() != nil)
	}

	// Another browser (the mail app's): confirmed, but no session — the
	// password is still needed.
	c := a.browser()
	c.do(http.MethodPost, "/registracija", signupForm("Druga Nova", "Bora", "bora@druga-nova.rs"))
	_, token = signupMail(t, a.sent(), "bora@druga-nova.rs")
	elsewhere := a.browser()
	w := elsewhere.do(http.MethodPost, "/registracija/potvrdi", url.Values{"t": {token}})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Nalog je potvrđen") || elsewhere.session() != nil {
		t.Fatalf("confirm elsewhere: %d, session %v", w.Code, elsewhere.session() != nil)
	}
	if s, _ := a.login("bora@druga-nova.rs", signupPassword); s == nil {
		t.Error("cannot sign in after confirming elsewhere")
	}
	if w := elsewhere.do(http.MethodPost, "/registracija/potvrdi", url.Values{"t": {token}}); w.Code != http.StatusGone {
		t.Errorf("link reused: %d", w.Code)
	}
}

// Whether the address is new or has an account, the page is the same; the
// owner of an existing address gets "you already have an account" and no
// firm is created.
func TestSignup_NoEnumeration(t *testing.T) {
	a := newResetApp(t)
	f := a.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	norm := func(body, email string) string { return strings.ReplaceAll(body, email, "EMAIL") }

	fresh := a.browser().do(http.MethodPost, "/registracija", signupForm("Nova", "Ana", "nova@adresa.rs"))
	taken := a.browser().do(http.MethodPost, "/registracija", signupForm("Nova", "Ana", f.email))
	if fresh.Code != taken.Code || norm(fresh.Body.String(), "nova@adresa.rs") != norm(taken.Body.String(), f.email) {
		t.Error("the reply differs between a new and a taken address")
	}
	if (fresh.Header().Get("Set-Cookie") == "") != (taken.Header().Get("Set-Cookie") == "") {
		t.Error("only one of the replies sets a cookie")
	}
	tags := map[string]string{}
	for _, m := range a.sent() {
		tags[m.To] = m.Tag
	}
	if tags["nova@adresa.rs"] != "signup" || tags[f.email] != "signup_exists" {
		t.Errorf("mails = %v", tags)
	}
	if countTenants(t, a.st) != 1 {
		t.Error("a firm was created for a taken address")
	}
}

func TestSignup_WrongCodes(t *testing.T) {
	a := newResetApp(t)
	b := a.browser()
	b.do(http.MethodPost, "/registracija", signupForm("Nova", "Ana", "ana@nova.rs"))
	code, token := signupMail(t, a.sent(), "ana@nova.rs")
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for i := 1; i < store.MaxSignupCodeTries; i++ {
		if w := b.do(http.MethodPost, "/registracija/kod", url.Values{"code": {wrong}}); w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("wrong code %d: %d", i, w.Code)
		}
	}
	if w := b.do(http.MethodPost, "/registracija/kod", url.Values{"code": {wrong}}); w.Code != http.StatusGone {
		t.Errorf("last wrong code: %d, want 410", w.Code)
	}
	if w := b.do(http.MethodPost, "/registracija/kod", url.Values{"code": {code}}); w.Code != http.StatusGone {
		t.Errorf("right code after the tries ran out: %d", w.Code)
	}
	if w := b.do(http.MethodPost, "/registracija/potvrdi", url.Values{"t": {token}}); w.Code != http.StatusGone {
		t.Errorf("link after the tries ran out: %d", w.Code)
	}
	if countTenants(t, a.st) != 0 {
		t.Error("firm opened after too many wrong codes")
	}
}

// The code works only in the browser that sent the form.
func TestSignup_CodeFromAnotherBrowser(t *testing.T) {
	a := newResetApp(t)
	a.browser().do(http.MethodPost, "/registracija", signupForm("Nova", "Ana", "ana@nova.rs"))
	code, _ := signupMail(t, a.sent(), "ana@nova.rs")
	if w := a.browser().do(http.MethodPost, "/registracija/kod", url.Values{"code": {code}}); w.Code != http.StatusGone {
		t.Errorf("code from another browser: %d", w.Code)
	}
	if countTenants(t, a.st) != 0 {
		t.Error("firm opened from another browser's code")
	}
}

func TestSignup_ExpiredLink(t *testing.T) {
	a := newResetApp(t)
	form := store.SignupForm{FirmName: "Stara", Name: "Ana", PasswordHash: "h", TermsVersion: legal.TermsVersion, TermsAcceptedAt: time.Now()}
	if err := a.st.CreateSignup(context.Background(), "star", "123456", "b", "ana@stara.rs", form, -time.Minute); err != nil {
		t.Fatal(err)
	}
	if w := a.browser().do(http.MethodGet, "/registracija/potvrdi?t=star", nil); w.Code != http.StatusGone || !strings.Contains(w.Body.String(), "istekao") {
		t.Errorf("expired link page: %d", w.Code)
	}
	if w := a.browser().do(http.MethodPost, "/registracija/potvrdi", url.Values{"t": {"star"}}); w.Code != http.StatusGone {
		t.Errorf("expired link confirm: %d", w.Code)
	}
}

func TestSignup_Validation(t *testing.T) {
	a := newResetApp(t)
	cases := map[string]url.Values{
		"no firm":      {"firm": {""}, "name": {"Ana"}, "email": {"a@b.rs"}, "password": {signupPassword}, "terms": {"da"}},
		"no name":      {"firm": {"F"}, "name": {" "}, "email": {"a@b.rs"}, "password": {signupPassword}, "terms": {"da"}},
		"bad email":    {"firm": {"F"}, "name": {"Ana"}, "email": {"nije-email"}, "password": {signupPassword}, "terms": {"da"}},
		"weak pass":    {"firm": {"F"}, "name": {"Ana"}, "email": {"a@b.rs"}, "password": {"1234567890"}, "terms": {"da"}},
		"pass = firm":  {"firm": {"Kamioni Jovic"}, "name": {"Ana"}, "email": {"a@b.rs"}, "password": {"jovic-2026-xy"}, "terms": {"da"}},
		"no terms box": {"firm": {"F"}, "name": {"Ana"}, "email": {"a@b.rs"}, "password": {signupPassword}},
		"long firm":    {"firm": {strings.Repeat("a", 101)}, "name": {"Ana"}, "email": {"a@b.rs"}, "password": {signupPassword}, "terms": {"da"}},
	}
	for name, form := range cases {
		w := a.browser().do(http.MethodPost, "/registracija", form)
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d, want 422", name, w.Code)
		}
		if strings.Contains(w.Body.String(), signupPassword) {
			t.Errorf("%s: the password is echoed back", name)
		}
	}
	if n := len(a.sent()); n != 0 {
		t.Errorf("%d mails for invalid forms", n)
	}
	// Typed values come back so nothing has to be retyped (but not the password).
	w := a.browser().do(http.MethodPost, "/registracija", url.Values{"firm": {"Moja Firma"}, "name": {"Ana"}, "email": {"a@b.rs"}, "password": {signupPassword}})
	if !strings.Contains(w.Body.String(), `value="Moja Firma"`) {
		t.Error("firm name not kept after an error")
	}
}

func TestSignup_RateLimits(t *testing.T) {
	a := newResetApp(t)
	for i := 0; i < signupLimitPerEmail; i++ {
		a.browser().do(http.MethodPost, "/registracija", signupForm("Nova", "Ana", "ana@nova.rs"))
	}
	if w := a.browser().do(http.MethodPost, "/registracija", signupForm("Nova", "Ana", "ana@nova.rs")); w.Code != http.StatusTooManyRequests {
		t.Errorf("over the per-address limit: %d", w.Code)
	}
	b := newResetApp(t)
	limited := false
	for i := 0; i < signupLimitPerIP+2; i++ {
		w := b.browser().do(http.MethodPost, "/registracija", signupForm("Nova", "Ana", "osoba"+strconv.FormatInt(int64(i), 10)+"@nova.rs"))
		if w.Code == http.StatusTooManyRequests {
			limited = true
			if i < signupLimitPerIP {
				t.Errorf("limited after %d, want %d", i, signupLimitPerIP)
			}
			break
		}
	}
	if !limited {
		t.Error("one IP was never limited")
	}
}

func TestSignup_OffWithoutMail(t *testing.T) {
	a := newResetApp(t)
	a.srv.MailReady = false
	if strings.Contains(a.request(http.MethodGet, "/login", nil, nil).Body.String(), `href="/registracija"`) {
		t.Error("login offers a sign-up that cannot send its code")
	}
	if w := a.browser().do(http.MethodPost, "/registracija", signupForm("Nova", "Ana", "ana@nova.rs")); w.Code != http.StatusServiceUnavailable {
		t.Errorf("sign-up without email: %d", w.Code)
	}
	if len(a.sent()) != 0 || countTenants(t, a.st) != 0 {
		t.Error("sign-up went ahead without email")
	}
}

func TestSignup_CrossOriginRefused(t *testing.T) {
	a := newResetApp(t)
	r := httptest.NewRequest(http.MethodPost, "/registracija", strings.NewReader(signupForm("Nova", "Ana", "ana@nova.rs").Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	a.handler.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || len(a.sent()) != 0 {
		t.Errorf("cross-site sign-up: %d", w.Code)
	}
}

// The feedback address is shown to signed-in users only.
func TestFeedbackLink(t *testing.T) {
	a := newResetApp(t)
	a.srv.FeedbackEmail = "utisci@dani90.rs"
	f := a.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	if body := a.mustLogin(f).get("/pregled").Body.String(); !strings.Contains(body, `href="mailto:utisci@dani90.rs?subject=`) {
		t.Error("signed-in page lacks the feedback link")
	}
	for _, path := range []string{"/login", "/registracija", "/privatnost"} {
		if strings.Contains(a.request(http.MethodGet, path, nil, nil).Body.String(), "utisci@dani90.rs") {
			t.Errorf("%s shows the feedback address to visitors", path)
		}
	}
	a.srv.FeedbackEmail = ""
	if strings.Contains(a.mustLogin(f).get("/pregled").Body.String(), "mailto:") {
		t.Error("feedback link shown without an address")
	}
}

// The public site's email button goes through /kontakt to MAIL_REPLY_TO.
func TestContactRedirect(t *testing.T) {
	a := newResetApp(t)
	a.srv.FeedbackEmail = "utisci@dani90.rs"
	w := a.request(http.MethodGet, "/kontakt", nil, nil)
	loc := w.Header().Get("Location")
	if w.Code != http.StatusFound || !strings.HasPrefix(loc, "mailto:utisci@dani90.rs?subject=Dani90%20%E2%80%94%20pitanje&body=Firma%3A") {
		t.Errorf("/kontakt = %d to %q", w.Code, loc)
	}
	if strings.Contains(loc, "+") {
		t.Errorf("mailto uses + for spaces: %q", loc)
	}
	a.srv.FeedbackEmail = ""
	if w := a.request(http.MethodGet, "/kontakt", nil, nil); w.Code != http.StatusNotFound {
		t.Errorf("/kontakt without an address = %d, want 404", w.Code)
	}
}
