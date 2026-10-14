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

func requestWithCF(remoteAddr, cfIP, xff string) *http.Request {
	r := requestFrom(remoteAddr, xff)
	if cfIP != "" {
		r.Header.Set("CF-Connecting-IP", cfIP)
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
			if got := ClientIP(requestFrom(tc.remoteAddr, tc.xff), ProxyCaddy); got != tc.want {
				t.Errorf("ClientIP() = %q, want %q\n  razlog: %s", got, tc.want, tc.why)
			}
		})
	}
}

// The whole point of the change: two different clients behind the same proxy
// must land in different rate-limit buckets, and one attacker must not be
// able to escape their bucket by forging the header.
func TestClientIP_SeparatesClientsBehindProxy(t *testing.T) {
	first := ClientIP(requestFrom("127.0.0.1:1111", "203.0.113.7"), ProxyCaddy)
	second := ClientIP(requestFrom("127.0.0.1:2222", "203.0.113.8"), ProxyCaddy)
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
		keys[ClientIP(requestFrom("127.0.0.1:3333", xff), ProxyCaddy)] = true
	}
	if len(keys) != 1 {
		t.Errorf("attacker produced %d distinct rate-limit keys (%v), want 1", len(keys), keys)
	}
}

// Behind a Cloudflare tunnel the app sees cloudflared on loopback, and
// Cloudflare guarantees CF-Connecting-IP holds the real visitor address
// (it strips any copy the visitor tried to send).
func TestClientIP_Cloudflare(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		cfIP       string
		xff        string
		want       string
		why        string
	}{
		{
			name:       "cloudflared on loopback",
			remoteAddr: "127.0.0.1:51234",
			cfIP:       "203.0.113.7",
			xff:        "203.0.113.7",
			want:       "203.0.113.7",
			why:        "the dani90.rs tunnel setup",
		},
		{
			name:       "CF header wins over an XFF that ends in a proxy hop",
			remoteAddr: "127.0.0.1:51234",
			cfIP:       "203.0.113.7",
			xff:        "203.0.113.7, 127.0.0.1",
			want:       "203.0.113.7",
			why:        "if cloudflared appends its own hop, XFF-last would collapse everyone onto one bucket",
		},
		{
			name:       "untrusted peer cannot fake the CF header",
			remoteAddr: "198.51.100.9:51234",
			cfIP:       "203.0.113.7",
			want:       "198.51.100.9",
			why:        "only the loopback proxy is trusted to set it",
		},
		{
			name:       "junk CF header does not fall through to XFF",
			remoteAddr: "127.0.0.1:51234",
			cfIP:       "not-an-ip",
			xff:        "203.0.113.7",
			want:       "127.0.0.1",
			why:        "Cloudflare does not promise anything about XFF; a malformed value must not become a rate-limit key",
		},
		{
			name:       "junk CF header and no XFF falls back to peer",
			remoteAddr: "127.0.0.1:51234",
			cfIP:       "not-an-ip",
			want:       "127.0.0.1",
			why:        "nothing trustworthy left to use",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClientIP(requestWithCF(tc.remoteAddr, tc.cfIP, tc.xff), ProxyCloudflare); got != tc.want {
				t.Errorf("ClientIP() = %q, want %q\n  razlog: %s", got, tc.want, tc.why)
			}
		})
	}
}

// SEC-06: Caddy passes the client's CF-Connecting-IP through untouched, so
// behind Caddy that header is attacker input and must not decide the key.
func TestClientIP_CaddyIgnoresForgedCFHeader(t *testing.T) {
	keys := make(map[string]bool)
	for _, forged := range []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"} {
		keys[ClientIP(requestWithCF("127.0.0.1:1", forged, "203.0.113.7"), ProxyCaddy)] = true
	}
	if len(keys) != 1 || !keys["203.0.113.7"] {
		t.Errorf("behind Caddy forged CF-Connecting-IP produced keys %v, want only the XFF address", keys)
	}
}

// With no proxy configured neither header is trusted, even from loopback.
func TestClientIP_NoProxyTrustsNoHeader(t *testing.T) {
	r := requestWithCF("127.0.0.1:1", "1.1.1.1", "2.2.2.2")
	if got := ClientIP(r, ProxyNone); got != "127.0.0.1" {
		t.Errorf("ClientIP(none) = %q, want the peer 127.0.0.1", got)
	}
}

func TestParseTrustedProxy(t *testing.T) {
	for in, want := range map[string]TrustedProxy{"": ProxyNone, "none": ProxyNone, "Cloudflare": ProxyCloudflare, " caddy ": ProxyCaddy} {
		if got, err := ParseTrustedProxy(in); err != nil || got != want {
			t.Errorf("ParseTrustedProxy(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseTrustedProxy("nginx"); err == nil {
		t.Error("unknown proxy must be rejected")
	}
}
