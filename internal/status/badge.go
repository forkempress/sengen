package status

import (
	"fmt"
	"html/template"
)

// Level is a driver's status. It is named by what it means, never by its
// colour, so the same word appears in the badge, the filter and the help
// page (UX-03, UX-13).
type Level int

const (
	LevelOK Level = iota
	LevelWarn
	LevelCritical
	LevelOver
)

// Levels lists every status from the worst to the best, the order the
// dashboard shows them in.
var Levels = []Level{LevelOver, LevelCritical, LevelWarn, LevelOK}

// Word is the status as a word.
func (l Level) Word() string {
	switch l {
	case LevelOver:
		return "Preko limita"
	case LevelCritical:
		return "Kritično"
	case LevelWarn:
		return "Pažnja"
	default:
		return "OK"
	}
}

// Icon differs per status in shape, not only in colour, so it reads in
// black and white and for colour-blind users.
func (l Level) Icon() string {
	switch l {
	case LevelOver:
		return "⛔"
	case LevelCritical:
		return "▲"
	case LevelWarn:
		return "!"
	default:
		return "✓"
	}
}

// Key is the value used in URLs (?ozbiljnost=…) and as the CSS modifier.
func (l Level) Key() string {
	switch l {
	case LevelOver:
		return "preko"
	case LevelCritical:
		return "kriticno"
	case LevelWarn:
		return "paznja"
	default:
		return "ok"
	}
}

// Class is the badge's CSS class.
func (l Level) Class() string { return "badge-" + l.Key() }

// LevelFromKey parses a Key; ok is false for anything else.
func LevelFromKey(key string) (Level, bool) {
	for _, l := range Levels {
		if l.Key() == key {
			return l, true
		}
	}
	return 0, false
}

// Range describes which remaining days fall into the level, for filter
// labels and the help page ("< 15", "15–30", "> 30", "0").
func (t Thresholds) Range(l Level) string {
	switch l {
	case LevelOver:
		return "0 dana"
	case LevelCritical:
		return fmt.Sprintf("< %d", t.Red)
	case LevelWarn:
		return fmt.Sprintf("%d–%d", t.Red, t.Orange)
	default:
		return fmt.Sprintf("> %d", t.Orange)
	}
}

// Badge is the one status component: icon + word + number.
type Badge struct {
	Level     Level
	Remaining int
	OverBy    int
}

// Number is the days figure in the badge: days left, or "−6" for a driver
// six days over the limit.
func (b Badge) Number() string {
	if b.OverBy > 0 {
		return fmt.Sprintf("−%d", b.OverBy)
	}
	return fmt.Sprintf("%d", b.Remaining)
}

// Text is the badge as plain text, e.g. "▲ Kritično 6".
func (b Badge) Text() string {
	return b.Level.Icon() + " " + b.Level.Word() + " " + b.Number()
}

// Class is the badge's CSS class.
func (b Badge) Class() string { return b.Level.Class() }

// HTML renders the badge. The icon is hidden from screen readers, which
// read the word and the number.
func (b Badge) HTML() template.HTML {
	return template.HTML(fmt.Sprintf(
		`<span class="badge %s"><span class="badge-icon" aria-hidden="true">%s</span> %s <span class="num">%s</span></span>`,
		b.Level.Class(), b.Level.Icon(), template.HTMLEscapeString(b.Level.Word()), b.Number()))
}

// LevelBadge renders a badge with only the icon and word, for legends and
// filters where there is no single driver's number.
func LevelBadge(l Level) template.HTML {
	return template.HTML(fmt.Sprintf(
		`<span class="badge %s"><span class="badge-icon" aria-hidden="true">%s</span> %s</span>`,
		l.Class(), l.Icon(), template.HTMLEscapeString(l.Word())))
}
