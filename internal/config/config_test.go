package config

import (
	"strings"
	"testing"
)

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{"PUBLIC_BASE_URL", "TELEGRAM_BOT_TOKEN", "TELEGRAM_WEBHOOK_SECRET", "TELEGRAM_BOT_USERNAME"} {
		t.Setenv(k, kv[k])
	}
}

// A half-configured bot must stop the app at startup with a clear message:
// otherwise it runs fine and then silently rejects every Telegram message.
func TestLoad_BotConfigurationIsValidated(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"no bot at all is fine", map[string]string{}, ""},
		{"token with a valid secret", map[string]string{
			"TELEGRAM_BOT_TOKEN": "123:abc", "TELEGRAM_WEBHOOK_SECRET": "a1b2_c3-d4",
		}, ""},
		{"token without secret", map[string]string{
			"TELEGRAM_BOT_TOKEN": "123:abc",
		}, "TELEGRAM_WEBHOOK_SECRET"},
		{"secret with characters Telegram refuses", map[string]string{
			"TELEGRAM_BOT_TOKEN": "123:abc", "TELEGRAM_WEBHOOK_SECRET": "ima razmak!",
		}, "samo slova"},
		{"secret too long", map[string]string{
			"TELEGRAM_BOT_TOKEN": "123:abc", "TELEGRAM_WEBHOOK_SECRET": strings.Repeat("a", 257),
		}, "samo slova"},
		{"plain http public address", map[string]string{
			"PUBLIC_BASE_URL": "http://app.dani90.rs",
		}, "https://"},
		{"https public address", map[string]string{
			"PUBLIC_BASE_URL": "https://app.dani90.rs",
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, tc.env)
			_, err := Load()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case tc.wantErr != "" && err == nil:
				t.Errorf("expected an error mentioning %q, got none", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Errorf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoad_NormalisesValues(t *testing.T) {
	setEnv(t, map[string]string{
		"PUBLIC_BASE_URL":       " https://app.dani90.rs/ ",
		"TELEGRAM_BOT_USERNAME": "@Dani90Bot",
	})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicBaseURL != "https://app.dani90.rs" {
		t.Errorf("PublicBaseURL = %q, want no trailing slash or spaces", cfg.PublicBaseURL)
	}
	if cfg.TelegramBotUsername != "Dani90Bot" {
		t.Errorf("TelegramBotUsername = %q, want it without @", cfg.TelegramBotUsername)
	}
}
