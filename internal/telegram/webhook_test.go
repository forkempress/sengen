package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// SEC-10: Telegram redelivers an update it thinks failed. The second copy
// of the same update_id must not be handled again.
func TestHandleWebhook_DuplicateUpdateIDIsSkipped(t *testing.T) {
	fake := newFakeTelegram(t)
	bot, _ := newTestBot(t)

	body := textUpdate(4242, "zdravo") // unknown chat: the bot answers once per handled update
	for i := 0; i < 3; i++ {
		if got := postWebhook(t, bot, testSecret, body).Code; got != http.StatusOK {
			t.Fatalf("delivery %d: status %d, want 200 (a duplicate is still acknowledged)", i+1, got)
		}
	}
	if texts := fake.texts(); len(texts) != 1 {
		t.Errorf("same update delivered 3 times produced %d replies, want 1: %v", len(texts), texts)
	}

	postWebhook(t, bot, testSecret, textUpdate(4242, "zdravo"))
	if texts := fake.texts(); len(texts) != 2 {
		t.Errorf("a new update_id must still be handled; replies = %d, want 2", len(texts))
	}
}

// SEC-10: the webhook answers 200 before the update is handled, so a slow
// Telegram API call no longer makes Telegram retry.
func TestHandleWebhook_RespondsBeforeHandling(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		io.WriteString(w, `{"ok":true,"result":{}}`)
	}))
	t.Cleanup(api.Close)
	t.Setenv("TELEGRAM_API_BASE_URL", api.URL)
	bot, _ := newTestBot(t)

	r := httptest.NewRequest(http.MethodPost, "/telegram/webhook", strings.NewReader(textUpdate(4243, "zdravo")))
	r.Header.Set("X-Telegram-Bot-Api-Secret-Token", testSecret)
	w := httptest.NewRecorder()
	returned := make(chan struct{})
	go func() { bot.HandleWebhook(w, r); close(returned) }()

	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("webhook did not answer while the Telegram API call was still hanging")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if bot.Wait(ctx) == nil {
		t.Error("update finished although the API call is still blocked; it was handled inside the request")
	}
	once.Do(func() { close(release) })
	if err := bot.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// Updates from one chat are handled in the order they arrived, even though
// handling is asynchronous; different chats do not wait for each other.
func TestUpdateQueue_KeepsOrderPerChat(t *testing.T) {
	var mu sync.Mutex
	got := map[int64][]string{}
	q := newUpdateQueue(func(ctx context.Context, upd Update) {
		time.Sleep(time.Millisecond)
		mu.Lock()
		got[upd.Message.Chat.ID] = append(got[upd.Message.Chat.ID], upd.Message.Text)
		mu.Unlock()
	})
	for i := 0; i < 20; i++ {
		for _, chat := range []int64{1, 2} {
			q.enqueue(Update{Message: &Message{Chat: Chat{ID: chat}, Text: fmt.Sprint(i)}})
		}
	}
	if err := q.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, chat := range []int64{1, 2} {
		b, _ := json.Marshal(got[chat])
		want := `["0","1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17","18","19"]`
		if string(b) != want {
			t.Errorf("chat %d handled in order %s", chat, b)
		}
	}
}
