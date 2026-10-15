package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Queries for the superadmin bot. Unlike the rest of the store they cross
// every firm, so they only ever return counts and firm-level fields — never a
// driver's name, stay dates or locations. Keep it that way: the superadmin
// commands print whatever these return. The one exception is a firm owner's
// contact while signing up (ListSignupsSince), which the operators need to
// follow up on a registration.

// Plans a firm can be on (tenants.plan).
const (
	PlanTrial  = "trial"
	PlanPaid   = "paid"
	PlanPaused = "paused"
)

const timestampLayout = "2006-01-02T15:04:05.000Z"

func formatTimestamp(t time.Time) string { return t.UTC().Format(timestampLayout) }

// PlatformStats are counts across every firm.
type PlatformStats struct {
	TenantsByPlan     map[string]int
	UsersByRole       map[string]int
	UsersOnTelegram   int
	DriversActive     int
	DriversLinked     int // active drivers linked to the bot
	DriversInSchengen int // active drivers with an open stay
	Stays             int
	StaysSince        int // stays created at or after the `since` argument
}

func (p PlatformStats) Tenants() int {
	n := 0
	for _, c := range p.TenantsByPlan {
		n += c
	}
	return n
}

func (p PlatformStats) Users() int {
	n := 0
	for _, c := range p.UsersByRole {
		n += c
	}
	return n
}

func (s *Store) PlatformStats(ctx context.Context, since time.Time) (PlatformStats, error) {
	p := PlatformStats{TenantsByPlan: map[string]int{}, UsersByRole: map[string]int{}}

	if err := s.countGroups(ctx, `SELECT plan, COUNT(*) FROM tenants GROUP BY plan`, p.TenantsByPlan); err != nil {
		return p, err
	}
	if err := s.countGroups(ctx, `SELECT role, COUNT(*) FROM users GROUP BY role`, p.UsersByRole); err != nil {
		return p, err
	}
	err := s.DB.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM users WHERE telegram_chat_id IS NOT NULL),
		(SELECT COUNT(*) FROM drivers WHERE active = 1),
		(SELECT COUNT(*) FROM drivers WHERE active = 1 AND telegram_chat_id IS NOT NULL),
		(SELECT COUNT(DISTINCT s.driver_id) FROM stays s JOIN drivers d ON d.id = s.driver_id
		  WHERE s.exit_date IS NULL AND d.active = 1),
		(SELECT COUNT(*) FROM stays),
		(SELECT COUNT(*) FROM stays WHERE created_at >= ?)`, formatTimestamp(since)).
		Scan(&p.UsersOnTelegram, &p.DriversActive, &p.DriversLinked, &p.DriversInSchengen, &p.Stays, &p.StaysSince)
	if err != nil {
		return p, fmt.Errorf("platform stats: %w", err)
	}
	return p, nil
}

func (s *Store) countGroups(ctx context.Context, query string, into map[string]int, args ...any) error {
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("count groups: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err != nil {
			return fmt.Errorf("scan group count: %w", err)
		}
		into[key] = n
	}
	return rows.Err()
}

// TenantOverview is one firm as the superadmin sees it: its own fields plus
// counts. LastActivity is the latest of: a driver added, a stay created or
// changed, an audit entry. FirstStayAt is when the firm entered its first stay.
type TenantOverview struct {
	ID                  int64
	Name                string
	Slug                string
	Plan                string
	TrialUntil          *time.Time // date only, UTC midnight
	PricePerDriverRSD   int // whole dinars per active driver per month
	CreatedAt           time.Time
	ActiveDrivers       int
	LinkedDrivers       int
	Users               int
	Stays               int
	LastActivity        *time.Time
	FirstStayAt         *time.Time
}

// MonthlyRSD is what the firm pays per month at its current driver count.
func (t TenantOverview) MonthlyRSD() int { return t.ActiveDrivers * t.PricePerDriverRSD }

const tenantOverviewQuery = `SELECT t.id, t.name, t.slug, t.plan, t.trial_until, t.price_per_driver_rsd, t.created_at,
	(SELECT COUNT(*) FROM drivers WHERE tenant_id = t.id AND active = 1),
	(SELECT COUNT(*) FROM drivers WHERE tenant_id = t.id AND active = 1 AND telegram_chat_id IS NOT NULL),
	(SELECT COUNT(*) FROM users WHERE tenant_id = t.id),
	(SELECT COUNT(*) FROM stays WHERE tenant_id = t.id),
	NULLIF(MAX(
		COALESCE((SELECT MAX(created_at) FROM drivers WHERE tenant_id = t.id), ''),
		COALESCE((SELECT MAX(updated_at) FROM stays WHERE tenant_id = t.id), ''),
		COALESCE((SELECT MAX(occurred_at) FROM audit_log WHERE tenant_id = t.id), '')), ''),
	(SELECT MIN(created_at) FROM stays WHERE tenant_id = t.id)
	FROM tenants t`

func scanTenantOverview(sc interface{ Scan(...any) error }) (TenantOverview, error) {
	var o TenantOverview
	var trialUntil, lastActivity, firstStay sql.NullString
	var createdAt string
	err := sc.Scan(&o.ID, &o.Name, &o.Slug, &o.Plan, &trialUntil, &o.PricePerDriverRSD, &createdAt,
		&o.ActiveDrivers, &o.LinkedDrivers, &o.Users, &o.Stays, &lastActivity, &firstStay)
	if err != nil {
		return o, err
	}
	o.CreatedAt, _ = parseTimestamp(createdAt)
	if trialUntil.Valid {
		if d, err := time.Parse(dateLayout, trialUntil.String); err == nil {
			o.TrialUntil = &d
		}
	}
	if lastActivity.Valid {
		if ts, err := parseTimestamp(lastActivity.String); err == nil {
			o.LastActivity = &ts
		}
	}
	if firstStay.Valid {
		if ts, err := parseTimestamp(firstStay.String); err == nil {
			o.FirstStayAt = &ts
		}
	}
	return o, nil
}

// ListTenantOverviews returns every firm, oldest first.
func (s *Store) ListTenantOverviews(ctx context.Context) ([]TenantOverview, error) {
	rows, err := s.DB.QueryContext(ctx, tenantOverviewQuery+` ORDER BY t.id`)
	if err != nil {
		return nil, fmt.Errorf("list tenant overviews: %w", err)
	}
	defer rows.Close()
	var out []TenantOverview
	for rows.Next() {
		o, err := scanTenantOverview(rows)
		if err != nil {
			return nil, fmt.Errorf("scan tenant overview: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) GetTenantOverview(ctx context.Context, id int64) (*TenantOverview, error) {
	o, err := scanTenantOverview(s.DB.QueryRowContext(ctx, tenantOverviewQuery+` WHERE t.id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get tenant overview: %w", err)
	}
	return &o, nil
}

