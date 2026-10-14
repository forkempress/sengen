package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// UX-01: deleting a stay and deactivating a driver or user ask first. The
// forms are plain POSTs, so hx-confirm would do nothing; data-confirm is
// handled by /static/app.js (no inline script, the CSP refuses those).
func TestDestructiveFormsAskForConfirmation(t *testing.T) {
	app, a, _ := twoFirmApp(t)
	sa := app.mustLogin(a)
	_, disp := app.addUser(a, "dispecer@prva.rs", "dispatcher")

	formRe := regexp.MustCompile(`<form[^>]*action="([^"]*/(?:obrisi|deaktiviraj))"[^>]*>`)
	for _, tc := range []struct {
		page, action string
	}{
		{"/vozaci/" + id(a.driver.ID), "/vozaci/" + id(a.driver.ID) + "/boravci/" + id(a.stay.ID) + "/obrisi"},
		{"/podesavanja/vozaci", "/podesavanja/vozaci/" + id(a.driver.ID) + "/deaktiviraj"},
		{"/podesavanja/korisnici", "/podesavanja/korisnici/" + id(disp.ID) + "/deaktiviraj"},
	} {
		body := sa.get(tc.page).Body.String()
		found := false
		for _, m := range formRe.FindAllStringSubmatch(body, -1) {
			if !strings.Contains(m[0], `data-confirm="`) {
				t.Errorf("%s: form %s has no data-confirm", tc.page, m[1])
			}
			if m[1] == tc.action {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no form posting to %s", tc.page, tc.action)
		}
		if !strings.Contains(body, `<script src="/static/app.js"`) {
			t.Errorf("%s does not load app.js", tc.page)
		}
	}

	// The stay's dates are in the question, so the user sees what goes.
	body := sa.get("/vozaci/" + id(a.driver.ID)).Body.String()
	if !strings.Contains(body, "Obrisati boravak "+a.stay.Entry.Format("02.01.2006")) {
		t.Error("delete confirmation does not name the stay")
	}
}

func TestTemplates_NoHxConfirmOnPlainForms(t *testing.T) {
	err := fs.WalkDir(templateFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(templateFS, path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "hx-confirm") {
			t.Errorf("%s uses hx-confirm, which does nothing on a plain form; use data-confirm", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAppJS_HandlesDataConfirm(t *testing.T) {
	app, _, _ := twoFirmApp(t)
	w := app.request("GET", "/static/app.js", nil, nil)
	js := w.Body.String()
	if w.Code != 200 || !strings.Contains(js, "'submit'") || !strings.Contains(js, "data-confirm") ||
		!strings.Contains(js, "preventDefault") {
		t.Errorf("app.js does not cancel a submit the user did not confirm (status %d)", w.Code)
	}
}
