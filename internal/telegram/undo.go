package telegram

import (
	"context"
	"errors"

	"shengen/internal/clock"
)

// performUndo reverses a stay create/close action — shared by the driver's
// time-limited inline "Poništi" button and the admin's /ispravi command
// (which has no time limit). Callers are responsible for any authorization
// or time-window check before calling this.
func (b *Bot) performUndo(ctx context.Context, tenantID, stayID int64, kind, actorType string, actorID *int64) (string, error) {
	st, err := b.Store.GetStay(ctx, tenantID, stayID)
	if err != nil {
		return "", errors.New("unos više ne postoji")
	}
	switch kind {
	case "create":
		if err := b.Store.DeleteStay(ctx, tenantID, st.ID); err != nil {
			return "", err
		}
		b.Store.RecordAudit(ctx, tenantID, actorType, actorID, "stay", st.ID, "delete", nil, nil)
		return "Poništen je izlazak iz Srbije.", nil
	case "close":
		if err := b.Store.UpdateStay(ctx, tenantID, st.ID, st.Entry, nil, st.Type, st.Note); err != nil {
			return "", err
		}
		b.Store.RecordAudit(ctx, tenantID, actorType, actorID, "stay", st.ID, "reopen", nil, nil)
		return "Poništen je povratak u Srbiju — unos je ponovo otvoren.", nil
	case "reopen":
		// Only offered right after a same-day re-entry, so the stay closes
		// again on today's date.
		today := clock.Today()
		if err := b.Store.UpdateStay(ctx, tenantID, st.ID, st.Entry, &today, st.Type, st.Note); err != nil {
			return "", err
		}
		b.Store.RecordAudit(ctx, tenantID, actorType, actorID, "stay", st.ID, "close", nil, nil)
		return "Poništen je ponovni ulazak — boravak je zatvoren danas.", nil
	default:
		return "", errors.New("nepoznata akcija")
	}
}