// CountUsersByRole counts one firm's web users per role.
func (s *Store) CountUsersByRole(ctx context.Context, tenantID int64) (map[string]int, error) {
	out := map[string]int{}
	err := s.countGroups(ctx, `SELECT role, COUNT(*) FROM users WHERE tenant_id = ? GROUP BY role`, out, tenantID)
	return out, err
}

// CountStaysCreatedBetween counts stays created in [from, to) across all firms.
func (s *Store) CountStaysCreatedBetween(ctx context.Context, from, to time.Time) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM stays WHERE created_at >= ? AND created_at < ?`,
		formatTimestamp(from), formatTimestamp(to)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count stays: %w", err)
	}
	return n, nil
}

// SetTenantBilling sets a firm's plan, trial end and price (cmd/seed).
func (s *Store) SetTenantBilling(ctx context.Context, tenantID int64, plan string, trialUntil *time.Time, pricePerDriverRSD int) error {
	switch plan {
	case PlanTrial, PlanPaid, PlanPaused:
	default:
		return &ValidationError{Msg: fmt.Sprintf("nepoznat plan %q (trial, paid ili paused)", plan)}
	}
	if pricePerDriverRSD < 0 {
		return &ValidationError{Msg: "cena ne može biti negativna"}
	}
	var trial any
	if trialUntil != nil {
		trial = trialUntil.Format(dateLayout)
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE tenants SET plan = ?, trial_until = ?, price_per_driver_rsd = ? WHERE id = ?`,
		plan, trial, pricePerDriverRSD, tenantID)
	if err != nil {
		return fmt.Errorf("set tenant billing: %w", err)
	}
	return nil
}

