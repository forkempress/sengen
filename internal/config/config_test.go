package config

import (
	"strings"
	"testing"
)

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{"PUBLIC_BASE_URL", "TELEGRAM_BOT_TOKEN", "TELEGRAM_WEBHOOK_SECRET", "TELEGRAM_BOT_USERNAME", "TRUSTED_PROXY", "BIND_ADDR", "PORT", "ADMIN_TELEGRAM_IDS"} {
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

// SEC-14: by default the app listens only on loopback, where the tunnel or
// Caddy reach it, not on every interface of the machine.
func TestLoad_ListensOnLoopbackByDefault(t *testing.T) {
	setEnv(t, map[string]string{})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.ListenAddr(); got != "127.0.0.1:8080" {
		t.Errorf("ListenAddr() = %q, want 127.0.0.1:8080", got)
	}

	setEnv(t, map[string]string{"BIND_ADDR": "0.0.0.0", "PORT": "9000"})
	if cfg, _ := Load(); cfg.ListenAddr() != "0.0.0.0:9000" {
		t.Errorf("ListenAddr() = %q, want 0.0.0.0:9000 when set", cfg.ListenAddr())
	}
}

// SEC-06: the proxy mode is explicit and an unknown value stops startup.
func TestLoad_TrustedProxy(t *testing.T) {
	setEnv(t, map[string]string{"TRUSTED_PROXY": "caddy"})
	if cfg, err := Load(); err != nil || cfg.TrustedProxy != "caddy" {
		t.Errorf("TRUSTED_PROXY=caddy: %q, %v", cfg.TrustedProxy, err)
	}
	setEnv(t, map[string]string{"TRUSTED_PROXY": "nginx"})
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXY") {
		t.Errorf("unknown TRUSTED_PROXY: err = %v, want a TRUSTED_PROXY error", err)
	}
}

func TestLoad_AdminTelegramIDs(t *testing.T) {
	setEnv(t, map[string]string{"ADMIN_TELEGRAM_IDS": " 111, 222 ,,"})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AdminTelegramIDs) != 2 || cfg.AdminTelegramIDs[0] != 111 || cfg.AdminTelegramIDs[1] != 222 {
		t.Errorf("AdminTelegramIDs = %v, want [111 222]", cfg.AdminTelegramIDs)
	}

	for _, bad := range []string{"111,abc", "-5", "0"} {
		setEnv(t, map[string]string{"ADMIN_TELEGRAM_IDS": bad})
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ADMIN_TELEGRAM_IDS") {
			t.Errorf("ADMIN_TELEGRAM_IDS=%q: err = %v, want an ADMIN_TELEGRAM_IDS error", bad, err)
		}
	}
}
