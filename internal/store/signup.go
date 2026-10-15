package store

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// SignupForm is what a sign-up carries until its email is confirmed. No firm
// or account exists before that: an address cannot be taken, nor Telegram
// links handed out, by someone who does not read its mail.
type SignupForm struct {
	FirmName        string    `json:"firm"`
	Name            string    `json:"name"`
	PasswordHash    string    `json:"password_hash"`
	TermsVersion    string    `json:"terms_version"`
	TermsAcceptedAt time.Time `json:"terms_accepted_at"` // when the box was ticked
}

// PendingSignup is an unconfirmed sign-up, as redeemed by code or link.
type PendingSignup struct {
	Email string
	Form  SignupForm
	// FromBrowser: the code or link was used in the browser that filled in
	// the form (it holds that sign-up's cookie).
	FromBrowser bool
}

// MaxSignupCodeTries: after this many wrong codes the sign-up is void and
// the form must be sent again. Six digits, five tries: one in 200 000.
const MaxSignupCodeTries = 5

// ErrWrongCode: the code does not match; the sign-up stays open.
var ErrWrongCode = errors.New("wrong code")

// codeHash binds the 6-digit code to its token, so the same code in two
// sign-ups does not hash the same.
func codeHash(tokenHash, code string) string { return hashToken(tokenHash + ":" + code) }