// RecordSuperadminAudit logs one superadmin command, allowed or refused.
func (s *Store) RecordSuperadminAudit(ctx context.Context, telegramUserID int64, command, args string, allowed bool) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO superadmin_audit_log (telegram_user_id, command, args, allowed) VALUES (?, ?, ?, ?)`,
		telegramUserID, command, args, boolToInt(allowed))
	if err != nil {
		return fmt.Errorf("record superadmin audit: %w", err)
	}
	return nil
}

// SuperadminAuditEntry is one row of superadmin_audit_log.
type SuperadminAuditEntry struct {
	TelegramUserID int64
	Command        string
	Args           string
	Allowed        bool
	OccurredAt     time.Time
}

// ListSuperadminAudit returns the newest entries first.
func (s *Store) ListSuperadminAudit(ctx context.Context, limit int) ([]SuperadminAuditEntry, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT telegram_user_id, command, args, allowed, occurred_at FROM superadmin_audit_log
		 ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list superadmin audit: %w", err)
	}
	defer rows.Close()
	var out []SuperadminAuditEntry
	for rows.Next() {
		var e SuperadminAuditEntry
		var allowed int
		var at string
		if err := rows.Scan(&e.TelegramUserID, &e.Command, &e.Args, &allowed, &at); err != nil {
			return nil, fmt.Errorf("scan superadmin audit: %w", err)
		}
		e.Allowed = allowed == 1
		e.OccurredAt, _ = parseTimestamp(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// TryRecordSuperadminNotification claims (kind, ref) and reports whether this
// call was the first to do so — the caller sends only when it was.
func (s *Store) TryRecordSuperadminNotification(ctx context.Context, kind, ref string) (bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT OR IGNORE INTO superadmin_notifications (kind, ref) VALUES (?, ?)`, kind, ref)
	if err != nil {
		return false, fmt.Errorf("record superadmin notification: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("record superadmin notification: %w", err)
	}
	return n == 1, nil
}

// DatabaseSize returns the size of the main database file in bytes.
func (s *Store) DatabaseSize(ctx context.Context) (int64, error) {
	var pages, pageSize int64
	if err := s.DB.QueryRowContext(ctx, `SELECT page_count, page_size FROM pragma_page_count(), pragma_page_size()`).Scan(&pages, &pageSize); err != nil {
		return 0, fmt.Errorf("database size: %w", err)
	}
	return pages * pageSize, nil
}

// Ping runs a trivial query, proving the database answers.
func (s *Store) Ping(ctx context.Context) error {
	var one int
	return s.DB.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
}

// SignupCounts are sign-ups in a period. Started counts the addresses a
// confirmation email went to (a form sent again counts once); Confirmed
// counts the firms a confirmed sign-up opened.
type SignupCounts struct {
	Started, Confirmed int
}

// CountSignupsBetween counts sign-ups in [from, to). Both sources outlive
// the sign-up tokens, which are deleted once they expire: email_log is kept
// 90 days, audit_log for good.
func (s *Store) CountSignupsBetween(ctx context.Context, from, to time.Time) (SignupCounts, error) {
	var c SignupCounts
	f, t := formatTimestamp(from), formatTimestamp(to)
	err := s.DB.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(DISTINCT recipient) FROM email_log WHERE tag = 'signup' AND created_at >= ? AND created_at < ?),
		(SELECT COUNT(*) FROM audit_log WHERE entity = 'tenant' AND action = 'signup' AND occurred_at >= ? AND occurred_at < ?)`,
		f, t, f, t).Scan(&c.Started, &c.Confirmed)
	if err != nil {
		return c, fmt.Errorf("count signups: %w", err)
	}
	return c, nil
}

// StartedSignup is a sign-up as the operators hear about it: who is
// registering which firm. The password hash in the form never leaves here.
type StartedSignup struct {
	Ref       string // stable id of the sign-up, for notification dedup
	Email     string
	FirmName  string
	Name      string
	CreatedAt time.Time
}

// ListSignupsSince returns sign-ups started at or after since, confirmed or
// not, oldest first.
func (s *Store) ListSignupsSince(ctx context.Context, since time.Time) ([]StartedSignup, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT token_hash, email, payload, created_at FROM email_tokens
		 WHERE purpose = ? AND created_at >= ? ORDER BY created_at, token_hash`,
		EmailTokenSignup, formatTimestamp(since))
	if err != nil {
		return nil, fmt.Errorf("list signups: %w", err)
	}
	defer rows.Close()
	var out []StartedSignup
	for rows.Next() {
		var su StartedSignup
		var payload, created string
		if err := rows.Scan(&su.Ref, &su.Email, &payload, &created); err != nil {
			return nil, fmt.Errorf("scan signup: %w", err)
		}
		var form SignupForm
		if err := json.Unmarshal([]byte(payload), &form); err != nil {
			return nil, fmt.Errorf("signup payload: %w", err)
		}
		su.FirmName, su.Name = form.FirmName, form.Name
		su.CreatedAt, _ = parseTimestamp(created)
		out = append(out, su)
	}
	return out, rows.Err()
}
