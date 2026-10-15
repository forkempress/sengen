package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckNewPassword(t *testing.T) {
	cases := []struct {
		password string
		personal []string
		want     error
	}{
		{"kratka", nil, ErrPasswordShort},
		{"čćžšđčćžš", nil, ErrPasswordShort}, // 9 characters, 18 bytes
		{strings.Repeat("ž", 37), nil, ErrPasswordLong},
		{"1234567890", nil, ErrPasswordCommon},
		{"QwertyUiop", nil, ErrPasswordCommon},
		{"Lozinka2026!", nil, ErrPasswordCommon},
		{"password!!!!", nil, ErrPasswordCommon},
		{"aaaaaaaaab", nil, ErrPasswordCommon},
		{"ana.petrovic77", []string{"ana.petrovic@firma.rs"}, ErrPasswordPersonal},
		{"MojDjuric2026", []string{"x@y.rs", "Prevoz Djuric d.o.o."}, ErrPasswordPersonal},
		{"novi-maric-77", []string{"Ana Marić"}, ErrPasswordPersonal},
		{"konj-trava-sto-9", []string{"ana@firma.rs", "Ana Petrović", "Firma"}, nil},
		{"tri lepe reci ovde", nil, nil},
	}
	for _, tc := range cases {
		if got := CheckNewPassword(tc.password, tc.personal...); !errors.Is(got, tc.want) {
			t.Errorf("CheckNewPassword(%q, %v) = %v, want %v", tc.password, tc.personal, got, tc.want)
		}
	}
}

func TestCommonPasswordsAreAllLongEnough(t *testing.T) {
	for p := range commonPasswords {
		if len([]rune(p)) < MinPasswordLen {
			t.Errorf("%q is shorter than the minimum and can never match", p)
		}
	}
}
