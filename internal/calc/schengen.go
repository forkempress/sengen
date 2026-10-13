package calc

// SchengenCountries is the authoritative list of ISO 3166-1 alpha-2 codes for
// countries in the Schengen Area — not the same set as the EU. Used by the
// Telegram geofence and by anywhere else in the app that needs to know whether
// a given country counts toward the 90/180 day limit.
var SchengenCountries = map[string]string{
	"AT": "Austrija",
	"BE": "Belgija",
	"BG": "Bugarska",
	"CZ": "Češka",
	"DK": "Danska",
	"EE": "Estonija",
	"FI": "Finska",
	"FR": "Francuska",
	"GR": "Grčka",
	"HR": "Hrvatska",
	"IS": "Island",
	"IT": "Italija",
	"LV": "Letonija",
	"LI": "Lihtenštajn",
	"LT": "Litvanija",
	"LU": "Luksemburg",
	"HU": "Mađarska",
	"MT": "Malta",
	"NL": "Holandija",
	"DE": "Nemačka",
	"NO": "Norveška",
	"PL": "Poljska",
	"PT": "Portugal",
	"RO": "Rumunija",
	"SK": "Slovačka",
	"SI": "Slovenija",
	"ES": "Španija",
	"CH": "Švajcarska",
	"SE": "Švedska",
}

// IsSchengen reports whether the given ISO 3166-1 alpha-2 country code is part
// of the Schengen Area. Explicitly NOT Schengen: Cyprus (CY), Ireland (IE),
// Bosnia and Herzegovina (BA), Montenegro (ME), North Macedonia (MK), Albania
// (AL), Kosovo (XK), Turkey (TR), United Kingdom (GB) — these are EU or
// Balkan neighbors but do not count toward the 90/180 rule.
func IsSchengen(countryCode string) bool {
	_, ok := SchengenCountries[countryCode]
	return ok
}
