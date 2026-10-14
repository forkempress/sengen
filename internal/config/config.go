// Package config loads runtime configuration from environment variables.
// Secrets (Telegram token, webhook secret) must never be committed to the
// repo — set them via the environment or a local .env loaded by the process
// manager (systemd EnvironmentFile=, or the Makefile for local dev).
package config

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"

	"shengen/internal/auth"
)

type Config struct {
	Port string // HTTP listen port, e.g. "8080"
	// BindAddr is the interface to listen on. The default 127.0.0.1 keeps
	// the app off the LAN: only the tunnel or Caddy on the same host reach
	// it. Set BIND_ADDR=0.0.0.0 only if something else must connect.
	BindAddr        string
	DBPath          string // path to the SQLite file
	InsecureCookies bool   // set true only for local http:// dev; omits the Secure cookie flag

	// TrustedProxy names the reverse proxy in front of the app
	// (TRUSTED_PROXY=cloudflare|caddy|none) and so which header holds the
	// client address. It must match the deploy: behind Caddy, trusting
	// CF-Connecting-IP lets anyone bypass the login rate limit.
	TrustedProxy auth.TrustedProxy

	// PublicBaseURL is where users reach the app, e.g. https://app.dani90.rs.
	// When set together with the bot token, the Telegram webhook is
	// registered automatically at startup.
	PublicBaseURL string

	// One Telegram bot serves every firm.
	TelegramBotToken      string
	TelegramWebhookSecret string // compared against X-Telegram-Bot-Api-Secret-Token
	TelegramBotUsername   string // optional; enables one-tap t.me links
}

// Telegram only accepts A-Z, a-z, 0-9, "_" and "-" in a webhook secret, 1–256
// characters; anything else makes setWebhook fail.
var webhookSecretRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)

// Load reads configuration from the environment, applying defaults for
// local development. A half-configured bot is an error: a token without a
// valid secret would start up fine and then reject every message.
func Load() (Config, error) {
	cfg := Config{
		Port:                  getenv("PORT", "8080"),
		BindAddr:              getenv("BIND_ADDR", "127.0.0.1"),
		DBPath:                getenv("DB_PATH", "shengen.db"),
		InsecureCookies:       getenv("DEV_INSECURE_COOKIES", "") == "true",
		PublicBaseURL:         strings.TrimRight(strings.TrimSpace(os.Getenv("PUBLIC_BASE_URL")), "/"),
		TelegramBotToken:      strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		TelegramWebhookSecret: strings.TrimSpace(os.Getenv("TELEGRAM_WEBHOOK_SECRET")),
		TelegramBotUsername:   strings.TrimPrefix(strings.TrimSpace(os.Getenv("TELEGRAM_BOT_USERNAME")), "@"),
	}

	proxy, err := auth.ParseTrustedProxy(os.Getenv("TRUSTED_PROXY"))
	if err != nil {
		return Config{}, err
	}
	cfg.TrustedProxy = proxy

	if cfg.TelegramBotToken != "" {
		if cfg.TelegramWebhookSecret == "" {
			return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN je postavljen, ali TELEGRAM_WEBHOOK_SECRET nije (napravite ga: openssl rand -hex 32)")
		}
		if !webhookSecretRe.MatchString(cfg.TelegramWebhookSecret) {
			return Config{}, fmt.Errorf("TELEGRAM_WEBHOOK_SECRET sme da sadrži samo slova, cifre, _ i - (najviše 256 znakova)")
		}
	}
	if cfg.PublicBaseURL != "" && !strings.HasPrefix(cfg.PublicBaseURL, "https://") {
		return Config{}, fmt.Errorf("PUBLIC_BASE_URL mora počinjati sa https:// (Telegram ne prihvata http webhook)")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ListenAddr is the host:port the HTTP server listens on.
func (c Config) ListenAddr() string { return net.JoinHostPort(c.BindAddr, c.Port) }
