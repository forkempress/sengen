// Package config loads runtime configuration from environment variables.
// Secrets (Telegram token, webhook secret, session secret) must never be
// committed to the repo — set them via the environment or a local .env
// loaded by the process manager (systemd EnvironmentFile=, or a Makefile
// target for local dev).
package config

import (
	"os"
)

type Config struct {
	Port                  string // HTTP listen port, e.g. "8080"
	DBPath                string // path to the SQLite file
	InsecureCookies       bool   // set true only for local http:// dev; omits the Secure cookie flag
	TelegramBotToken      string
	TelegramWebhookSecret string // compared against X-Telegram-Bot-Api-Secret-Token
}

// Load reads configuration from the environment, applying defaults for
// local development.
func Load() (Config, error) {
	cfg := Config{
		Port:                  getenv("PORT", "8080"),
		DBPath:                getenv("DB_PATH", "shengen.db"),
		InsecureCookies:       getenv("DEV_INSECURE_COOKIES", "") == "true",
		TelegramBotToken:      os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramWebhookSecret: os.Getenv("TELEGRAM_WEBHOOK_SECRET"),
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
