package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"shengen/internal/auth"
	"shengen/internal/store"
)

// addUser adds a signed-in-able user to firm f and returns it as a firm
// value for login (same firm, other credentials).
func (a *testApp) addUser(f firm, email, role string) (firm, *store.User) {
	a.t.Helper()
	const password = "lozinka-korisnika"
	hash, err := auth.HashPassword(password)
	if err != nil {
		a.t.Fatal(err)
	}
	u, err := a.st.CreateUser(context.Background(), f.tenant.ID, email, hash, role, "Korisnik "+email)
	if err != nil {
		a.t.Fatal(err)
	}
	g := f
	g.email, g.password = email, password
	return g, u
}

func (a *testApp) user(tenantID, userID int64) *store.User {
	a.t.Helper()
	u, err := a.st.GetUser(context.Background(), tenantID, userID)
	if err != nil {
		a.t.Fatal(err)
	}
	return u
}

func (a *testApp) ownerOf(f firm) *store.User {
	a.t.Helper()
	u, err := a.st.GetUserByEmail(context.Background(), f.email)
	if err != nil {
		a.t.Fatal(err)
	}
	return u
}

func (a *testApp) auditActions(tenantID, userID int64) []string {
	a.t.Helper()
	rows, err := a.st.DB.QueryContext(context.Background(),
		`SELECT action || ' ' || COALESCE(before_json, '') || ' ' || COALESCE(after_json, '')
		 FROM audit_log WHERE tenant_id = ? AND entity = 'user' AND entity_id = ? ORDER BY id`, tenantID, userID)
	if err != nil {
		a.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func signedIn(s *session) bool {
	return s.get("/pregled").Code == http.StatusOK
}

// SEC-02: a deactivated dispatcher is signed out everywhere at once, cannot
// sign in again, and is back after reactivation.
func TestUsers_DeactivateEndsAccess(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	owner := app.mustLogin(a)
	dispFirm, disp := app.addUser(a, "dispecer@prva.rs", "dispatcher")
	d1, d2 := app.mustLogin(dispFirm), app.mustLogin(dispFirm)
	if err := app.st.SetUserTelegramChatID(context.Background(), a.tenant.ID, disp.ID, 9001); err != nil {
		t.Fatal(err)
	}

	w := owner.post("/podesavanja/korisnici/"+id(disp.ID)+"/deaktiviraj", nil)
	if msg := flashFrom(owner, w, "ok"); !strings.Contains(msg, "deaktiviran") {
		t.Errorf("confirmation = %q", msg)
	}
	if signedIn(d1) || signedIn(d2) {
		t.Error("a session of the deactivated user still opens /pregled")
	}
	if s, w := app.login(dispFirm.email, dispFirm.password); s != nil || w.Code != http.StatusForbidden ||
		!strings.Contains(w.Body.String(), "deaktiviran") {
		t.Errorf("login after deactivation: status %d, session %v", w.Code, s != nil)
	}
	if u := app.user(a.tenant.ID, disp.ID); u.Active || u.TelegramChatID != nil {
		t.Errorf("after deactivation: active %v, telegram %v", u.Active, u.TelegramChatID)
	}
	if !signedIn(owner) {
		t.Error("the owner was signed out too")
	}

	owner.post("/podesavanja/korisnici/"+id(disp.ID)+"/aktiviraj", nil)
	if s, w := app.login(dispFirm.email, dispFirm.password); s == nil {
		t.Errorf("login after reactivation failed: %d", w.Code)
	}

	got := app.auditActions(a.tenant.ID, disp.ID)
	if len(got) != 2 || !strings.HasPrefix(got[0], `deactivate {"active":true,"telegram_linked":true} {"active":false`) ||
		!strings.HasPrefix(got[1], `activate {"active":false`) {
		t.Errorf("audit = %q", got)
	}
}

func TestUsers_OwnerCannotDeactivateHimself(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	app.addUser(a, "drugi.vlasnik@prva.rs", "owner")
	sa := app.mustLogin(a)
	me := app.ownerOf(a)

	w := sa.post("/podesavanja/korisnici/"+id(me.ID)+"/deaktiviraj", nil)
	if msg := flashFrom(sa, w, "err"); !strings.Contains(msg, "sopstveni") {
		t.Errorf("error = %q, want a refusal", msg)
	}
	if !app.user(a.tenant.ID, me.ID).Active || !signedIn(sa) {
		t.Error("owner deactivated himself")
	}
}

func TestUsers_FirmKeepsAnActiveOwner(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	sa := app.mustLogin(a)
	me := app.ownerOf(a)

	w := sa.post("/podesavanja/korisnici/"+id(me.ID)+"/uloga", url.Values{"role": {"dispatcher"}})
	if msg := flashFrom(sa, w, "err"); !strings.Contains(msg, "bar jednog aktivnog vlasnika") {
		t.Errorf("error = %q", msg)
	}
	if app.user(a.tenant.ID, me.ID).Role != "owner" {
		t.Fatal("the only owner was demoted")
	}

	// With a second owner, stepping down works and leaves the users page.
	app.addUser(a, "drugi.vlasnik@prva.rs", "owner")
	w = sa.post("/podesavanja/korisnici/"+id(me.ID)+"/uloga", url.Values{"role": {"dispatcher"}})
	if loc := w.Header().Get("Location"); loc != "/pregled" {
		t.Errorf("redirect = %q, want /pregled", loc)
	}
	if app.user(a.tenant.ID, me.ID).Role != "dispatcher" {
		t.Error("demotion with another owner present failed")
	}
	if w := sa.get("/podesavanja/korisnici"); w.Code != http.StatusForbidden {
		t.Errorf("former owner opens users page: %d", w.Code)
	}
}

func TestUsers_RoleChangeIsAudited(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	sa := app.mustLogin(a)
	_, disp := app.addUser(a, "dispecer@prva.rs", "dispatcher")

	sa.post("/podesavanja/korisnici/"+id(disp.ID)+"/uloga", url.Values{"role": {"owner"}})
	if app.user(a.tenant.ID, disp.ID).Role != "owner" {
		t.Fatal("role not changed")
	}
	got := app.auditActions(a.tenant.ID, disp.ID)
	if len(got) != 1 || got[0] != `role_change {"role":"dispatcher"} {"role":"owner"}` {
		t.Errorf("audit = %q", got)
	}

	w := sa.post("/podesavanja/korisnici/"+id(disp.ID)+"/uloga", url.Values{"role": {"viewer"}})
	if msg := flashFrom(sa, w, "err"); msg != "Nepoznata uloga." {
		t.Errorf("unknown role: %q", msg)
	}
}

func TestUsers_DispatcherCannotManageUsers(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	dispFirm, _ := app.addUser(a, "dispecer@prva.rs", "dispatcher")
	sd := app.mustLogin(dispFirm)
	owner := app.ownerOf(a)

	for _, path := range []string{"/deaktiviraj", "/uloga"} {
		if w := sd.post("/podesavanja/korisnici/"+id(owner.ID)+path, url.Values{"role": {"dispatcher"}}); w.Code != http.StatusForbidden {
			t.Errorf("dispatcher POST %s = %d, want 403", path, w.Code)
		}
	}
	if u := app.user(a.tenant.ID, owner.ID); !u.Active || u.Role != "owner" {
		t.Errorf("owner changed by a dispatcher: %+v", u)
	}
}

// SEC-02: a user changes his own password; the current one is required,
// every other session ends, this browser stays signed in.
func TestPassword_Change(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	dispFirm, disp := app.addUser(a, "dispecer@prva.rs", "dispatcher")
	here, elsewhere := app.mustLogin(dispFirm), app.mustLogin(dispFirm)
	if err := app.st.SetUserTelegramChatID(context.Background(), a.tenant.ID, disp.ID, 9002); err != nil {
		t.Fatal(err)
	}
	const newPass = "nova-lozinka-123"

	for name, form := range map[string]url.Values{
		"wrong current": {"current_password": {"pogresna-lozinka"}, "new_password": {newPass}, "new_password_repeat": {newPass}},
		"too short":     {"current_password": {dispFirm.password}, "new_password": {"kratka"}, "new_password_repeat": {"kratka"}},
		"mismatch":      {"current_password": {dispFirm.password}, "new_password": {newPass}, "new_password_repeat": {newPass + "x"}},
	} {
		if w := here.post("/podesavanja/lozinka", form); w.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422", name, w.Code)
		}
	}
	if s, _ := app.login(dispFirm.email, dispFirm.password); s == nil {
		t.Fatal("password changed by a rejected form")
	}

	w := here.post("/podesavanja/lozinka", url.Values{
		"current_password": {dispFirm.password}, "new_password": {newPass}, "new_password_repeat": {newPass}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("change: status %d", w.Code)
	}
	var fresh *session
	for _, c := range w.Result().Cookies() {
		if c.Name == auth.SessionCookieName && c.Value != "" {
			fresh = &session{app: app, cookie: c}
		}
	}
	if fresh == nil {
		t.Fatal("no new session cookie after the change")
	}
	if body := fresh.get("/pregled").Body.String(); !strings.Contains(body, "Lozinka je promenjena") {
		t.Error("new session does not show the confirmation")
	}
	if signedIn(here) || signedIn(elsewhere) {
		t.Error("a session from before the change still works")
	}
	if s, _ := app.login(dispFirm.email, dispFirm.password); s != nil {
		t.Error("old password still signs in")
	}
	if s, _ := app.login(dispFirm.email, newPass); s == nil {
		t.Error("new password does not sign in")
	}
	if u := app.user(a.tenant.ID, disp.ID); u.TelegramChatID != nil {
		t.Error("Telegram still linked after the password change")
	}
	if got := app.auditActions(a.tenant.ID, disp.ID); len(got) != 1 || !strings.HasPrefix(got[0], "password_change") {
		t.Errorf("audit = %q", got)
	}
}

// The "add" forms live in dialogs; a refused form opens its dialog again
// with the reason and the typed values.
func TestAddForms_InDialogs(t *testing.T) {
	a := newTestApp(t)
	f := a.addFirm("Prva Firma", "prva", "vlasnik@prva.rs", "Ana", 30)
	s := a.mustLogin(f)
	for path, id := range map[string]string{
		"/podesavanja/vozaci":        "dodaj-vozaca",
		"/podesavanja/korisnici":     "dodaj-korisnika",
		"/vozaci/" + id(f.driver.ID): "dodaj-boravak",
	} {
		body := s.get(path).Body.String()
		if !strings.Contains(body, `<dialog class="add-dialog" id="`+id+`"`) || !strings.Contains(body, `data-dialog-open="`+id+`"`) {
			t.Errorf("%s: no dialog %s with its button", path, id)
		}
		if strings.Contains(body, "data-open-on-load") {
			t.Errorf("%s: dialog opens by itself without an error", path)
		}
	}
	w := s.post("/podesavanja/korisnici", url.Values{"name": {"Mika"}, "email": {"mika@prva.rs"}, "password": {"kratka"}, "role": {"dispatcher"}})
	body := w.Body.String()
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(body, "data-open-on-load") || !strings.Contains(body, "najmanje 10 znakova") || !strings.Contains(body, `value="mika@prva.rs"`) {
		t.Errorf("refused user form: %d, dialog not reopened with the error and values", w.Code)
	}
}
