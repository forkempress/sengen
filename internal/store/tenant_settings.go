package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// TenantSettings holds what each firm configures for itself. The Telegram
// bot is shared by all firms (token and webhook secret come from the
// environment), so only the firm's own alert chat lives here.
//
// Older rows may still carry telegram_bot_token / telegram_webhook_secret
// from when each firm ran its own bot; encoding/json ignores unknown keys,
// and the next save drops them.
type TenantSettings struct {
	TelegramAlertChatID int64 `json:"telegram_alert_chat_id,omitempty"`
	ThresholdRedDays    int   `json:"threshold_red_days,omitempty"`    // default 15
	ThresholdOrangeDays int   `json:"threshold_orange_days,omitempty"` // default 30
	// DocReadModel reads this firm's scans; empty is the server's default.
	// Set by the operators (cmd/seed -doc-model), not by the firm.
	DocReadModel string `json:"doc_read_model,omitempty"`
	// CalendarNumber is the number the driver calendar shows in each day
	// by default: CalendarRemaining (also when empty) or CalendarTrip.
	CalendarNumber string `json:"calendar_number,omitempty"`
	// TypicalTripDays is how long the firm's usual trip is; the calendar
	// colors a day by whether such a trip still fits. Zero means the default.
	TypicalTripDays int `json:"typical_trip_days,omitempty"`
}

// DefaultTypicalTripDays is the usual trip when the firm has not set one.
const DefaultTypicalTripDays = 9

// TypicalTrip is TypicalTripDays with the default filled in.
func (s TenantSettings) TypicalTrip() int {
	if s.TypicalTripDays > 0 {
		return s.TypicalTripDays
	}
	return DefaultTypicalTripDays
}

const (
	CalendarRemaining = "remaining" // days left on that day
	CalendarTrip      = "trip"      // longest trip starting that day
)

// CalendarMode is CalendarNumber with the default filled in.
func (s TenantSettings) CalendarMode() string {
	if s.CalendarNumber == CalendarTrip {
		return CalendarTrip
	}
	return CalendarRemaining
}

func (t *Tenant) ParsedSettings() (TenantSettings, error) {
	var s TenantSettings
	if t.Settings == "" {
		return s, nil
	}
	if err := json.Unmarshal([]byte(t.Settings), &s); err != nil {
		return TenantSettings{}, fmt.Errorf("parse tenant settings: %w", err)
	}
	return s, nil
}

func (s *Store) UpdateTenantSettings(ctx context.Context, tenantID int64, settings TenantSettings) error {
	b, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal tenant settings: %w", err)
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE tenants SET settings = ? WHERE id = ?`, string(b), tenantID)
	if err != nil {
		return fmt.Errorf("update tenant settings: %w", err)
	}
	return nil
}
