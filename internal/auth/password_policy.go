package auth

import (
	"bufio"
	_ "embed"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Password rules for every password a person chooses: sign-up, reset,
// invitation, change, and the owner adding a user.
const (
	MinPasswordLen = 10
	// MaxPasswordBytes: bcrypt silently ignores anything past 72 bytes, so
	// the limit is enforced with a readable message instead.
	MaxPasswordBytes = 72
)

var (
	ErrPasswordShort    = errors.New("Lozinka mora imati najmanje 10 znakova.")
	ErrPasswordLong     = errors.New("Lozinka je predugačka (najviše 72 bajta).")
	ErrPasswordCommon   = errors.New("Ova lozinka je među najčešćim i lako se pogađa. Izaberite drugu.")
	ErrPasswordPersonal = errors.New("Lozinka ne sme da sadrži vaš email, ime ili naziv firme.")
)

//go:embed common_passwords.txt
var commonPasswordsFile string

var commonPasswords = func() map[string]bool {
	m := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(commonPasswordsFile))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m[strings.ToLower(line)] = true
	}
	return m
}()

// commonWords are guessed with any digits or symbols tacked on:
// "Lozinka2026!" is "lozinka" to whoever is guessing.
var commonWords = map[string]bool{
	"password": true, "passwort": true, "lozinka": true, "sifra": true, "šifra": true,
	"qwerty": true, "qwertz": true, "admin": true, "administrator": true, "welcome": true,
	"iloveyou": true, "letmein": true, "dani": true, "beograd": true, "srbija": true,
	"zvezda": true, "partizan": true, "volimte": true, "schengen": true, "sengen": true,
	"šengen": true, "prevoz": true, "kamion": true, "transport": true, "vozac": true, "vozač": true,
}

// CheckNewPassword applies the password rules. personal lists what the
// person is known by — email, name, firm name — none of which may appear
// in the password (parts shorter than 4 characters are ignored).
func CheckNewPassword(password string, personal ...string) error {
	if utf8.RuneCountInString(password) < MinPasswordLen {
		return ErrPasswordShort
	}
	if len(password) > MaxPasswordBytes {
		return ErrPasswordLong
	}
	lower := strings.ToLower(password)
	if commonPasswords[lower] || commonWords[strings.TrimRightFunc(lower, notLetter)] || distinctRunes(lower) < 4 {
		return ErrPasswordCommon
	}
	folded := foldDiacritics(lower)
	for _, p := range personal {
		for _, part := range personalParts(p) {
			if strings.Contains(folded, foldDiacritics(part)) {
				return ErrPasswordPersonal
			}
		}
	}
	return nil
}

// foldDiacritics makes "Marić" and "maric" the same word.
var foldDiacritics = strings.NewReplacer("č", "c", "ć", "c", "š", "s", "ž", "z", "đ", "dj").Replace

func notLetter(r rune) bool { return !unicode.IsLetter(r) }

func distinctRunes(s string) int {
	seen := map[rune]bool{}
	for _, r := range s {
		seen[r] = true
	}
	return len(seen)
}

// personalParts splits "ana.petrovic@prevoz-djuric.rs" or "Prevoz Đurić
// d.o.o." into the words a password must not contain.
func personalParts(s string) []string {
	s = strings.ToLower(s)
	if at := strings.IndexByte(s, '@'); at >= 0 {
		s = s[:at] // the domain is shared with colleagues and says little
	}
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if utf8.RuneCountInString(f) >= 4 && f != "d.o.o" && f != "doo" {
			out = append(out, f)
		}
	}
	return out
}
