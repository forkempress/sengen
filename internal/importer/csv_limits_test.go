package importer

import (
	"strings"
	"testing"
)

// SEC-13: one import holds at most MaxRows data rows.
func TestParse_RejectsTooManyRows(t *testing.T) {
	var b strings.Builder
	b.WriteString("vozač,ulaz,izlaz,tip\n")
	for i := 0; i <= MaxRows; i++ {
		b.WriteString("Ana,2025-01-01,2025-01-02,posao\n")
	}
	if _, err := Parse(strings.NewReader(b.String()), nil); err == nil || !strings.Contains(err.Error(), "5000") {
		t.Errorf("Parse(%d rows) err = %v, want a row-limit error", MaxRows+1, err)
	}
}

// SEC-13: a row whose note is over the limit is shown as invalid rather than
// failing (or being stored) at confirm time.
func TestParse_LongNoteIsARowError(t *testing.T) {
	existing := []ExistingDriver{{ID: 1, Name: "Ana"}}
	csv := "vozač,ulaz,izlaz,tip,napomena\n" +
		"Ana,2025-01-01,2025-01-02,posao," + strings.Repeat("n", MaxNoteLen+1) + "\n" +
		"Ana,2025-02-01,2025-02-02,posao," + strings.Repeat("n", MaxNoteLen) + "\n"
	rows, err := Parse(strings.NewReader(csv), existing)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].Valid() {
		t.Error("row with a 501-character note is valid")
	}
	if !rows[1].Valid() {
		t.Errorf("row with a 500-character note is invalid: %v", rows[1].Errors)
	}
}
