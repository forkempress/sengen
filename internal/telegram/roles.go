package telegram

// IsAdminRole reports whether a role may use the bot's admin commands
// (/pregled, /dodaj, file import, quick text entry, …), all of which write
// to driver records.
//
// The check is a deliberate allowlist rather than a "not a viewer" denylist:
// a role added later defaults to no admin access until it is listed here,
// instead of silently inheriting write permissions the day it is introduced.
func IsAdminRole(role string) bool {
	switch role {
	case "owner", "dispatcher":
		return true
	default:
		return false
	}
}

const noAdminAccessMsg = "Vaš nalog nema dozvolu za ovu komandu. Obratite se vlasniku naloga."
