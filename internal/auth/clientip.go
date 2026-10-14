package auth

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP returns the IP address to attribute a request to, used as the
// rate-limit key.
//
// In production Caddy terminates TLS on the same host and proxies to
// localhost, so r.RemoteAddr is always 127.0.0.1 and would collapse every
// visitor onto one limiter bucket. The real address therefore has to come
// from X-Forwarded-For — but only when the immediate peer is the loopback
// proxy. A request arriving from anywhere else may have set that header
// itself, and trusting it there would let an attacker forge a fresh identity
// per attempt and bypass the limit entirely.
//
// With exactly one trusted proxy in front, the trustworthy element is the
// LAST one in the list: Caddy appends the address it actually saw, after any
// values the client may have sent. Reading the first element instead — the
// common mistake — reads attacker-controlled input.
//
// Behind Cloudflare (tunnel or proxied DNS) CF-Connecting-IP is preferred:
// Cloudflare always sets it to the true client address and strips any copy
// the client sent, whereas the exact shape of X-Forwarded-For depends on
// whether cloudflared appends its own hop.
func ClientIP(r *http.Request) string {
	peer := remoteIP(r.RemoteAddr)

	if !isLoopback(peer) {
		return peer
	}

	if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); net.ParseIP(cf) != nil {
		return cf
	}

	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return peer
	}
	parts := strings.Split(xff, ",")
	last := strings.TrimSpace(parts[len(parts)-1])
	if net.ParseIP(last) == nil {
		return peer
	}
	return last
}

// remoteIP strips the port from a RemoteAddr ("1.2.3.4:5678",
// "[::1]:5678"), falling back to the raw value if it has no port.
func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func isLoopback(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.IsLoopback()
}
