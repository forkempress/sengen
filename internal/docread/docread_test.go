package docread

import (
	"bytes"
	"strings"
	"testing"
)

func TestCSV_QuotesAndOrder(t *testing.T) {
	got := string(CSV([]Stay{
		{Driver: "Petrović, Marko", Entry: "01.02.2026", Exit: "", Type: "posao", Note: `"tura" Beč`},
	}))
	want := "vozač,ulaz,izlaz,tip,napomena\n\"Petrović, Marko\",01.02.2026,,posao,\"\"\"tura\"\" Beč\"\n"
	if got != want {
		t.Errorf("CSV:\n%q\nwant\n%q", got, want)
	}
}

func TestCheckFile(t *testing.T) {
	page := "1 0 obj << /Type /Page /Parent 2 0 R >> endobj\n"
	if err := CheckFile([]byte("%PDF"+strings.Repeat(page, MaxPages)+"<< /Type /Pages >>"), "application/pdf"); err != nil {
		t.Errorf("%d pages refused: %v", MaxPages, err)
	}
	if err := CheckFile([]byte("%PDF"+strings.Repeat(page, MaxPages+1)), "application/pdf"); err != ErrTooLarge {
		t.Errorf("%d pages: %v, want ErrTooLarge", MaxPages+1, err)
	}
	if err := CheckFile(bytes.Repeat([]byte{0}, MaxFileBytes+1), "image/png"); err != ErrTooLarge {
		t.Errorf("oversized image: %v, want ErrTooLarge", err)
	}
}

func TestCheckResult_CountsDriversCaseInsensitively(t *testing.T) {
	var stays []Stay
	for range MaxStays {
		stays = append(stays, Stay{Driver: "Ana"}, Stay{Driver: " ana "})
	}
	if err := CheckResult(Result{Stays: stays[:MaxStays]}); err != nil {
		t.Errorf("%d stays of one driver refused: %v", MaxStays, err)
	}
	if err := CheckResult(Result{Stays: stays[:MaxStays+1]}); err != ErrTooLarge {
		t.Errorf("%d stays: %v, want ErrTooLarge", MaxStays+1, err)
	}
}
