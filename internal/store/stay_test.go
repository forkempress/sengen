package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"shengen/internal/clock"
	"shengen/internal/db"
	"shengen/internal/store"
)

// newTestStore opens a real SQLite database (migrations and all) in a temp
// directory, so these tests exercise the same schema constraints production
// runs on — including the partial unique index that backs "one open stay
// per driver". The clock is pinned to noon of the current Belgrade date, so
// "tomorrow" is in the future even when the suite runs after 23:00.
func newTestStore(t *testing.T) (*store.Store, int64, int64) {
	t.Helper()
	d := clock.Today()
	noon := time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, clock.Now().Location())
	t.Cleanup(clock.Set(func() time.Time { return noon }))

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	st := store.New(conn)
	ctx := context.Background()

	tenant, err := st.CreateTenant(ctx, "Test Prevoz", "test.local")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	driver, err := st.CreateDriver(ctx, tenant.ID, "Marko Markovic", "BG-123-AB")
	if err != nil {
		t.Fatalf("create driver: %v", err)
	}
	return st, tenant.ID, driver.ID
}

func day(offset int) time.Time {
	return clock.Today().AddDate(0, 0, offset)
}

func dayPtr(offset int) *time.Time {
	d := day(offset)
	return &d
}

// mustCreateStay fails the test if the stay is rejected — used to set up
// preconditions, not to assert behaviour.
func mustCreateStay(t *testing.T, st *store.Store, tenantID, driverID int64, entry time.Time, exit *time.Time) *store.Stay {
	t.Helper()
	stay, err := st.CreateStay(context.Background(), tenantID, driverID, entry, exit, "work", "manual", "", nil)
	if err != nil {
		t.Fatalf("CreateStay(%s–%v) unexpectedly failed: %v", entry.Format("02.01.2006"), exit, err)
	}
	return stay
}

func assertValidationError(t *testing.T, err error, context string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected a validation error, got nil", context)
	}
	var verr *store.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("%s: expected *store.ValidationError, got %T: %v", context, err, err)
	}
	if verr.Msg == "" {
		t.Errorf("%s: validation error has an empty message", context)
	}
}

func TestCreateStay_RejectsExitBeforeEntry(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)

	_, err := st.CreateStay(context.Background(), tenantID, driverID, day(-5), dayPtr(-10), "work", "manual", "", nil)
	assertValidationError(t, err, "exit before entry")
}

func TestCreateStay_RejectsFutureDates(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	ctx := context.Background()

	// Open stay starting tomorrow: only the entry date is in the future, so
	// this isolates the entry check instead of tripping the exit check.
	_, err := st.CreateStay(ctx, tenantID, driverID, day(1), nil, "work", "manual", "", nil)
	assertValidationError(t, err, "entry in the future (open stay)")

	// Closed stay ending tomorrow: isolates the exit check.
	_, err = st.CreateStay(ctx, tenantID, driverID, day(-2), dayPtr(1), "work", "manual", "", nil)
	assertValidationError(t, err, "exit in the future")

	// Both in the future must fail too.
	_, err = st.CreateStay(ctx, tenantID, driverID, day(1), dayPtr(3), "work", "manual", "", nil)
	assertValidationError(t, err, "entry and exit in the future")
}

func TestCreateStay_AllowsTodayAndSameDayTrip(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)

	// Entering and leaving on the same day is a legitimate 1-day stay.
	stay := mustCreateStay(t, st, tenantID, driverID, day(0), dayPtr(0))
	if stay.Exit == nil || !stay.Exit.Equal(day(0)) {
		t.Errorf("same-day stay stored exit %v, want %s", stay.Exit, day(0).Format("02.01.2006"))
	}
}

func TestCreateStay_RejectsOverlapWithExistingStay(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	ctx := context.Background()
	mustCreateStay(t, st, tenantID, driverID, day(-20), dayPtr(-10))

	cases := []struct {
		name         string
		entry, exitN int
	}{
		{"identical range", -20, -10},
		{"contained inside", -18, -12},
		{"spans across", -25, -5},
		{"overlaps start", -25, -15},
		{"overlaps end", -15, -5},
		{"touches first day", -25, -20},
		{"touches last day", -10, -5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := st.CreateStay(ctx, tenantID, driverID, day(tc.entry), dayPtr(tc.exitN), "work", "manual", "", nil)
			assertValidationError(t, err, tc.name)
		})
	}
}

// The day after an exit is free: a driver can leave Schengen and re-enter the
// next day. An off-by-one here would silently block legitimate entries.
func TestCreateStay_AllowsBackToBackStays(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	mustCreateStay(t, st, tenantID, driverID, day(-20), dayPtr(-10))

	mustCreateStay(t, st, tenantID, driverID, day(-9), dayPtr(-5))
	mustCreateStay(t, st, tenantID, driverID, day(-25), dayPtr(-21))
}

