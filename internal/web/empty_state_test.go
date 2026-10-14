package web

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"shengen/internal/status"
	"shengen/internal/store"
)

func renderPage(t *testing.T, page string, data any) string {
	t.Helper()
	var buf bytes.Buffer
	if err := loadTemplates()[page].ExecuteTemplate(&buf, "layout.html", data); err != nil {
		t.Fatalf("render %s: %v", page, err)
	}
	return buf.String()
}

// A new firm sees its first steps, not four zeros and an empty table.
func TestPregled_NewFirmSeesChecklistInsteadOfZeros(t *testing.T) {
	html := renderPage(t, "pregled.html", pregledData{
		baseData:   baseData{User: &store.User{Name: "V", Role: "owner"}},
		Thresholds: status.DefaultThresholds,
		IsToday:    true,
	})
	for _, want := range []string{"Još 3 koraka do prvog pregleda", `href="/podesavanja/vozaci"`, `href="/import"`} {
		if !strings.Contains(html, want) {
			t.Errorf("empty dashboard does not contain %q", want)
		}
	}
	for _, unwanted := range []string{"Ukupno vozača", "kpi-row", "<table"} {
		if strings.Contains(html, unwanted) {
			t.Errorf("empty dashboard still shows %q", unwanted)
		}
	}
}

func TestPregled_ChecklistGoneOnceDriversHaveStays(t *testing.T) {
	html := renderPage(t, "pregled.html", pregledData{
		baseData:   baseData{User: &store.User{Name: "V", Role: "owner"}},
		Thresholds: status.DefaultThresholds,
		IsToday:    true,
		Setup:      setupSteps{HasDrivers: true, HasStays: true},
	})
	if strings.Contains(html, "do prvog pregleda") {
		t.Error("checklist shown to a firm that is set up")
	}
	if !strings.Contains(html, "kpi-row") {
		t.Error("dashboard cards missing")
	}
}

func TestDriverDetail_NoStaysShowsNextSteps(t *testing.T) {
	d := store.Driver{ID: 7, Name: "Vladimir", Active: true}
	html := renderPage(t, "vozac_detalj.html", driverDetailData{
		baseData: baseData{User: &store.User{Name: "V", Role: "owner"}},
		Driver:   d,
		Status:   status.Compute(d, nil, mustDate(t, "2026-10-14")),
	})
	for _, want := range []string{"Unesite boravke iz poslednjih 180 dana", `href="#dodaj-boravak"`, "Uvezi CSV"} {
		if !strings.Contains(html, want) {
			t.Errorf("driver without stays does not contain %q", want)
		}
	}
	if strings.Contains(html, "month-card") {
		t.Error("empty calendar still shown")
	}
}

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
