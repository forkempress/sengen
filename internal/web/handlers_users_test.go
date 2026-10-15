package web

import (
	"strings"
	"testing"
)

func TestValidateNewUser(t *testing.T) {
	const good = "dovoljnoduga"
	cases := []struct {
		name, uName, email, pass, role string
		wantErr                        bool
	}{
		{"valid dispatcher", "Ana Anić", "ana@firma.rs", good, "dispatcher", false},
		{"valid owner", "Ana", "ana@firma.rs", good, "owner", false},
		{"missing name", "  ", "ana@firma.rs", good, "dispatcher", true},
		{"missing email", "Ana", "", good, "dispatcher", true},
		{"not an email", "Ana", "ana.firma.rs", good, "dispatcher", true},
		{"display-name form is not a bare address", "Ana", "Ana <ana@firma.rs>", good, "dispatcher", true},
		{"password too short", "Ana", "ana@firma.rs", "kratka", "dispatcher", true},
		{"password exactly 10", "Ana", "ana@firma.rs", "konj-trava", "dispatcher", false},
		{"common password", "Ana", "ana@firma.rs", "1234567890", "dispatcher", true},
		{"password containing the name", "Ana Marić", "ana@firma.rs", "maric-2026-x", "dispatcher", true},
		{"password over bcrypt limit", "Ana", "ana@firma.rs", strings.Repeat("a", 73), "dispatcher", true},
		// viewer is not assignable until it has real read-only enforcement.
		{"viewer not assignable yet", "Ana", "ana@firma.rs", good, "viewer", true},
		{"unknown role", "Ana", "ana@firma.rs", good, "admin", true},
		{"empty role", "Ana", "ana@firma.rs", good, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := validateNewUser(tc.uName, tc.email, tc.pass, tc.role)
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// Ten Serbian letters are ten characters to the user even though they are
// more bytes; the length rule must count what the user typed.
func TestValidateNewUser_PasswordLengthCountsCharacters(t *testing.T) {
	if _, _, err := validateNewUser("Ana", "ana@firma.rs", "čćžšđčćžšđ", "dispatcher"); err != nil {
		t.Errorf("10-character password with diacritics rejected: %v", err)
	}
}

func TestValidateNewUser_ReturnsCleanedValues(t *testing.T) {
	name, email, err := validateNewUser("  Ana Anić ", "  Ana@Firma.RS ", "dovoljnoduga", "dispatcher")
	if err != nil {
		t.Fatal(err)
	}
	if name != "Ana Anić" || email != "ana@firma.rs" {
		t.Errorf("cleaned = %q, %q; want \"Ana Anić\", \"ana@firma.rs\"", name, email)
	}
}
