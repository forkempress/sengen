package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"shengen/internal/docread"
)

// Grade compares what a model read with the correct stays. A stay is
// correct only when driver, entry, exit and type all match; the note is not
// graded.
type Grade struct {
	Expected int `json:"expected"`
	Correct  int `json:"correct"`
	// Misread: a stay that exists, but with a wrong date or type.
	Misread int `json:"misread"`
	// Extra: a stay that is not in the document at all.
	Extra int `json:"extra"`
	// Missing: a correct stay the model did not return.
	Missing int `json:"missing"`
	// FlaggedErrors counts misread and extra stays the model marked
	// uncertain; FlaggedCorrect counts correct stays it marked uncertain.
	FlaggedErrors  int  `json:"flagged_errors"`
	FlaggedCorrect int  `json:"flagged_correct"`
	Perfect        bool `json:"perfect"`

	Mistakes []Mistake `json:"mistakes,omitempty"`
}

// Mistake is one difference, for the report. Want or Got is nil for a
// missing or an extra stay.
type Mistake struct {
	Want *docread.Stay `json:"want,omitempty"`
	Got  *docread.Stay `json:"got,omitempty"`
}

type stayKey struct{ driver, entry, exit, typ string }

func key(s docread.Stay) stayKey {
	return stayKey{normName(s.Driver), normDate(s.Entry), normDate(s.Exit), normType(s.Type)}
}

func normName(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

var dateLayouts = []string{"02.01.2006", "2006-01-02", "2.1.2006"}

func normDate(s string) string {
	s = strings.TrimSpace(s)
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("02.01.2006")
		}
	}
	return s
}

func normType(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "privatno", "private":
		return "privatno"
	default:
		return "posao"
	}
}

func grade(want, got []docread.Stay) Grade {
	g := Grade{Expected: len(want)}
	usedWant := make([]bool, len(want))
	var rest []docread.Stay
	for _, s := range got {
		k, matched := key(s), false
		for i, w := range want {
			if !usedWant[i] && key(w) == k {
				usedWant[i], matched = true, true
				break
			}
		}
		if !matched {
			rest = append(rest, s)
			continue
		}
		g.Correct++
		if s.Uncertain {
			g.FlaggedCorrect++
		}
	}
	// A stay of the same driver sharing the entry or the exit date is the
	// right stay read wrong; anything else was made up.
	for _, s := range rest {
		k, pair := key(s), -1
		for i, w := range want {
			wk := key(w)
			if !usedWant[i] && wk.driver == k.driver && (wk.entry == k.entry || wk.exit == k.exit) {
				pair = i
				break
			}
		}
		got := s
		if pair >= 0 {
			usedWant[pair] = true
			g.Misread++
			g.Mistakes = append(g.Mistakes, Mistake{Want: &want[pair], Got: &got})
		} else {
			g.Extra++
			g.Mistakes = append(g.Mistakes, Mistake{Got: &got})
		}
		if s.Uncertain {
			g.FlaggedErrors++
		}
	}
	for i := range want {
		if !usedWant[i] {
			g.Missing++
			g.Mistakes = append(g.Mistakes, Mistake{Want: &want[i]})
		}
	}
	g.Perfect = g.Misread == 0 && g.Extra == 0 && g.Missing == 0
	return g
}

// readExpected reads the correct stays in the import CSV format: a header
// row, then vozač, ulaz, izlaz, tip, napomena.
func readExpected(r io.Reader) ([]docread.Stay, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	records, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("prazan fajl")
	}
	var stays []docread.Stay
	for i, rec := range records[1:] {
		for len(rec) < 5 {
			rec = append(rec, "")
		}
		if strings.TrimSpace(strings.Join(rec, "")) == "" {
			continue
		}
		if strings.TrimSpace(rec[0]) == "" || strings.TrimSpace(rec[1]) == "" {
			return nil, fmt.Errorf("red %d: nedostaje vozač ili ulaz", i+2)
		}
		stays = append(stays, docread.Stay{Driver: strings.TrimSpace(rec[0]), Entry: rec[1], Exit: rec[2], Type: rec[3], Note: rec[4]})
	}
	return stays, nil
}

// driverNames lists the distinct drivers of the correct stays, in order.
func driverNames(stays []docread.Stay) []string {
	seen := map[string]bool{}
	var names []string
	for _, s := range stays {
		if k := normName(s.Driver); !seen[k] {
			seen[k] = true
			names = append(names, s.Driver)
		}
	}
	return names
}

func stayString(s *docread.Stay) string {
	exit := normDate(s.Exit)
	if exit == "" {
		exit = "…"
	}
	out := fmt.Sprintf("%s %s–%s %s", s.Driver, normDate(s.Entry), exit, normType(s.Type))
	if s.Uncertain {
		out += " (nesigurno)"
	}
	return out
}
