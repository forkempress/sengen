package calc

import "testing"

// The Schengen Area is not the EU. These two tables are taken verbatim from
// the domain spec: getting a country on the wrong side of this line means
// counting (or failing to count) days that decide whether a driver is
// deported at the border, so both directions are asserted explicitly.
func TestIsSchengen_CountriesInZone(t *testing.T) {
	inZone := map[string]string{
		"AT": "Austrija", "BE": "Belgija", "BG": "Bugarska", "CZ": "Češka",
		"DK": "Danska", "EE": "Estonija", "FI": "Finska", "FR": "Francuska",
		"GR": "Grčka", "HR": "Hrvatska", "IS": "Island", "IT": "Italija",
		"LV": "Letonija", "LI": "Lihtenštajn", "LT": "Litvanija", "LU": "Luksemburg",
		"HU": "Mađarska", "MT": "Malta", "NL": "Holandija", "DE": "Nemačka",
		"NO": "Norveška", "PL": "Poljska", "PT": "Portugal", "RO": "Rumunija",
		"SK": "Slovačka", "SI": "Slovenija", "ES": "Španija", "CH": "Švajcarska",
		"SE": "Švedska",
	}

	for code, name := range inZone {
		if !IsSchengen(code) {
			t.Errorf("IsSchengen(%q) = false, want true (%s je u Šengenu)", code, name)
		}
		if got := SchengenCountries[code]; got != name {
			t.Errorf("SchengenCountries[%q] = %q, want %q", code, got, name)
		}
	}

	if len(SchengenCountries) != len(inZone) {
		t.Errorf("SchengenCountries ima %d zemalja, očekivano %d", len(SchengenCountries), len(inZone))
	}
}

func TestIsSchengen_CountriesOutsideZone(t *testing.T) {
	// EU-but-not-Schengen, plus the non-EU neighbours Serbian drivers cross.
	outside := map[string]string{
		"CY": "Kipar", "IE": "Irska", "BA": "BiH", "ME": "Crna Gora",
		"MK": "S. Makedonija", "AL": "Albanija", "XK": "Kosovo",
		"TR": "Turska", "GB": "UK", "RS": "Srbija",
	}
	for code, name := range outside {
		if IsSchengen(code) {
			t.Errorf("IsSchengen(%q) = true, want false (%s nije u Šengenu)", code, name)
		}
	}
}

func TestIsSchengen_UnknownCode(t *testing.T) {
	for _, code := range []string{"", "ZZ", "at", "AUT"} {
		if IsSchengen(code) {
			t.Errorf("IsSchengen(%q) = true, want false", code)
		}
	}
}
