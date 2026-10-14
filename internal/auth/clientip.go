package auth

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// TrustedProxy says which reverse proxy sits in front of the app, and so
// which header (if any) carries the real client address. It is set from
// TRUSTED_PROXY and must match the deploy: trusting a header the proxy does
// not overwrite lets a client pick a fresh address on every request.
type TrustedProxy string

const (
	// ProxyNone: the app is reached directly. Only RemoteAddr counts.
	ProxyNone TrustedProxy = "none"
	// ProxyCloudflare: cloudflared on loopback. Cloudflare sets
	// CF-Connecting-IP to the visitor's address and strips any copy the
	// visitor sent.
	ProxyCloudflare TrustedProxy = "cloudflare"
	// ProxyCaddy: Caddy on loopback, appending the address it saw to
	// X-Forwarded-For. Caddy passes CF-Connecting-IP through untouched, so
	// that header is attacker-controlled here and is ignored.
	ProxyCaddy TrustedProxy = "caddy"
)

// ParseTrustedProxy validates a TRUSTED_PROXY value; empty means none.
func ParseTrustedProxy(v string) (TrustedProxy, error) {
	switch p := TrustedProxy(strings.ToLower(strings.TrimSpace(v))); p {
	case "":
		return ProxyNone, nil
	case ProxyNone, ProxyCloudflare, ProxyCaddy:
		return p, nil
	default:
		return "", fmt.Errorf("TRUSTED_PROXY mora biti cloudflare, caddy ili none (dobijeno %q)", v)
	}
}

// ClientIP returns the IP address to attribute a request to, used as the
// rate-limit key.
//
// Behind a proxy on the same host r.RemoteAddr is always loopback and would
// collapse every visitor onto one limiter bucket, so the real address has to
// come from the proxy's header — but only when the immediate peer is the
// loopback proxy, and only the header that proxy actually controls.
//
// For Caddy the trustworthy X-Forwarded-For element is the LAST one: Caddy
// appends the address it actually saw, after any values the client may have
// sent. Reading the first element instead — the common mistake — reads
// attacker-controlled input.
func ClientIP(r *http.Request, proxy TrustedProxy) string {
	peer := remoteIP(r.RemoteAddr)

	if !isLoopback(peer) {
		return peer
	}

	switch proxy {
	case ProxyCloudflare:
		if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); net.ParseIP(cf) != nil {
			return cf
		}
	case ProxyCaddy:
		xff := r.Header.Get("X-Forwarded-For")
		if xff == "" {
			return peer
		}
		parts := strings.Split(xff, ",")
		if last := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(last) != nil {
			return last
		}
	}
	return peer
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
