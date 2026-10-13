// Package idgen generates cryptographically random, URL-safe tokens — used
// for session tokens, CSRF secrets, and Telegram link tokens.
package idgen

import (
	"crypto/rand"
	"encoding/base64"
)

func RandomToken(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
