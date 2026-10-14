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
	"strconv"
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

	// AdminTelegramIDs are the Telegram user ids of the service operators
	// (ADMIN_TELEGRAM_IDS, comma-separated). Only they get the superadmin
	// bot commands and push notifications; empty turns both off.
	AdminTelegramIDs []int64

	// BackupHeartbeatFile is touched by the backup job after every
	// successful backup (BACKUP_HEARTBEAT_FILE). When set, a file older than
	// a day and a bit — or missing — is reported to the superadmins.
	BackupHeartbeatFile string
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
		BackupHeartbeatFile:   strings.TrimSpace(os.Getenv("BACKUP_HEARTBEAT_FILE")),
	}

	ids, err := parseTelegramIDs(os.Getenv("ADMIN_TELEGRAM_IDS"))
	if err != nil {
		return Config{}, err
	}
	cfg.AdminTelegramIDs = ids

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

// parseTelegramIDs reads a comma-separated list of Telegram user ids. A typo
// is an error rather than a skipped entry: a silently dropped id would leave
// an operator wondering why the bot ignores them.
func parseTelegramIDs(raw string) ([]int64, error) {
	var ids []int64
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("ADMIN_TELEGRAM_IDS: %q nije Telegram ID korisnika (pozitivan broj)", part)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ListenAddr is the host:port the HTTP server listens on.
func (c Config) ListenAddr() string { return net.JoinHostPort(c.BindAddr, c.Port) }