func TestCreateStay_RejectsSecondOpenStay(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	mustCreateStay(t, st, tenantID, driverID, day(-5), nil)

	_, err := st.CreateStay(context.Background(), tenantID, driverID, day(-2), nil, "work", "manual", "", nil)
	assertValidationError(t, err, "second open stay")
}

// An open stay occupies every day through today, so a closed stay may not be
// slipped in underneath it.
func TestCreateStay_OpenStayBlocksOverlappingClosedStay(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	mustCreateStay(t, st, tenantID, driverID, day(-5), nil)

	_, err := st.CreateStay(context.Background(), tenantID, driverID, day(-3), dayPtr(-1), "work", "manual", "", nil)
	assertValidationError(t, err, "closed stay under an open one")
}

// Two drivers are counted separately, so identical dates must be fine.
func TestCreateStay_DifferentDriversMayShareDates(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	ctx := context.Background()

	other, err := st.CreateDriver(ctx, tenantID, "Petar Petrovic", "NS-456-CD")
	if err != nil {
		t.Fatalf("create second driver: %v", err)
	}

	mustCreateStay(t, st, tenantID, driverID, day(-20), dayPtr(-10))
	mustCreateStay(t, st, tenantID, other.ID, day(-20), dayPtr(-10))
}

// Editing a stay must not treat the stay itself as a conflicting one.
func TestUpdateStay_ExcludesItselfFromOverlapCheck(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	ctx := context.Background()
	stay := mustCreateStay(t, st, tenantID, driverID, day(-20), dayPtr(-10))

	if err := st.UpdateStay(ctx, tenantID, stay.ID, day(-20), dayPtr(-10), "work", "bez izmene"); err != nil {
		t.Fatalf("re-saving a stay with unchanged dates failed: %v", err)
	}
	if err := st.UpdateStay(ctx, tenantID, stay.ID, day(-21), dayPtr(-9), "private", "prošireno"); err != nil {
		t.Fatalf("widening a stay with no neighbours failed: %v", err)
	}

	got, err := st.GetStay(ctx, tenantID, stay.ID)
	if err != nil {
		t.Fatalf("GetStay: %v", err)
	}
	if !got.Entry.Equal(day(-21)) || got.Exit == nil || !got.Exit.Equal(day(-9)) {
		t.Errorf("stay after update = %s–%v, want %s–%s",
			got.Entry.Format("02.01.2006"), got.Exit, day(-21).Format("02.01.2006"), day(-9).Format("02.01.2006"))
	}
	if got.Type != "private" {
		t.Errorf("stay type after update = %q, want \"private\"", got.Type)
	}
}

func TestUpdateStay_StillRejectsOverlapWithOtherStays(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	ctx := context.Background()
	mustCreateStay(t, st, tenantID, driverID, day(-20), dayPtr(-10))
	second := mustCreateStay(t, st, tenantID, driverID, day(-9), dayPtr(-5))

	err := st.UpdateStay(ctx, tenantID, second.ID, day(-15), dayPtr(-5), "work", "")
	assertValidationError(t, err, "update pulled into a neighbour")
}

func TestCloseOpenStay(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	ctx := context.Background()

	if _, err := st.CloseOpenStay(ctx, tenantID, driverID, day(0)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("CloseOpenStay with no open stay = %v, want ErrNotFound", err)
	}

	mustCreateStay(t, st, tenantID, driverID, day(-5), nil)
	closed, err := st.CloseOpenStay(ctx, tenantID, driverID, day(0))
	if err != nil {
		t.Fatalf("CloseOpenStay: %v", err)
	}
	if closed.Exit == nil || !closed.Exit.Equal(day(0)) {
		t.Errorf("closed stay exit = %v, want %s", closed.Exit, day(0).Format("02.01.2006"))
	}
	if _, err := st.OpenStayForDriver(ctx, tenantID, driverID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("driver still has an open stay after closing: %v", err)
	}
}

func TestCloseOpenStay_RejectsReturnBeforeEntry(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	mustCreateStay(t, st, tenantID, driverID, day(-5), nil)

	_, err := st.CloseOpenStay(context.Background(), tenantID, driverID, day(-10))
	assertValidationError(t, err, "return date before entry")
}

// After 23:00 Belgrade time it is already tomorrow at an EET border (BG, RO,
// GR); a stamp with that date is not in the future. The day after is.
func TestCreateStay_AcceptsEETDateBeforeBelgradeMidnight(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	v, _ := time.ParseInLocation("2006-01-02 15:04", "2026-10-14 23:30", clock.Now().Location())
	t.Cleanup(clock.Set(func() time.Time { return v }))
	ctx := context.Background()
	d := func(s string) time.Time { x, _ := time.Parse("2006-01-02", s); return x }

	_, err := st.CreateStay(ctx, tenantID, driverID, d("2026-10-16"), nil, "work", "manual", "", nil)
	assertValidationError(t, err, "entry two days ahead")
	if _, err := st.CreateStay(ctx, tenantID, driverID, d("2026-10-15"), nil, "work", "telegram_button", "", nil); err != nil {
		t.Errorf("entry on the EET date was refused: %v", err)
	}
}
