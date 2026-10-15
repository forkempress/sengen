package web

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"shengen/internal/auth"
	"shengen/internal/mail"
	"shengen/internal/store"
)

// errMailLimit: the daily sending cap is reached; the message is not sent.
var errMailLimit = errors.New("daily email limit reached")

// sendMail sends one message, within the daily cap, and logs the outcome.
func (s *Server) sendMail(ctx context.Context, tenantID *int64, msg mail.Message) error {
	sent, err := s.Store.CountEmailsSentSince(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		return err
	}
	if sent >= s.MailDailyLimit {
		s.recordMail(ctx, tenantID, msg, store.EmailOverLimit, "")
		return errMailLimit
	}
	if err := s.Mailer.Send(ctx, msg); err != nil {
		s.recordMail(ctx, tenantID, msg, store.EmailFailed, err.Error())
		return err
	}
	s.recordMail(ctx, tenantID, msg, store.EmailSent, "")
	return nil
}

func (s *Server) recordMail(ctx context.Context, tenantID *int64, msg mail.Message, status, errText string) {
	if err := s.Store.RecordEmail(ctx, msg.Tag, tenantID, msg.To, status, errText); err != nil {
		log.Printf("mail log: %v", err)
	}
}

// sendMailAsync sends after the response has gone out. Forms that must not
// reveal whether an account exists use it: the reply comes just as fast
// whether or not a message is sent.
func (s *Server) sendMailAsync(tenantID *int64, msg mail.Message) {
	s.mailWG.Add(1)
	go func() {
		defer s.mailWG.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.sendMail(ctx, tenantID, msg); err != nil {
			log.Printf("mail [%s]: %v", msg.Tag, err)
		}
	}()
}

// WaitMail waits for background sends to finish, or for ctx to end. The
// server calls it on shutdown; tests call it before looking at the outbox.
func (s *Server) WaitMail(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		s.mailWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// link builds an absolute URL to the app for an email.
func (s *Server) link(path string) string { return s.PublicURL + path }

func (s *Server) clientIP(r *http.Request) string { return auth.ClientIP(r, s.TrustedProxy) }