// CreateSignup stores a sign-up waiting for its email to be confirmed. The
// token goes in the link, code is the 6-digit code, browser is the value of
// the cookie set in the browser that sent the form. A new sign-up for the
// same address voids the earlier ones.
func (s *Store) CreateSignup(ctx context.Context, token, code, browser, email string, form SignupForm, ttl time.Duration) error {
	payload, err := json.Marshal(form)
	if err != nil {
		return err
	}
	email = NormalizeEmail(email)
	th := hashToken(token)
	return s.WithTx(ctx, func(tx *Store) error {
		if _, err := tx.DB.ExecContext(ctx,
			`DELETE FROM email_tokens WHERE purpose = ? AND email = ? AND used_at IS NULL`, EmailTokenSignup, email); err != nil {
			return fmt.Errorf("void signups: %w", err)
		}
		if _, err := tx.DB.ExecContext(ctx,
			`INSERT INTO email_tokens (token_hash, purpose, email, code_hash, browser_hash, payload, expires_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			th, EmailTokenSignup, email, codeHash(th, code), hashToken(browser), string(payload),
			formatTimestamp(time.Now().Add(ttl))); err != nil {
			return fmt.Errorf("create signup: %w", err)
		}
		return nil
	})
}

// PeekSignupByLink returns the sign-up behind a link without using it, for
// the confirmation page (a mail scanner opening the link must not use it).
func (s *Store) PeekSignupByLink(ctx context.Context, token string) (*PendingSignup, error) {
	et, err := s.PeekEmailToken(ctx, token, EmailTokenSignup)
	if err != nil {
		return nil, err
	}
	return pendingFrom(et.Email, et.Payload, false)
}

// ConsumeSignupLink redeems the link from the email. browser is the
// sign-up cookie of the browser that opened it ("" if none).
func (s *Store) ConsumeSignupLink(ctx context.Context, token, browser string) (*PendingSignup, error) {
	now := formatTimestamp(time.Now())
	var email, payload string
	var browserHash sql.NullString
	err := s.DB.QueryRowContext(ctx,
		`UPDATE email_tokens SET used_at = ?
		 WHERE token_hash = ? AND purpose = ? AND used_at IS NULL AND expires_at > ? AND code_tries < ?
		 RETURNING email, payload, browser_hash`,
		now, hashToken(token), EmailTokenSignup, now, MaxSignupCodeTries,
	).Scan(&email, &payload, &browserHash)
	if errors.Is(err, sql.ErrNoRows) {
		if _, perr := s.PeekEmailToken(ctx, token, EmailTokenSignup); perr != nil {
			return nil, perr
		}
		return nil, ErrTokenExpired // voided by too many wrong codes
	}
	if err != nil {
		return nil, fmt.Errorf("consume signup link: %w", err)
	}
	return pendingFrom(email, payload, browser != "" && browserHash.Valid && sameHash(browserHash.String, hashToken(browser)))
}

// ConsumeSignupCode redeems the code typed in the browser that sent the
// form; the code works only there. A wrong code counts towards
// MaxSignupCodeTries, after which the sign-up is void.
func (s *Store) ConsumeSignupCode(ctx context.Context, browser, code string) (*PendingSignup, error) {
	if browser == "" {
		return nil, ErrNotFound
	}
	var out *PendingSignup
	err := s.WithTx(ctx, func(tx *Store) error {
		now := formatTimestamp(time.Now())
		var th, email, payload, stored string
		var tries int
		err := tx.DB.QueryRowContext(ctx,
			`SELECT token_hash, email, payload, code_hash, code_tries FROM email_tokens
			 WHERE purpose = ? AND browser_hash = ? AND used_at IS NULL AND expires_at > ?
			 ORDER BY created_at DESC LIMIT 1`,
			EmailTokenSignup, hashToken(browser), now,
		).Scan(&th, &email, &payload, &stored, &tries)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("find signup: %w", err)
		}
		if tries >= MaxSignupCodeTries {
			return ErrTokenExpired
		}
		if !sameHash(stored, codeHash(th, code)) {
			if tries+1 >= MaxSignupCodeTries {
				return errLastWrongCode
			}
			return ErrWrongCode
		}
		if _, err := tx.DB.ExecContext(ctx,
			`UPDATE email_tokens SET used_at = ? WHERE token_hash = ?`, now, th); err != nil {
			return fmt.Errorf("use signup: %w", err)
		}
		out, err = pendingFrom(email, payload, true)
		return err
	})
	// The transaction commits only on success, so a wrong code is counted
	// after it.
	switch {
	case errors.Is(err, ErrWrongCode):
		return nil, s.countWrongCode(ctx, browser, ErrWrongCode)
	case errors.Is(err, errLastWrongCode):
		return nil, s.countWrongCode(ctx, browser, ErrTokenExpired)
	}
	return out, err
}

// errLastWrongCode: the wrong code that uses up the last try.
var errLastWrongCode = errors.New("last wrong code")

// countWrongCode records a wrong code and returns result, or the database
// error: a try that was not counted must not look like a counted one.
func (s *Store) countWrongCode(ctx context.Context, browser string, result error) error {
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE email_tokens SET code_tries = code_tries + 1
		 WHERE purpose = ? AND browser_hash = ? AND used_at IS NULL AND code_tries < ?`,
		EmailTokenSignup, hashToken(browser), MaxSignupCodeTries); err != nil {
		return fmt.Errorf("count code try: %w", err)
	}
	return result
}

func sameHash(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

func pendingFrom(email, payload string, fromBrowser bool) (*PendingSignup, error) {
	var form SignupForm
	if err := json.Unmarshal([]byte(payload), &form); err != nil {
		return nil, fmt.Errorf("signup payload: %w", err)
	}
	return &PendingSignup{Email: email, Form: form, FromBrowser: fromBrowser}, nil
}

// ErrEmailTaken: the address got an account while the sign-up waited.
var ErrEmailTaken = &ValidationError{Msg: "Nalog sa ovim emailom već postoji. Prijavite se."}

// CreateFirmWithOwner opens a confirmed sign-up's firm: the firm on a trial
// ending trialUntil (a Belgrade date), its owner, and the terms as accepted
// on the form.
func (s *Store) CreateFirmWithOwner(ctx context.Context, email string, form SignupForm, trialUntil time.Time) (*Tenant, *User, error) {
	email = NormalizeEmail(email)
	if err := checkLen("naziv firme", form.FirmName, MaxNameLen); err != nil {
		return nil, nil, err
	}
	if err := checkLen("ime", form.Name, MaxNameLen); err != nil {
		return nil, nil, err
	}
	var t *Tenant
	var u *User
	err := s.WithTx(ctx, func(tx *Store) error {
		slug, err := tx.freeSlug(ctx, SlugFromName(form.FirmName))
		if err != nil {
			return err
		}
		res, err := tx.DB.ExecContext(ctx,
			`INSERT INTO tenants (name, slug, settings, plan, trial_until) VALUES (?, ?, '{}', ?, ?)`,
			form.FirmName, slug, PlanTrial, trialUntil.Format("2006-01-02"))
		if err != nil {
			return fmt.Errorf("create firm: %w", err)
		}
		tenantID, _ := res.LastInsertId()
		u, err = tx.CreateUser(ctx, tenantID, email, form.PasswordHash, "owner", form.Name)
		var verr *ValidationError
		if errors.As(err, &verr) {
			return ErrEmailTaken
		}
		if err != nil {
			return err
		}
		if _, err := tx.DB.ExecContext(ctx,
			`UPDATE tenants SET terms_version = ?, terms_accepted_at = ?, terms_accepted_by = ? WHERE id = ?`,
			form.TermsVersion, formatTimestamp(form.TermsAcceptedAt), u.ID, tenantID); err != nil {
			return fmt.Errorf("record terms: %w", err)
		}
		t = &Tenant{ID: tenantID, Name: form.FirmName, Slug: slug, Settings: "{}"}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return t, u, nil
}

// freeSlug returns base, or base-2, base-3 … if it is taken.
func (s *Store) freeSlug(ctx context.Context, base string) (string, error) {
	if base == "" {
		base = "firma"
	}
	for i := 1; i < 1000; i++ {
		slug := base
		if i > 1 {
			slug = base + "-" + strconv.Itoa(i)
		}
		var n int
		if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM tenants WHERE slug = ?`, slug).Scan(&n); err != nil {
			return "", fmt.Errorf("check slug: %w", err)
		}
		if n == 0 {
			return slug, nil
		}
	}
	return "", fmt.Errorf("no free slug for %q", base)
}
