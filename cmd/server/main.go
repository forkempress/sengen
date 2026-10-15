// Command server is the single binary that serves the web UI, htmx
// endpoints, the Telegram webhook, and runs background jobs (reminders,
// alerts). Every firm uses the same address: the signed-in account decides
// which firm's data a request sees.
package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"shengen/internal/auth"
	"shengen/internal/config"
	"shengen/internal/db"
	"shengen/internal/docread"
	"shengen/internal/jobs"
	"shengen/internal/mail"
	"shengen/internal/store"
	"shengen/internal/superadmin"
	"shengen/internal/telegram"
	"shengen/internal/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	conn, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer conn.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st := store.New(conn)
	authSvc := auth.New(st, cfg.InsecureCookies)
	bot := telegram.New(st, telegram.Config{
		Token:         cfg.TelegramBotToken,
		WebhookSecret: cfg.TelegramWebhookSecret,
		Username:      cfg.TelegramBotUsername,
	})
	admins := superadmin.New(st, cfg.AdminTelegramIDs, cfg.BackupHeartbeatFile)
	if len(cfg.AdminTelegramIDs) > 0 {
		bot.SuperAdmin = admins
		log.Printf("superadmin: %d operatora", len(cfg.AdminTelegramIDs))
	}
	webServer := web.New(st, authSvc, bot)
	webServer.TrustedProxy = cfg.TrustedProxy
	log.Printf("trusted proxy: %s", cfg.TrustedProxy)
	configureMail(cfg, webServer)
	if cfg.AnthropicAPIKey != "" {
		webServer.DocReader = docread.NewClaude(cfg.AnthropicAPIKey)
		log.Printf("uvoz dokumenata: uključen")
	}

	runner := jobs.New(st, bot.Client())
	if len(cfg.AdminTelegramIDs) > 0 && bot.Client() != nil {
		runner.SuperAdmin = admins.Notifier(bot.Client())
	}
	go runner.Start(ctx)
	go registerWebhook(ctx, cfg, bot)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := conn.Ping(); err != nil {
			http.Error(w, "db not reachable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.Handle("/", webServer.Routes())

	srv := newHTTPServer(cfg.ListenAddr(), mux)
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
		// Reset links already promised on a page should still go out.
		webServer.WaitMail(shutdownCtx)
		// Updates already answered with 200 will not be redelivered, so
		// give the ones still being handled a moment to finish.
		bot.Wait(shutdownCtx)
	}()

	log.Printf("listening on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	<-shutdownDone
}

// newHTTPServer sets timeouts on every phase of a connection, so a client
// that sends its request (or reads the reply) a byte at a time cannot hold
// connections open indefinitely (slowloris).
func newHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// configureMail sets up email links, sending and the Turnstile check.
func configureMail(cfg config.Config, s *web.Server) {
	switch {
	case cfg.PublicBaseURL != "":
		s.PublicURL = cfg.PublicBaseURL
	case cfg.InsecureCookies:
		// Local development: links point at this machine.
		s.PublicURL = "http://localhost:" + cfg.Port
	}
	log.Printf("mail: linkovi vode na %s", s.PublicURL)

	s.MailDailyLimit = cfg.MailDailyLimit
	if cfg.MailReplyTo != nil {
		s.FeedbackEmail = cfg.MailReplyTo.Address
	}
	switch {
	case cfg.SMTPHost == "" && cfg.InsecureCookies:
		log.Printf("mail: SMTP_HOST nije postavljen — mejlovi se ispisuju u log (lokalni razvoj)")
	case cfg.SMTPHost == "":
		// Not local development: never put reset links in the log.
		s.Mailer = mail.Unconfigured{}
		s.MailReady = false
		log.Printf("mail: SMTP_HOST nije postavljen — mejlovi se NE šalju, reset lozinke je isključen")
	default:
		s.Mailer = &mail.SMTPMailer{
			Host: cfg.SMTPHost, Port: cfg.SMTPPort,
			Username: cfg.SMTPUsername, Password: cfg.SMTPPassword,
			From: cfg.MailFrom, ReplyTo: cfg.MailReplyTo,
		}
		log.Printf("mail: šalje se preko %s:%s kao %s, najviše %d dnevno", cfg.SMTPHost, cfg.SMTPPort, cfg.MailFrom.Address, cfg.MailDailyLimit)
	}

	if cfg.TurnstileSecretKey == "" {
		log.Printf("turnstile: ključevi nisu postavljeni — javne forme rade bez provere")
		return
	}
	t := &web.Turnstile{SiteKey: cfg.TurnstileSiteKey, Secret: cfg.TurnstileSecretKey}
	if u, err := url.Parse(s.PublicURL); err == nil && strings.HasPrefix(s.PublicURL, "https://") {
		t.Hostname = u.Hostname()
	}
	s.Turnstile = t
}

// registerWebhook points the shared bot at this server. setWebhook is
// idempotent, so doing it on every start keeps Telegram in sync after the
// address or the secret changes. A failure is logged, not fatal: the web app
// must keep working while Telegram is unreachable.
func registerWebhook(ctx context.Context, cfg config.Config, bot *telegram.Bot) {
	switch {
	case !bot.Configured():
		log.Printf("telegram: TELEGRAM_BOT_TOKEN nije postavljen — bot je isključen")
		return
	case cfg.PublicBaseURL == "":
		log.Printf("telegram: PUBLIC_BASE_URL nije postavljen — webhook se ne registruje automatski")
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	url := cfg.PublicBaseURL + "/telegram/webhook"
	if err := bot.Client().SetWebhook(ctx, url, cfg.TelegramWebhookSecret); err != nil {
		log.Printf("telegram: registracija webhook-a na %s nije uspela: %v", url, err)
		return
	}
	log.Printf("telegram: webhook registrovan na %s", url)
}
