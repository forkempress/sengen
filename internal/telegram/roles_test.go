package telegram

import "testing"

func TestIsAdminRole(t *testing.T) {
	cases := map[string]bool{
		"owner":      true,
		"dispatcher": true,
		// `viewer` arrives with the 90dayschengen parity work; it must never
		// reach the bot's write commands.
		"viewer": false,
		"driver": false,
		"admin":  false,
		"":       false,
		"Owner":  false, // roles are stored lowercase; no case-insensitive slack
		"owner ": false,
	}
	for role, want := range cases {
		if got := IsAdminRole(role); got != want {
			t.Errorf("IsAdminRole(%q) = %v, want %v", role, got, want)
		}
	}
}
