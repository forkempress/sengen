package jobs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"shengen/internal/clock"
	"shengen/internal/db"
	"shengen/internal/store"
	"shengen/internal/telegram"
)

// fakeTelegram records sendMessage calls. While failing is set it answers
// like Telegram does when it is down.
type fakeTelegram struct {
	mu      sync.Mutex
	sent    []sentMessage
	failing bool
}

type sentMessage struct {
	ChatID int64
	Text   string
}

func newFakeTelegram(t *testing.T) *fakeTelegram {
	t.Helper()
	f := &fakeTelegram{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			ChatID int64  `json:"chat_id"`
			Text   string `json:"text"`
		}
		json.Unmarshal(raw, &body)
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if f.failing {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"ok":false,"description":"Internal Server Error"}`)
			return
		}
		f.sent = append(f.sent, sentMessage{body.ChatID, body.Text})
		io.WriteString(w, `{"ok":true,"result":{}}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("TELEGRAM_API_BASE_URL", srv.URL)
	return f
}

func (f *fakeTelegram) setFailing(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failing = v
}

// to returns the texts sent to chatID.
func (f *fakeTelegram) to(chatID int64) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, m := range f.sent {
		if m.ChatID == chatID {
			out = append(out, m.Text)
		}
	}
	return out
}

const (
	alertChat  int64 = 9000
	driverChat int64 = 555
)

// newJobsFixture builds a firm with an alert chat and one driver linked to
// driverChat. The clock is pinned to noon so the 07:00 digest stays quiet.
func newJobsFixture(t *testing.T) (*Runner, *store.Store, *store.Tenant, *store.Driver) {
	t.Helper()
	d := clock.Today()
	noon := time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, clock.Now().Location())
	t.Cleanup(clock.Set(func() time.Time { return noon }))

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	st := store.New(conn)
	ctx := context.Background()
	tn, err := st.CreateTenant(ctx, "Firma", "firma")
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := tn.ParsedSettings()
	settings.TelegramAlertChatID = alertChat
	if err := st.UpdateTenantSettings(ctx, tn.ID, settings); err != nil {
		t.Fatal(err)
	}
	tn, _ = st.GetTenant(ctx, tn.ID)
	driver, err := st.CreateDriver(ctx, tn.ID, "Marko", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetDriverTelegramChatID(ctx, tn.ID, driver.ID, driverChat); err != nil {
		t.Fatal(err)
	}
	r := New(st, telegram.NewClient("TEST-TOKEN"))
	r.lastCleanup = time.Now()
	return r, st, tn, driver
}

func stay(t *testing.T, st *store.Store, tn *store.Tenant, d *store.Driver, entry time.Time, exit *time.Time) {
	t.Helper()
	if _, err := st.CreateStay(context.Background(), tn.ID, d.ID, entry, exit, "work", "manual", "", nil); err != nil {
		t.Fatal(err)
	}
}

// The reminder after a week asks about Schengen, not the EU: Cyprus and
// Ireland are in the EU and do not count.
func TestOpenStayReminder_AsksAboutSchengen(t *testing.T) {
	fake := newFakeTelegram(t)
	r, st, tn, d := newJobsFixture(t)
	stay(t, st, tn, d, clock.Today().AddDate(0, 0, -8), nil)

	r.tick(context.Background())

	var reminder string
	for _, text := range fake.to(driverChat) {
		if strings.Contains(text, "još u") {
			reminder = text
		}
	}
	if !strings.Contains(reminder, "još u Šengenu") || strings.Contains(reminder, "EU") {
		t.Errorf("reminder = %q, want it to ask about Schengen", reminder)
	}
}
