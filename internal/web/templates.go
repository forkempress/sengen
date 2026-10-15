package web

import (
	"embed"
	"html/template"
	"time"

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
	"fmtDatePtr": func(t *time.Time) string {
		if t == nil {
			return "otvoreno"
		}
		return t.Format("02.01.2006")
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
	"pregled.html",
	"vozac_detalj.html",
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
