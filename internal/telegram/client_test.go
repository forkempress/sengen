package telegram

import (
	"context"
	"net"
	"strings"
	"testing"
)

// A failed HTTP request's error contains the request URL, and the Bot API
// puts the bot token in the URL. Whoever holds the token controls the bot, so
// it must never reach a log line or a page.
func TestClient_ErrorsNeverContainTheToken(t *testing.T) {
	// A port nothing listens on: the request fails at the network level, the
	// case where Go's error embeds the full URL.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	const token = "123456789:AAH-tajni-token-bota"
	c := &Client{BaseURL: "http://" + addr, Token: token, HTTP: NewClient(token).HTTP}

	checks := map[string]error{
		"SendMessage": c.SendMessage(context.Background(), 1, "x", nil),
		"SetWebhook":  c.SetWebhook(context.Background(), "https://x", "s"),
	}
	_, checks["DownloadDocument"] = c.DownloadDocument(context.Background(), "f")

	for name, err := range checks {
		if err == nil {
			t.Fatalf("%s: expected a network error", name)
		}
		if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "AAH-tajni") {
			t.Errorf("%s error leaks the bot token: %v", name, err)
		}
		if !strings.Contains(err.Error(), "<token>") {
			t.Errorf("%s error lost its context entirely: %v", name, err)
		}
	}
}
