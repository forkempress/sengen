package telegram

import (
	"context"
	"errors"
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
	default:
		return "", errors.New("nepoznata akcija")
	}
}
