// Command mailtest checks the SMTP settings from the environment (.env):
// without flags it only signs in to the relay and sends nothing; with -to it
// sends one test message to that address. Run it after changing provider
// or credentials, before restarting the server:
//
//	make mailtest                      # only sign in
//	make mailtest ARGS="-to ja@firma.rs"
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"shengen/internal/clock"
	"shengen/internal/config"
	"shengen/internal/mail"
)

func main() {
	to := flag.String("to", "", "pošalji probni mejl na ovu adresu (bez -to se samo proverava prijava na SMTP)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.SMTPHost == "" {
		log.Fatal("SMTP_HOST nije postavljen u okruženju (.env)")
	}
	m := &mail.SMTPMailer{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort,
		Username: cfg.SMTPUsername, Password: cfg.SMTPPassword,
		From: cfg.MailFrom, ReplyTo: cfg.MailReplyTo,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := m.Check(ctx); err != nil {
		log.Fatalf("prijava na %s:%s nije uspela: %v", cfg.SMTPHost, cfg.SMTPPort, err)
	}
	fmt.Printf("Prijava na %s:%s uspela (TLS, korisnik prihvaćen).\n", cfg.SMTPHost, cfg.SMTPPort)
	if *to == "" {
		return
	}

	when := clock.Now().Format("02.01.2006. u 15:04")
	msg := mail.Message{
		To:      *to,
		Subject: "Dani90 — probni mejl",
		Text:    "Ovo je probni mejl iz Dani90 aplikacije, poslat " + when + ".\nAko ste ga dobili, slanje mejlova radi.\n",
		HTML:    "<p>Ovo je probni mejl iz Dani90 aplikacije, poslat " + when + ".</p><p>Ako ste ga dobili, slanje mejlova radi.</p>",
		Tag:     "test",
	}
	if err := m.Send(ctx, msg); err != nil {
		log.Fatalf("slanje nije uspelo: %v", err)
	}
	fmt.Printf("Probni mejl predat %s za %s (pošiljalac %s).\n", cfg.SMTPHost, *to, cfg.MailFrom.Address)
	fmt.Println("U Gmail-u: ⋮ → Prikaži original — SPF, DKIM i DMARC treba da budu PASS.")
}
