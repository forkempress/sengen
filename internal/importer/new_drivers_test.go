package importer

import (
	"strings"
	"testing"
)

func TestParseAllowingNewDrivers(t *testing.T) {
	existing := []ExistingDriver{{ID: 1, Name: "Ana Prva"}}
	csv := "vozač,ulaz,izlaz,tip\n" +
		"ana prva,01.01.2026,05.01.2026,posao\n" +
		"Novi Vozač,01.01.2026,05.01.2026,posao\n" +
		"novi vozač,03.01.2026,08.01.2026,posao\n" + // overlaps the new driver's row above
		strings.Repeat("x", MaxDriverNameLen+1) + ",01.02.2026,02.02.2026,posao\n"

	rows, err := ParseAllowingNewDrivers(strings.NewReader(csv), existing)
	if err != nil {
		t.Fatal(err)
	}
	if r := rows[0]; !r.Valid() || r.DriverID != 1 || r.NewDriver {
		t.Errorf("existing driver row: %+v", r)
	}
	if r := rows[1]; !r.Valid() || !r.NewDriver || r.DriverID != 0 {
		t.Errorf("new driver row: %+v", r)
	}
	if r := rows[2]; r.Valid() || !strings.Contains(strings.Join(r.Errors, ";"), "preklapa se sa drugim redom") {
		t.Errorf("overlap within a new driver not caught: %+v", r)
	}
	if r := rows[3]; r.Valid() || r.NewDriver {
		t.Errorf("over-long new name accepted: %+v", r)
	}

	// Plain Parse (CSV upload) still refuses unknown names.
	rows, _ = Parse(strings.NewReader(csv), existing)
	if rows[1].Valid() || rows[1].NewDriver {
		t.Errorf("Parse accepted an unknown driver: %+v", rows[1])
	}
}
