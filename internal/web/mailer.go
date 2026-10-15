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

// defaultMailRetryDelays are the waits before each new attempt after a
// failed send: a provider hiccup or a blocked port often clears within
// minutes. Only the last failure goes to email_log as failed, and the
// superadmin bot reports it.
var defaultMailRetryDelays = []time.Duration{30 * time.Second, 2 * time.Minute}

// sendMail sends one message, within the daily cap, and logs the outcome.
// A failure is logged only when final is set; an earlier attempt is retried.
func (s *Server) sendMail(ctx context.Context, tenantID *int64, msg mail.Message, final bool) error {
	sent, err := s.Store.CountEmailsSentSince(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		return err
	}
	if sent >= s.MailDailyLimit {
		s.recordMail(ctx, tenantID, msg, store.EmailOverLimit, "")
		return errMailLimit
	}
	if err := s.Mailer.Send(ctx, msg); err != nil {
		if final {
			s.recordMail(ctx, tenantID, msg, store.EmailFailed, err.Error())
		}
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
// whether or not a message is sent. A failed send is tried again after each
// of MailRetryDelays.
func (s *Server) sendMailAsync(tenantID *int64, msg mail.Message) {
	s.mailWG.Add(1)
	go func() {
		defer s.mailWG.Done()
		for attempt := 0; ; attempt++ {
			final := attempt == len(s.MailRetryDelays)
			err := s.sendAttempt(tenantID, msg, final)
			if err == nil {
				return
			}
			if final || errors.Is(err, errMailLimit) {
				log.Printf("mail [%s]: %v (pokušaja: %d)", msg.Tag, err, attempt+1)
				return
			}
			delay := s.MailRetryDelays[attempt]
			log.Printf("mail [%s]: %v; ponovo za %s", msg.Tag, err, delay)
			select {
			case <-time.After(delay):
			case <-s.mailStop:
				// Shutting down: the message is lost, so log it as failed.
				s.recordMail(context.Background(), tenantID, msg, store.EmailFailed,
					err.Error()+" (ponovni pokušaj prekinut gašenjem servera)")
				return
			}
		}
	}()
}

func (s *Server) sendAttempt(tenantID *int64, msg mail.Message, final bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.sendMail(ctx, tenantID, msg, final)
}

// StopMailRetries ends the waits between attempts; the server calls it on
// shutdown, before WaitMail, so a pending retry does not hold it up.
func (s *Server) StopMailRetries() {
	s.mailStopOnce.Do(func() { close(s.mailStop) })
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
