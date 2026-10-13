package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// TenantSettings holds the per-tenant configuration that used to be a
// single global config in a single-customer deploy, but must live per-tenant
// now that one binary serves many companies: each customer runs their own
// Telegram bot (their own @BotFather token) pointed at their own subdomain.
type TenantSettings struct {
	TelegramBotToken      string `json:"telegram_bot_token,omitempty"`
	TelegramWebhookSecret string `json:"telegram_webhook_secret,omitempty"`
	TelegramAlertChatID   int64  `json:"telegram_alert_chat_id,omitempty"`
	ThresholdRedDays      int    `json:"threshold_red_days,omitempty"`    // default 15
	ThresholdOrangeDays   int    `json:"threshold_orange_days,omitempty"` // default 30
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
