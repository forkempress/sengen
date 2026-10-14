package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func requestFrom(remoteAddr, xff string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/login", nil)
	r.RemoteAddr = remoteAddr
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	return r
}

func TestClientIP(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		xff        string
		want       string
		why        string
	}{
		{
			name:       "direct connection, no proxy header",
			remoteAddr: "203.0.113.7:51234",
			want:       "203.0.113.7",
			why:        "local dev / direct exposure: RemoteAddr is the client",
		},
		{
			name:       "behind Caddy on loopback",
			remoteAddr: "127.0.0.1:51234",
			xff:        "203.0.113.7",
			want:       "203.0.113.7",
			why:        "the production path: real client comes from the proxy header",
		},
		{
			name:       "behind Caddy over IPv6 loopback",
			remoteAddr: "[::1]:51234",
			xff:        "203.0.113.7",
			want:       "203.0.113.7",
			why:        "::1 is loopback just like 127.0.0.1",
		},
		{
			name:       "client forged a prefix in X-Forwarded-For",
			remoteAddr: "127.0.0.1:51234",
			xff:        "1.1.1.1, 203.0.113.7",
			want:       "203.0.113.7",
			why:        "Caddy appends the address it saw; the last entry is the trustworthy one",
		},
		{
			name:       "forged header with many entries",
			remoteAddr: "127.0.0.1:51234",
			xff:        "9.9.9.9, 8.8.8.8, 1.1.1.1, 203.0.113.7",
			want:       "203.0.113.7",
			why:        "an attacker cannot push their real address out of the last slot",
		},
		{
			name:       "untrusted peer sending X-Forwarded-For",
			remoteAddr: "198.51.100.9:51234",
			xff:        "203.0.113.7",
			want:       "198.51.100.9",
			why:        "header must be ignored when the peer is not the loopback proxy",
		},
		{
			name:       "loopback peer with junk in the header",
			remoteAddr: "127.0.0.1:51234",
			xff:        "not-an-ip",
			want:       "127.0.0.1",
			why:        "unparseable value falls back to the peer rather than keying on garbage",
		},
		{
			name:       "loopback peer with empty header value",
			remoteAddr: "127.0.0.1:51234",
			xff:        "   ",
			want:       "127.0.0.1",
			why:        "whitespace-only header is not an address",
		},
		{
			name:       "RemoteAddr without a port",
			remoteAddr: "203.0.113.7",
			want:       "203.0.113.7",
			why:        "defensive: some servers hand over a bare address",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClientIP(requestFrom(tc.remoteAddr, tc.xff)); got != tc.want {
				t.Errorf("ClientIP() = %q, want %q\n  razlog: %s", got, tc.want, tc.why)
			}
		})
	}
}

// The whole point of the change: two different clients behind the same proxy
// must land in different rate-limit buckets, and one attacker must not be
// able to escape their bucket by forging the header.
func TestClientIP_SeparatesClientsBehindProxy(t *testing.T) {
	first := ClientIP(requestFrom("127.0.0.1:1111", "203.0.113.7"))
	second := ClientIP(requestFrom("127.0.0.1:2222", "203.0.113.8"))
	if first == second {
		t.Errorf("two different clients behind the proxy share the key %q", first)
	}

	attacker := "198.51.100.50"
	forged := []string{
		attacker,
		"10.0.0.1, " + attacker,
		"1.1.1.1, 2.2.2.2, " + attacker,
	}
	keys := make(map[string]bool)
	for _, xff := range forged {
		keys[ClientIP(requestFrom("127.0.0.1:3333", xff))] = true
	}
	if len(keys) != 1 {
		t.Errorf("attacker produced %d distinct rate-limit keys (%v), want 1", len(keys), keys)
	}
}
