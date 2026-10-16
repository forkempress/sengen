// Package config loads runtime configuration from environment variables.
// Secrets (Telegram token, webhook secret) must never be committed to the
// repo — set them via the environment or a local .env loaded by the process
// manager (systemd EnvironmentFile=, or the Makefile for local dev).
package config

import (
	"fmt"
	"net"
	"net/mail"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"shengen/internal/auth"
	"shengen/internal/docread"
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

	// Outgoing email through the provider's SMTP relay. Without SMTP_HOST
	// the app only logs each message (local development).
	SMTPHost     string
	SMTPPort     string // 587 (STARTTLS) by default; 465 for TLS from the start
	SMTPUsername string
	SMTPPassword string
	MailFrom     *mail.Address // MAIL_FROM, e.g. "Dani90 <nalog@dani90.rs>"
	MailReplyTo  *mail.Address // MAIL_REPLY_TO, optional
	// MailDailyLimit caps messages per 24 hours (MAIL_DAILY_LIMIT), below
	// the provider's own limit, so a flood of requests cannot use up the
	// quota that password resets depend on.
	MailDailyLimit int

	// AnthropicAPIKey turns on reading scanned histories (PDF, photo) on
	// the import page (ANTHROPIC_API_KEY); empty hides that option.
	AnthropicAPIKey string
	// AnthropicKeyExpires is the key's last valid day
	// (ANTHROPIC_API_KEY_EXPIRES, YYYY-MM-DD); the superadmins are reminded
	// before it. nil when not set.
	AnthropicKeyExpires *time.Time
	// DocReadModel reads documents for firms without their own model
	// (DOC_READ_MODEL: opus, sonnet, haiku or a model id); empty is
	// docread.DefaultModel.
	DocReadModel string

	// Cloudflare Turnstile on the public forms; both keys or neither.
	TurnstileSiteKey   string
	TurnstileSecretKey string
}

// defaultMailDailyLimit stays under Brevo's free 300 a day.
const defaultMailDailyLimit = 250

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
		SMTPHost:              strings.TrimSpace(os.Getenv("SMTP_HOST")),
		SMTPPort:              getenv("SMTP_PORT", "587"),
		SMTPUsername:          strings.TrimSpace(os.Getenv("SMTP_USERNAME")),
		SMTPPassword:          os.Getenv("SMTP_PASSWORD"),
		TurnstileSiteKey:      strings.TrimSpace(os.Getenv("TURNSTILE_SITE_KEY")),
		TurnstileSecretKey:    strings.TrimSpace(os.Getenv("TURNSTILE_SECRET_KEY")),
		AnthropicAPIKey:       strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")),
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
	if err := loadMail(&cfg); err != nil {
		return Config{}, err
	}
	if raw := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY_EXPIRES")); raw != "" {
		d, err := time.Parse("2006-01-02", raw) // a date, kept as UTC midnight like every date in the app
		if err != nil {
			return Config{}, fmt.Errorf("ANTHROPIC_API_KEY_EXPIRES: %q nije datum (primer: 2026-11-14)", raw)
		}
		cfg.AnthropicKeyExpires = &d
	}
	if raw := os.Getenv("DOC_READ_MODEL"); strings.TrimSpace(raw) != "" {
		model, ok := docread.ResolveModel(raw)
		if !ok {
			return Config{}, fmt.Errorf("DOC_READ_MODEL: nepoznat model %q (opus, sonnet ili haiku)", raw)
		}
		cfg.DocReadModel = model
	}
	if (cfg.TurnstileSiteKey == "") != (cfg.TurnstileSecretKey == "") {
		return Config{}, fmt.Errorf("TURNSTILE_SITE_KEY i TURNSTILE_SECRET_KEY se postavljaju zajedno")
	}
	if cfg.PublicBaseURL != "" && !strings.HasPrefix(cfg.PublicBaseURL, "https://") {
		return Config{}, fmt.Errorf("PUBLIC_BASE_URL mora počinjati sa https:// (Telegram ne prihvata http webhook)")
	}
	return cfg, nil
}

// loadMail reads the email settings. Half a configuration is an error, like
// a bot token without its secret: the app would start and then fail every
// password reset.
func loadMail(cfg *Config) error {
	limit := getenv("MAIL_DAILY_LIMIT", strconv.Itoa(defaultMailDailyLimit))
	n, err := strconv.Atoi(limit)
	if err != nil || n < 1 {
		return fmt.Errorf("MAIL_DAILY_LIMIT: %q nije pozitivan broj", limit)
	}
	cfg.MailDailyLimit = n

	if from := strings.TrimSpace(os.Getenv("MAIL_FROM")); from != "" {
		addr, err := mail.ParseAddress(from)
		if err != nil {
			return fmt.Errorf("MAIL_FROM: %q nije email adresa (primer: Dani90 <nalog@dani90.rs>)", from)
		}
		cfg.MailFrom = addr
	}
	if replyTo := strings.TrimSpace(os.Getenv("MAIL_REPLY_TO")); replyTo != "" {
		addr, err := mail.ParseAddress(replyTo)
		if err != nil {
			return fmt.Errorf("MAIL_REPLY_TO: %q nije email adresa", replyTo)
		}
		cfg.MailReplyTo = addr
	}
	if cfg.SMTPHost == "" {
		return nil
	}
	if cfg.MailFrom == nil {
		return fmt.Errorf("SMTP_HOST je postavljen, ali MAIL_FROM nije")
	}
	if cfg.SMTPUsername == "" || cfg.SMTPPassword == "" {
		return fmt.Errorf("SMTP_HOST je postavljen, ali SMTP_USERNAME ili SMTP_PASSWORD nije")
	}
	if p, err := strconv.Atoi(cfg.SMTPPort); err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("SMTP_PORT: %q nije port", cfg.SMTPPort)
	}
	return nil
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
