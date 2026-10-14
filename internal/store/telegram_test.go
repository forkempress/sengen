package store_test

import (
	"context"
	"sync"
	"testing"
	"time"
)

// SEC-11: under concurrent webhook deliveries one token must be redeemed
// exactly once.
func TestConsumeLinkToken_OnlyOnceUnderConcurrency(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	ctx := context.Background()

	for round := 0; round < 20; round++ {
		token := "tok-" + time.Now().Format("150405.000000000")
		if err := st.CreateDriverLinkToken(ctx, token, tenantID, driverID, time.Hour); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		ok := 0
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := st.ConsumeLinkToken(ctx, token); err == nil {
					mu.Lock()
					ok++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		if ok != 1 {
			t.Fatalf("round %d: token redeemed %d times, want 1", round, ok)
		}
	}
}

func TestConsumeLinkToken_ExpiredAndUnknown(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	ctx := context.Background()
	if err := st.CreateDriverLinkToken(ctx, "old", tenantID, driverID, -time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConsumeLinkToken(ctx, "old"); err == nil {
		t.Error("expired token was accepted")
	}
	if _, err := st.ConsumeLinkToken(ctx, "nema-ga"); err == nil {
		t.Error("unknown token was accepted")
	}
}

// SEC-11: issuing a new link for the same driver or user voids the old ones,
// so only the last link sent out works.
func TestCreateLinkToken_VoidsEarlierTokens(t *testing.T) {
	st, tenantID, driverID := newTestStore(t)
	ctx := context.Background()

	if err := st.CreateDriverLinkToken(ctx, "prvi", tenantID, driverID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateDriverLinkToken(ctx, "drugi", tenantID, driverID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConsumeLinkToken(ctx, "prvi"); err == nil {
		t.Error("earlier driver token still works after a new one was issued")
	}
	if _, err := st.ConsumeLinkToken(ctx, "drugi"); err != nil {
		t.Errorf("newest driver token refused: %v", err)
	}

	u, err := st.CreateUser(ctx, tenantID, "a@b.rs", "hash", "owner", "Vlasnik")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateUserLinkToken(ctx, "u1", tenantID, u.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateUserLinkToken(ctx, "u2", tenantID, u.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ConsumeLinkToken(ctx, "u1"); err == nil {
		t.Error("earlier user token still works after a new one was issued")
	}
}

func TestMarkTelegramUpdate_OnlyFirstDeliveryIsFresh(t *testing.T) {
	st, _, _ := newTestStore(t)
	ctx := context.Background()
	first, err := st.MarkTelegramUpdate(ctx, 1001)
	if err != nil || !first {
		t.Fatalf("first delivery: fresh=%v err=%v, want fresh", first, err)
	}
	again, err := st.MarkTelegramUpdate(ctx, 1001)
	if err != nil || again {
		t.Errorf("redelivery: fresh=%v err=%v, want not fresh", again, err)
	}
	n, err := st.DeleteTelegramUpdatesBefore(ctx, time.Now().Add(time.Minute))
	if err != nil || n != 1 {
		t.Errorf("cleanup deleted %d rows (err %v), want 1", n, err)
	}
	if fresh, _ := st.MarkTelegramUpdate(ctx, 1001); !fresh {
		t.Error("after cleanup the id is forgotten")
	}
}

func TestReleaseAlert_FreesTheSlot(t *testing.T) {
	st, tn, driverID := newTestStore(t)
	ctx := context.Background()
	if ok, err := st.TryRecordAlert(ctx, tn, driverID, "threshold_red", "2026-10-14"); err != nil || !ok {
		t.Fatalf("first claim = %v, %v", ok, err)
	}
	if err := st.ReleaseAlert(ctx, tn, driverID, "threshold_red", "2026-10-14"); err != nil {
		t.Fatal(err)
	}
	if ok, err := st.TryRecordAlert(ctx, tn, driverID, "threshold_red", "2026-10-14"); err != nil || !ok {
		t.Errorf("claim after release = %v, %v, want true", ok, err)
	}
}
