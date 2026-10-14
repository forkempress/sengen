package web

import (
	"fmt"
	"html/template"
	"strings"

	"rsc.io/qr"
)

// qrSVG draws text as a QR code in inline SVG, so a driver can scan the
// link from the dispatcher's screen. It is generated here, not by an
// outside service: the link carries a one-time token that must not leave
// the server (rsc.io/qr, BSD, no dependencies). Returns "" on failure; the
// link itself is always shown next to it.
func qrSVG(text string) template.HTML {
	if text == "" {
		return ""
	}
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return ""
	}
	const quiet = 4 // white border the scanners need, in modules
	n := code.Size + 2*quiet
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="qr" viewBox="0 0 %d %d" shape-rendering="crispEdges" role="img" aria-label="QR kod linka za povezivanje">`, n, n)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#fff"/><path fill="#16202B" d="`, n, n)
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return template.HTML(b.String())
}
