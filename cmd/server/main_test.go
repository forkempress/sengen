package main

import (
	"net/http"
	"testing"
)

// SEC-14: without timeouts a slow client holds a connection forever.
func TestNewHTTPServer_HasTimeouts(t *testing.T) {
	srv := newHTTPServer("127.0.0.1:8080", http.NotFoundHandler())
	for name, d := range map[string]int64{
		"ReadHeaderTimeout": int64(srv.ReadHeaderTimeout),
		"ReadTimeout":       int64(srv.ReadTimeout),
		"WriteTimeout":      int64(srv.WriteTimeout),
		"IdleTimeout":       int64(srv.IdleTimeout),
	} {
		if d <= 0 {
			t.Errorf("%s is not set", name)
		}
	}
}
