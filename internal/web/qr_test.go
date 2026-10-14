package web

import (
	"strings"
	"testing"
)

func TestQRSVG_DrawsLinkLocally(t *testing.T) {
	got := string(qrSVG("https://t.me/Dani90Bot?start=abcdefghijklmnopqrstuvwxyz012345"))
	if !strings.HasPrefix(got, `<svg class="qr"`) || !strings.Contains(got, "h1v1h-1z") {
		t.Fatalf("qrSVG did not draw a code: %.80q", got)
	}
	if strings.Contains(got, "http") {
		t.Error("the SVG must not reference any outside resource")
	}
	if qrSVG("") != "" {
		t.Error("empty text must give no code")
	}
}
