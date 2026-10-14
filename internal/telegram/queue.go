package telegram

import (
	"context"
	"log"
	"sync"
	"time"
)

// processTimeout bounds one update's handling once the webhook has already
// answered Telegram: a hung API call must not pin a chat's queue forever.
const processTimeout = 2 * time.Minute

// updateQueue runs updates off the HTTP request, one chat at a time and in
// arrival order: a driver tapping two buttons quickly must see them handled
// in that order (multi-step flows depend on it), while a slow chat does not
// hold up any other.
type updateQueue struct {
	mu     sync.Mutex
	chats  map[int64][]Update // chat -> waiting updates; present while a worker runs
	wg     sync.WaitGroup
	handle func(ctx context.Context, upd Update)
}

func newUpdateQueue(handle func(ctx context.Context, upd Update)) *updateQueue {
	return &updateQueue{chats: make(map[int64][]Update), handle: handle}
}

func (q *updateQueue) enqueue(upd Update) {
	chat := updateChatID(upd)
	q.mu.Lock()
	defer q.mu.Unlock()
	waiting, running := q.chats[chat]
	q.chats[chat] = append(waiting, upd)
	if !running {
		q.wg.Add(1)
		go q.drain(chat)
	}
}

func (q *updateQueue) drain(chat int64) {
	defer q.wg.Done()
	for {
		q.mu.Lock()
		waiting := q.chats[chat]
		if len(waiting) == 0 {
			delete(q.chats, chat)
			q.mu.Unlock()
			return
		}
		upd := waiting[0]
		q.chats[chat] = waiting[1:]
		q.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), processTimeout)
		q.handle(ctx, upd)
		cancel()
	}
}

// wait blocks until every queued update has been handled or ctx ends.
func (q *updateQueue) wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() { q.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		log.Printf("telegram: shutdown before all updates were handled")
		return ctx.Err()
	}
}

// updateChatID is the chat an update belongs to, for ordering; updates
// without one share chat 0.
func updateChatID(upd Update) int64 {
	switch {
	case upd.CallbackQuery != nil && upd.CallbackQuery.Message != nil:
		return upd.CallbackQuery.Message.Chat.ID
	case upd.Message != nil:
		return upd.Message.Chat.ID
	case upd.EditedMessage != nil:
		return upd.EditedMessage.Chat.ID
	}
	return 0
}
