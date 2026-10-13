// Command server is the single binary that serves the web UI, htmx
// endpoints, the Telegram webhook, and runs background jobs (reminders,
// alerts). /healthz is reachable regardless of Host header; every other
// route requires a known tenant host.
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
	"shengen/internal/tenant"
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
	bot := telegram.New(st)
	webServer := web.New(st, authSvc, bot)

	jobRunner := jobs.New(st)
	go jobRunner.Start(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := conn.Ping(); err != nil {
			http.Error(w, "db not reachable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.Handle("/", tenant.Middleware(st)(webServer.Routes()))

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
