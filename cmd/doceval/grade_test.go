package main

import (
	"strings"
	"testing"

	"shengen/internal/docread"
)

func TestGrade_SortsMistakes(t *testing.T) {
	want := []docread.Stay{
		{Driver: "Marko Petrović", Entry: "01.02.2026", Exit: "10.02.2026", Type: "posao"},
		{Driver: "Marko Petrović", Entry: "01.03.2026", Exit: "05.03.2026", Type: "posao"},
		{Driver: "Ana Jović", Entry: "03.04.2026", Exit: "", Type: "privatno"},
	}
	got := []docread.Stay{
		// Same stay, other spelling of case, spaces, date and type.
		{Driver: " marko  petrović", Entry: "2026-02-01", Exit: "10.2.2026", Type: ""},
		// Right entry, wrong exit, flagged.
		{Driver: "Marko Petrović", Entry: "01.03.2026", Exit: "08.03.2026", Type: "posao", Uncertain: true},
		// Not in the document.
		{Driver: "Ana Jović", Entry: "20.05.2026", Exit: "22.05.2026", Type: "posao"},
	}
	g := grade(want, got)
	if g.Expected != 3 || g.Correct != 1 || g.Misread != 1 || g.Extra != 1 || g.Missing != 1 ||
		g.FlaggedErrors != 1 || g.FlaggedCorrect != 0 || g.Perfect {
		t.Errorf("grade = %+v", g)
	}
	if !grade(want, want).Perfect {
		t.Error("identical stays are not perfect")
	}
}

func TestGrade_DuplicateStaysCountOnce(t *testing.T) {
	s := docread.Stay{Driver: "Ana", Entry: "01.01.2026", Exit: "02.01.2026"}
	g := grade([]docread.Stay{s}, []docread.Stay{s, s})
	if g.Correct != 1 || g.Extra != 1 {
		t.Errorf("grade = %+v", g)
	}
}

func TestReadExpected(t *testing.T) {
	stays, err := readExpected(strings.NewReader("vozač,ulaz,izlaz,tip,napomena\nAna Jović,03.04.2026,,privatno,\n,,,,\nMarko,01.02.2026,05.02.2026\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stays) != 2 || stays[0].Type != "privatno" || stays[1].Exit != "05.02.2026" {
		t.Errorf("stays = %+v", stays)
	}
	if names := driverNames(append(stays, docread.Stay{Driver: "ana jović"})); len(names) != 2 {
		t.Errorf("driverNames = %v", names)
	}
	if _, err := readExpected(strings.NewReader("vozač,ulaz\n,01.02.2026\n")); err == nil {
		t.Error("row without a driver accepted")
	}
}
