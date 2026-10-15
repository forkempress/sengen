package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"shengen/internal/store"
)

func count(t *testing.T, st *store.Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := st.DB.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// Permanent deletion leaves nothing about the driver in any table, keeps the
// audit trail without his dates, and does not touch another driver.
func TestDeleteDriverPermanently_RemovesEverything(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	ctx := context.Background()
	other, err := st.CreateDriver(ctx, tenantID, "Drugi Vozač", "")
	if err != nil {
		t.Fatal(err)
	}

	stay := mustCreateStay(t, st, tenantID, driverID, day(-20), dayPtr(-10))
	mustCreateStay(t, st, tenantID, driverID, day(-2), nil)
	otherStay := mustCreateStay(t, st, tenantID, other.ID, day(-20), dayPtr(-10))
	plan, err := st.CreatePlannedStay(ctx, tenantID, driverID, day(5), day(9), "Minhen", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetDriverTelegramChatID(ctx, tenantID, driverID, 777); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateDriverLinkToken(ctx, "token-za-brisanje", tenantID, driverID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := st.TryRecordAlert(ctx, tenantID, driverID, "driver_red", "2026-10-15"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.ExecContext(ctx,
		`INSERT INTO events (tenant_id, driver_id, kind, occurred_at) VALUES (?, ?, 'exit_serbia', '2026-10-01T10:00:00Z')`,
		tenantID, driverID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.ExecContext(ctx,
		`INSERT INTO stays_cross_tenant (id, tenant_id, driver_id, entry_date, type, source) VALUES (9999, 999, ?, '2026-09-01', 'work', 'import')`,
		driverID); err != nil {
		t.Fatal(err)
	}
	dates := `{"od":"2026-10-20","do":"2026-10-24"}`
	st.RecordAudit(ctx, tenantID, "user", nil, "stay", stay.ID, "create", nil, &dates)
	st.RecordAudit(ctx, tenantID, "user", nil, "planned_stay", plan.ID, "create", nil, &dates)
	st.RecordAudit(ctx, tenantID, "user", nil, "stay", otherStay.ID, "create", nil, &dates)

	if err := st.DeleteDriverPermanently(ctx, tenantID, driverID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	for _, q := range []string{
		`SELECT COUNT(*) FROM drivers WHERE id = ?`,
		`SELECT COUNT(*) FROM stays WHERE driver_id = ?`,
		`SELECT COUNT(*) FROM planned_stays WHERE driver_id = ?`,
		`SELECT COUNT(*) FROM events WHERE driver_id = ?`,
		`SELECT COUNT(*) FROM telegram_link_tokens WHERE driver_id = ?`,
		`SELECT COUNT(*) FROM telegram_alert_log WHERE driver_id = ?`,
		`SELECT COUNT(*) FROM stays_cross_tenant WHERE driver_id = ?`,
	} {
		if n := count(t, st, q, driverID); n != 0 {
			t.Errorf("%s = %d after deletion, want 0", q, n)
		}
	}
	if n := count(t, st, `SELECT COUNT(*) FROM drivers WHERE telegram_chat_id = 777`); n != 0 {
		t.Error("the Telegram link survived")
	}
	if n := count(t, st, `SELECT COUNT(*) FROM audit_log WHERE entity IN ('stay','planned_stay') AND entity_id IN (?, ?) AND after_json IS NOT NULL AND tenant_id = ?`, stay.ID, plan.ID, tenantID); n != 0 {
		t.Errorf("%d audit entries still hold the deleted driver's dates", n)
	}
	if n := count(t, st, `SELECT COUNT(*) FROM audit_log WHERE entity = 'stay' AND entity_id = ? AND after_json IS NOT NULL`, otherStay.ID); n != 1 {
		t.Error("another driver's audit entry was scrubbed")
	}
	if n := count(t, st, `SELECT COUNT(*) FROM stays WHERE driver_id = ?`, other.ID); n != 1 {
		t.Error("another driver's stay was deleted")
	}
	if err := st.DeleteDriverPermanently(ctx, tenantID, driverID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
}

func TestDeleteDriverPermanently_OtherFirmCannot(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	ctx := context.Background()
	mustCreateStay(t, st, tenantID, driverID, day(-20), dayPtr(-10))
	otherFirm, err := st.CreateTenant(ctx, "Druga", "druga.local")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteDriverPermanently(ctx, otherFirm.ID, driverID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("delete by another firm = %v, want ErrNotFound", err)
	}
	if n := count(t, st, `SELECT COUNT(*) FROM stays WHERE driver_id = ?`, driverID); n != 1 {
		t.Error("another firm deleted the driver's stays")
	}
	if _, err := st.ExportDriver(ctx, otherFirm.ID, driverID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("export by another firm = %v, want ErrNotFound", err)
	}
}

func TestExportDriver(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	ctx := context.Background()
	mustCreateStay(t, st, tenantID, driverID, day(-20), dayPtr(-10))
	if _, err := st.CreatePlannedStay(ctx, tenantID, driverID, day(5), day(9), "", nil); err != nil {
		t.Fatal(err)
	}
	ex, err := st.ExportDriver(ctx, tenantID, driverID)
	if err != nil {
		t.Fatal(err)
	}
	if ex.Driver.Name != "Marko Markovic" || len(ex.Stays) != 1 || len(ex.Plans) != 1 || ex.CreatedAt.IsZero() || ex.PrivacyAckAt != nil {
		t.Errorf("export = %+v", ex)
	}
}

func TestTermsAndPrivacyAck(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	ctx := context.Background()

	ts, err := st.GetTermsStatus(ctx, tenantID)
	if err != nil || ts.Version != "" || ts.AcceptedAt != nil {
		t.Fatalf("new firm terms = %+v, %v; want none accepted", ts, err)
	}
	owner, err := st.CreateUser(ctx, tenantID, "vlasnik@test.rs", "hash", "owner", "Vlasnik")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AcceptTerms(ctx, tenantID, owner.ID, "2026-10-15"); err != nil {
		t.Fatal(err)
	}
	ts, _ = st.GetTermsStatus(ctx, tenantID)
	if ts.Version != "2026-10-15" || ts.AcceptedAt == nil || ts.AcceptedBy == nil || *ts.AcceptedBy != owner.ID {
		t.Errorf("after accepting = %+v", ts)
	}
	if _, err := st.GetTermsStatus(ctx, 99999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown firm = %v", err)
	}

	if err := st.SetDriverTelegramChatID(ctx, tenantID, driverID, 555); err != nil {
		t.Fatal(err)
	}
	waiting, err := st.DriversAwaitingPrivacyAck(ctx, tenantID)
	if err != nil || len(waiting) != 1 {
		t.Fatalf("awaiting ack = %v, %v; want the linked driver", waiting, err)
	}
	if err := st.SetDriverPrivacyAck(ctx, tenantID, driverID); err != nil {
		t.Fatal(err)
	}
	if ack, _ := st.DriverPrivacyAck(ctx, tenantID, driverID); ack == nil {
		t.Error("ack not recorded")
	}
	if waiting, _ := st.DriversAwaitingPrivacyAck(ctx, tenantID); len(waiting) != 0 {
		t.Error("driver who confirmed is still awaiting")
	}
	// A new link must be confirmed again.
	if err := st.SetDriverTelegramChatID(ctx, tenantID, driverID, 556); err != nil {
		t.Fatal(err)
	}
	if ack, _ := st.DriverPrivacyAck(ctx, tenantID, driverID); ack != nil {
		t.Error("ack kept across a new Telegram link")
	}
	if err := st.UnlinkDriverTelegram(ctx, tenantID, driverID); err != nil {
		t.Fatal(err)
	}
	if d, _ := st.GetDriver(ctx, tenantID, driverID); d.TelegramChatID != nil {
		t.Error("driver still linked after unlinking")
	}
	if err := st.UnlinkDriverTelegram(ctx, tenantID, 99999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unlink unknown driver = %v", err)
	}
	if _, err := st.DriverPrivacyAck(ctx, tenantID, 99999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("ack of unknown driver = %v", err)
	}
}
