package store

import (
	"fmt"
	"unicode/utf8"
)

// Length limits for free-text fields that are stored for good. Without them
// a form could write megabytes per field into the database.
const (
	MaxNameLen  = 100 // driver name, vehicle label, user name
	MaxNoteLen  = 500 // stay note
	MaxEmailLen = 254 // RFC 5321 path limit
)

// checkLen rejects a value longer than max characters (not bytes: č is one).
func checkLen(field, value string, max int) error {
	if utf8.RuneCountInString(value) > max {
		return &ValidationError{Msg: fmt.Sprintf("%s je predugačak (najviše %d znakova)", field, max)}
	}
	return nil
}
