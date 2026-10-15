package mail

// Data for each template. Keeping one struct per message makes a template
// that references a missing field fail in the tests, not in someone's inbox.

// ResetData: the "set a new password" link.
type ResetData struct {
	Name     string
	URL      string
	ValidFor string // "1 sat"
}

// ResetDoneData: notice after the password was changed through a reset link.
type ResetDoneData struct {
	Name     string
	When     string // "15.10.2026. u 14:05"
	ResetURL string
}
