package superadmin

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"shengen/internal/store"
)

// docImportWindow: a scan read (or confirmed) this recently is reported.
const docImportWindow = 24 * time.Hour

// checkDocImports reports each scan read with Claude (what it cost, what
// was found) and, separately, each one the firm then confirmed.
func (n *Notifier) checkDocImports(ctx context.Context, now time.Time) {
	reads, err := n.Store.ListDocumentImportsSince(ctx, now.Add(-docImportWindow))
	if err != nil {
		log.Printf("superadmin: list document imports: %v", err)
		return
	}
	for _, d := range reads {
		month, err := n.Store.DocumentImportSpendSince(ctx, d.TenantID, monthStart(now))
		if err != nil {
			log.Printf("superadmin: document import spend: %v", err)
			continue
		}
		n.notify(ctx, "doc_import", strconv.FormatInt(d.ID, 10), docImportReadMsg(d, month))
	}

	confirmed, err := n.Store.ListDocumentImportsConfirmedSince(ctx, now.Add(-docImportWindow))
	if err != nil {
		log.Printf("superadmin: list confirmed document imports: %v", err)
		return
	}
	for _, d := range confirmed {
		n.notify(ctx, "doc_import_confirmed", strconv.FormatInt(d.ID, 10),
			fmt.Sprintf("✅ Uvoz dokumenta upisan: #%d %s — %s za %s.",
				d.TenantID, d.TenantName, plural(d.StaysWritten, "boravak", "boravka", "boravaka"),
				plural(d.DriversWritten, "vozača", "vozača", "vozača")))
	}
}

func docImportReadMsg(d store.DocumentImport, month store.DocumentImportSpend) string {
	var b strings.Builder
	switch d.Status {
	case store.DocImportOK:
		fmt.Fprintf(&b, "📄 Uvoz dokumenta: #%d %s — pročitano %s za %s, %d ispravno. Čeka potvrdu.",
			d.TenantID, d.TenantName, plural(d.StaysRead, "boravak", "boravka", "boravaka"),
			plural(d.DriversRead, "vozača", "vozača", "vozača"), d.RowsValid)
	case store.DocImportEmpty:
		fmt.Fprintf(&b, "📄 Uvoz dokumenta: #%d %s — nije nađen nijedan boravak.", d.TenantID, d.TenantName)
	case store.DocImportTooLarge:
		fmt.Fprintf(&b, "📄 Uvoz dokumenta: #%d %s — prevelik (%s za %s), ponuđeno je slanje mejlom.",
			d.TenantID, d.TenantName, plural(d.StaysRead, "boravak", "boravka", "boravaka"),
			plural(d.DriversRead, "vozača", "vozača", "vozača"))
	default:
		fmt.Fprintf(&b, "❌ Uvoz dokumenta nije uspeo: #%d %s — greška pri čitanju (detalji u logu servera).", d.TenantID, d.TenantName)
	}
	fmt.Fprintf(&b, "\nTrošak: %s", formatUSD(d.CostMicroUSD))
	if d.Model != "" {
		fmt.Fprintf(&b, " (%s, %s + %s tokena)", d.Model, groupThousands(d.InputTokens), groupThousands(d.OutputTokens))
	}
	fmt.Fprintf(&b, ". Firma ovog meseca: %s (%s).", formatUSD(month.CostMicroUSD),
		plural(month.Count, "dokument", "dokumenta", "dokumenata"))
	return b.String()
}

// monthStart is the first day of now's month, at midnight in now's zone.
func monthStart(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
}

// formatUSD shows a cost to a tenth of a cent — one document costs cents.
func formatUSD(micro int64) string {
	tenths := (micro + 500) / 1000 // in thousandths of a dollar
	return fmt.Sprintf("$%d,%03d", tenths/1000, tenths%1000)
}

func groupThousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// plural picks the Serbian form for n: 1 boravak, 2 boravka, 5 boravaka.
func plural(n int, one, few, many string) string {
	form := many
	if n%10 == 1 && n%100 != 11 {
		form = one
	} else if n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14) {
		form = few
	}
	return strconv.Itoa(n) + " " + form
}
