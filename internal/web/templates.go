package web

import (
	"embed"
	"html/template"
	"time"

	"shengen/internal/clock"
	"shengen/internal/legal"
	"shengen/internal/status"
)

//go:embed static
var staticFS embed.FS

//go:embed templates/*.html
var templateFS embed.FS

type templateSet map[string]*template.Template

var funcMap = template.FuncMap{
	// levelBadge renders a status without a number (legends, filters).
	"levelBadge": status.LevelBadge,
	"levels":     func() []status.Level { return status.Levels },
	"qrSVG":      qrSVG,
	"asset":      assetURL,
	// dana picks "dan" or "dana" for a count: 1 dan, 21 dan, 11 dana, 5 dana.
	"dana": status.DaysWord,
	// days is the English one, for the report printed in English.
	"days": func(n int) string {
		if n == 1 || n == -1 {
			return "day"
		}
		return "days"
	},
	// legal is the provider's details, the terms version and the disclaimer.
	"legal": legal.Get,
	"fmtDate": func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Format("02.01.2006")
	},
	"fmtDateInput": func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Format("2006-01-02")
	},
	// today is the Belgrade date: the default in date fields, so a dispatcher
	// only changes the day and month. latestToday is the last date a stay may
	// have (an EET crossing after 23:00 is already tomorrow there).
	"today":       clock.Today,
	"latestToday": clock.LatestToday,
	"fmtDayMonth": func(t time.Time) string { return t.Format("02.01.") },
	// lastTripDay is the last day of a trip of n days starting on start.
	"lastTripDay": func(start time.Time, n int) time.Time {
		if n < 1 {
			return start
		}
		return start.AddDate(0, 0, n-1)
	},
	"fmtDatePtr": func(t *time.Time) string {
		if t == nil {
			return "otvoreno"
		}
		return t.Format("02.01.2006")
	},
	// crossing hands the dashboard's border form one row; where tells the
	// table ("t") from the phone cards ("k"), which both render every row.
	"crossing": func(st status.DriverStatus, page pregledData, where string) crossingRow {
		return crossingRow{Status: st, CSRFToken: page.CSRFToken, BackURL: page.BackURL, AsOf: page.AsOf, Where: where}
	},
	// driverCrossing is the same form on the drivers page, dated today.
	"driverCrossing": func(st status.DriverStatus, csrf string) crossingRow {
		return crossingRow{Status: st, CSRFToken: csrf, BackURL: "/podesavanja/vozaci", AsOf: clock.Today(), Where: "v"}
	},
	"deref": func(t *time.Time) time.Time {
		if t == nil {
			return time.Time{}
		}
		return *t
	},
	"barPct": func(used int) int {
		pct := used * 100 / 90
		if pct > 100 {
			return 100
		}
		if pct < 0 {
			return 0
		}
		return pct
	},
	"roleLabel": func(role string) string {
		if label, ok := assignableRoles[role]; ok {
			return label
		}
		return role
	},
	"sourceLabel": sourceLabel,
}

// pages lists every top-level template that gets rendered inside layout.html.
var pages = []string{
	"login.html",
	"zaboravljena_lozinka.html",
	"nova_lozinka.html",
	"registracija.html",
	"registracija_potvrda.html",
	"pregled.html",
	"vozac_detalj.html",
	"vozac_izvestaj.html",
	"planer.html",
	"privatnost.html",
	"uslovi.html",
	"obrada_podataka.html",
	"prihvatanje.html",
	"podesavanja_vozaci.html",
	"podesavanja_telegram.html",
	"podesavanja_korisnici.html",
	"podesavanja_lozinka.html",
	"import.html",
	"pomoc.html",
	"greska.html",
}

func loadTemplates() templateSet {
	out := make(templateSet, len(pages))
	for _, p := range pages {
		t := template.Must(template.New("layout.html").Funcs(funcMap).ParseFS(templateFS, "templates/layout.html", "templates/_planer.html", "templates/"+p))
		out[p] = t
	}
	return out
}

// sourceLabel names where a stay came from, for the page and the export.
func sourceLabel(source string) string {
	switch source {
	case "manual":
		return "ručno"
	case "import":
		return "uvoz"
	case "telegram_button":
		return "Telegram"
	case "telegram_admin":
		return "Telegram (admin)"
	case "telegram_location":
		return "Telegram (lokacija)"
	case "":
		return "—"
	default:
		return source
	}
}
