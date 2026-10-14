// Command server is the single binary that serves the web UI, htmx
// endpoints, the Telegram webhook, and runs background jobs (reminders,
// alerts). Every firm uses the same address: the signed-in account decides
// which firm's data a request sees.
package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"shengen/internal/auth"
	"shengen/internal/config"
	"shengen/internal/db"
	"shengen/internal/jobs"
	"shengen/internal/store"
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
	webServer := web.New(st, authSvc, bot)

	go jobs.New(st, bot.Client()).Start(ctx)
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

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	log.Printf("listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
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
