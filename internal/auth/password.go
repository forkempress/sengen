package auth

import (
	"sync"

	"golang.org/x/crypto/bcrypt"
)

func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// dummyHash is computed once, on first use, so that unknown accounts cost the
// same bcrypt work as known ones.
var dummyHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("nije-prava-lozinka"), bcrypt.DefaultCost)
	return h
})

// SpendPasswordCheck performs a bcrypt comparison that always fails. Call it
// when the email is unknown: otherwise a failed login returns noticeably
// faster for a non-existent account than for a wrong password, and response
// times reveal which addresses have accounts — across every firm, now that
// the email alone decides the firm.
func SpendPasswordCheck(plain string) {
	bcrypt.CompareHashAndPassword(dummyHash(), []byte(plain))
}
