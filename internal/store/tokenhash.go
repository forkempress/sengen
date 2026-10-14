package store

import (
	"crypto/sha256"
	"encoding/hex"
)

// hashToken is what the database keeps instead of a session or link token.
// The tokens are 32 random bytes, so a plain SHA-256 is enough: there is
// nothing to brute-force, only a copied database to make useless.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
