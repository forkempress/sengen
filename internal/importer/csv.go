// Package importer parses the CSV history upload shared by the web "Uvoz
// istorije" page and (later) the Telegram admin file-import command, and
// validates each row against the same domain rules as manual entry (no
// future dates, exit >= entry, no overlaps) before anything is written.
package importer

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"shengen/internal/clock"
)

// Row is one parsed (and validated) line from the uploaded file. LineNum is
// 1-based and counts the header row, so error messages match what the user
// sees when they open the file in a spreadsheet program.
type Row struct {
	LineNum    int
	DriverName string
	DriverID   int64 // resolved against existing drivers; 0 if unknown
	Entry      time.Time
	Exit       *time.Time
	Type       string // normalized "work" | "private"
	Note       string
	Errors     []string
}

func (r Row) Valid() bool { return len(r.Errors) == 0 }

// ExistingDriver is the minimal lookup the parser needs to resolve a CSV
// driver name to a driver_id, and to check the new rows against that
// driver's current stays.
type ExistingDriver struct {
	ID    int64
	Name  string
	Stays []ExistingStay
}

type ExistingStay struct {
	Entry time.Time
	Exit  *time.Time
}

var dateLayouts = []string{"02.01.2006", "2006-01-02", "2.1.2006"}

func parseFlexDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("neprepoznat format datuma %q (koristi dd.mm.gggg)", s)
}

func normalizeType(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "work", "posao":
		return "work", true
	case "private", "privatno":
		return "private", true
	default:
		return "", false
	}
}

// Parse reads a CSV file with columns: vozač, ulaz, izlaz, tip[, napomena]
// (header row required, any header text accepted — only column position
// matters) and validates each row:
//   - shape: parseable dates, exit >= entry, no future dates, known type
//   - identity: driver name must match an existing driver (case-insensitive)
//   - overlap: against that driver's existing stays AND other rows for the
//     same driver earlier in this same file
//
// It never touches the database — callers insert the Valid() rows themselves,
// typically inside a single transaction (see store.Store.WithTx).
func Parse(r io.Reader, existing []ExistingDriver) ([]Row, error) {
	byName := make(map[string]*ExistingDriver, len(existing))
	for i := range existing {
		byName[strings.ToLower(strings.TrimSpace(existing[i].Name))] = &existing[i]
	}
	// Running set of rows accepted so far per driver, for in-batch overlap checks.
	batchByDriver := make(map[int64][]Row)

	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	today := clock.Today()

	var rows []Row
	lineNum := 0
	first := true
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("greška pri čitanju CSV-a u redu %d: %w", lineNum+1, err)
		}
		lineNum++
		if first {
			first = false
			continue // skip header
		}
		if len(rec) < 4 {
			rows = append(rows, Row{LineNum: lineNum, Errors: []string{"red ima manje od 4 kolone (vozač, ulaz, izlaz, tip)"}})
			continue
		}

		row := Row{LineNum: lineNum, DriverName: strings.TrimSpace(rec[0])}
		if len(rec) >= 5 {
			row.Note = strings.TrimSpace(rec[4])
		}

		driver, known := byName[strings.ToLower(row.DriverName)]
		if row.DriverName == "" {
			row.Errors = append(row.Errors, "nedostaje ime vozača")
		} else if !known {
			row.Errors = append(row.Errors, fmt.Sprintf("nepoznat vozač %q — dodajte ga prvo u Podešavanjima", row.DriverName))
		} else {
			row.DriverID = driver.ID
		}

		entry, eerr := parseFlexDate(rec[1])
		if eerr != nil {
			row.Errors = append(row.Errors, "ulaz: "+eerr.Error())
		} else {
			row.Entry = entry
			if entry.After(today) {
				row.Errors = append(row.Errors, "datum ulaska je u budućnosti")
			}
		}

		if exitStr := strings.TrimSpace(rec[2]); exitStr != "" {
			exit, xerr := parseFlexDate(exitStr)
			if xerr != nil {
				row.Errors = append(row.Errors, "izlaz: "+xerr.Error())
			} else {
				row.Exit = &exit
				if exit.After(today) {
					row.Errors = append(row.Errors, "datum izlaska je u budućnosti")
				}
				if eerr == nil && exit.Before(entry) {
					row.Errors = append(row.Errors, "izlaz je pre ulaska")
				}
			}
		}

		typ, ok := normalizeType(rec[3])
		if !ok {
			row.Errors = append(row.Errors, fmt.Sprintf("nepoznat tip %q (očekivano: posao/privatno)", rec[3]))
		} else {
			row.Type = typ
		}

		if known && eerr == nil {
			if msg := findOverlap(entry, row.Exit, driver.Stays); msg != "" {
				row.Errors = append(row.Errors, "preklapa se sa postojećim boravkom "+msg)
			}
			if msg := findOverlap(entry, row.Exit, toExistingStays(batchByDriver[driver.ID])); msg != "" {
				row.Errors = append(row.Errors, "preklapa se sa drugim redom iz ovog fajla "+msg)
			}
			batchByDriver[driver.ID] = append(batchByDriver[driver.ID], row)
		}

		rows = append(rows, row)
	}
	return rows, nil
}

func toExistingStays(rows []Row) []ExistingStay {
	out := make([]ExistingStay, len(rows))
	for i, r := range rows {
		out[i] = ExistingStay{Entry: r.Entry, Exit: r.Exit}
	}
	return out
}

// findOverlap returns a human-readable description of the first conflicting
// stay, or "" if entry/exit doesn't overlap any of existing.
func findOverlap(entry time.Time, exit *time.Time, existing []ExistingStay) string {
	today := clock.Today()
	newEnd := today
	if exit != nil {
		newEnd = *exit
	}
	for _, ex := range existing {
		exEnd := today
		if ex.Exit != nil {
			exEnd = *ex.Exit
		}
		if !entry.After(exEnd) && !ex.Entry.After(newEnd) {
			exitLabel := "otvoren"
			if ex.Exit != nil {
				exitLabel = ex.Exit.Format("02.01.2006")
			}
			return fmt.Sprintf("%s–%s", ex.Entry.Format("02.01.2006"), exitLabel)
		}
	}
	return ""
}
