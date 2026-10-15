package store_test

import (
	"context"
	"errors"
	"testing"

	"shengen/internal/store"
)

func TestPlannedStay_CRUD(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	ctx := context.Background()

	p, err := st.CreatePlannedStay(ctx, tenantID, driverID, day(10), day(19), "Minhen", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	second, err := st.CreatePlannedStay(ctx, tenantID, driverID, day(3), day(4), "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	list, err := st.ListPlannedStaysByDriver(ctx, tenantID, driverID)
	if err != nil || len(list) != 2 || list[0].ID != second.ID {
		t.Fatalf("list = %+v, %v; want both plans, earliest first", list, err)
	}
	if !list[1].From.Equal(day(10)) || !list[1].To.Equal(day(19)) || list[1].Note != "Minhen" {
		t.Errorf("stored plan = %+v", list[1])
	}

	if err := st.UpdatePlannedStay(ctx, tenantID, p.ID, day(11), day(20), "Beč"); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := st.GetPlannedStay(ctx, tenantID, p.ID)
	if err != nil || !got.From.Equal(day(11)) || got.Note != "Beč" {
		t.Errorf("after update = %+v, %v", got, err)
	}

	from, err := st.ListPlannedStaysFrom(ctx, tenantID, day(5))
	if err != nil || len(from) != 1 || from[0].ID != p.ID {
		t.Errorf("plans ending on or after day 5 = %+v, %v; want only the later one", from, err)
	}

	if err := st.DeletePlannedStay(ctx, tenantID, p.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.GetPlannedStay(ctx, tenantID, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("deleted plan still found: %v", err)
	}
	if err := st.DeletePlannedStay(ctx, tenantID, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
}

func TestPlannedStay_Validation(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name     string
		from, to int
	}{
		{"u prošlosti", -1, 5},
		{"povratak pre polaska", 10, 9},
		{"duže od 90 dana", 1, 91},
	} {
		_, err := st.CreatePlannedStay(ctx, tenantID, driverID, day(tc.from), day(tc.to), "", nil)
		assertValidationError(t, err, tc.name)
	}
	if _, err := st.CreatePlannedStay(ctx, tenantID, driverID, day(0), day(89), "", nil); err != nil {
		t.Errorf("90-day plan from today refused: %v", err)
	}
	_, err := st.CreatePlannedStay(ctx, tenantID, driverID, day(1), day(2), string(make([]byte, store.MaxNoteLen+1)), nil)
	assertValidationError(t, err, "predugačka napomena")
	_, err = st.CreatePlannedStay(ctx, tenantID, 999999, day(1), day(2), "", nil)
	assertValidationError(t, err, "nepoznat vozač")
}

// A plan under way keeps its start in the past when only its return moves;
// moving the start into the past is refused.
func TestPlannedStay_UpdateOfAPlanUnderWay(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	ctx := context.Background()
	res, err := st.DB.ExecContext(ctx,
		`INSERT INTO planned_stays (tenant_id, driver_id, start_date, end_date) VALUES (?, ?, ?, ?)`,
		tenantID, driverID, day(-3).Format("2006-01-02"), day(4).Format("2006-01-02"))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()

	if err := st.UpdatePlannedStay(ctx, tenantID, id, day(-3), day(8), ""); err != nil {
		t.Errorf("moving the return of a plan under way: %v", err)
	}
	assertValidationError(t, st.UpdatePlannedStay(ctx, tenantID, id, day(-5), day(8), ""), "polazak pomeren u prošlost")
	assertValidationError(t, st.UpdatePlannedStay(ctx, tenantID, id, day(-3), day(-1), ""), "povratak u prošlosti")
	if err := st.UpdatePlannedStay(ctx, tenantID, 999999, day(1), day(2), ""); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("update of a missing plan = %v, want ErrNotFound", err)
	}
}

// The database itself refuses a plan for another firm's driver and a plan
// longer than 90 days.
func TestPlannedStay_DatabaseConstraints(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	ctx := context.Background()
	other, err := st.CreateTenant(ctx, "Druga", "druga.local")
	if err != nil {
		t.Fatal(err)
	}
	ins := `INSERT INTO planned_stays (tenant_id, driver_id, start_date, end_date) VALUES (?, ?, ?, ?)`
	if _, err := st.DB.ExecContext(ctx, ins, other.ID, driverID, "2027-01-01", "2027-01-05"); err == nil {
		t.Error("database accepted a plan whose driver belongs to another firm")
	}
	if _, err := st.DB.ExecContext(ctx, ins, tenantID, driverID, "2027-01-01", "2027-04-01"); err == nil {
		t.Error("database accepted a plan longer than 90 days")
	}
	if _, err := st.DB.ExecContext(ctx, ins, tenantID, driverID, "2027-01-05", "2027-01-01"); err == nil {
		t.Error("database accepted a plan that ends before it starts")
	}
}

func TestListRecentAudit_OnlyTheAskedEntity(t *testing.T) {
	st, tenantID, _ := newTestStore(t)
	ctx := context.Background()
	st.RecordAudit(ctx, tenantID, "user", nil, "stay", 1, "create", nil, nil)
	st.RecordAudit(ctx, tenantID, "user", nil, "planned_stay", 2, "create", nil, nil)
	st.RecordAudit(ctx, tenantID, "user", nil, "user", 3, "create", nil, nil)

	got, err := st.ListRecentAudit(ctx, tenantID, "stay", 5)
	if err != nil || len(got) != 1 || got[0].EntityID != 1 {
		t.Errorf("stay audit = %+v, %v; want only the stay entry", got, err)
	}
}
