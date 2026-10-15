// Package mail sends the app's transactional email (password reset, and
// later sign-up confirmation, invitations and trial reminders) and renders
// the messages from embedded templates, each as plain text plus simple HTML.
package mail

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"log"
	"strings"
	"sync"
	texttemplate "text/template"
)

// Message is one email to one recipient.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
	// Tag names the kind of message ("reset", "signup", …) for the log.
	Tag string
}

// Mailer delivers a message. Send returns once the provider has accepted it
// (or refused it); it does not wait for delivery.
type Mailer interface {
	Send(ctx context.Context, m Message) error
}

// LogMailer writes each message to the server log instead of sending it,
// links and codes included, so local development needs no provider account.
type LogMailer struct{}

func (LogMailer) Send(_ context.Context, m Message) error {
	log.Printf("mail (nije poslat, SMTP nije podešen) [%s] za %s: %s\n%s", m.Tag, m.To, m.Subject, m.Text)
	return nil
}

// ErrNotConfigured: a server that is not in development mode has no SMTP.
var ErrNotConfigured = errors.New("mail: SMTP is not configured")

// Unconfigured is the mailer of a production server without SMTP: every
// send fails and is logged as such. Unlike LogMailer it never writes the
// message itself, which would put working reset links in the server log.
type Unconfigured struct{}

func (Unconfigured) Send(context.Context, Message) error { return ErrNotConfigured }

// Recorder keeps every message instead of sending it, for tests.
type Recorder struct {
	mu   sync.Mutex
	sent []Message
	Err  error // returned by Send when set
}

func (r *Recorder) Send(_ context.Context, m Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return r.Err
	}
	r.sent = append(r.sent, m)
	return nil
}

// Sent returns a copy of the messages sent so far.
func (r *Recorder) Sent() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Message(nil), r.sent...)
}

//go:embed templates/*.txt templates/*.html
var templateFS embed.FS

var (
	textTemplates = texttemplate.Must(texttemplate.ParseFS(templateFS, "templates/*.txt"))
	htmlTemplates = htmltemplate.Must(htmltemplate.ParseFS(templateFS, "templates/*.html"))
)

// Render builds a message from the templates named name: name.txt defines
// "<name>.subject" and "<name>.text", name.html defines "<name>.html", which
// is wrapped in the shared layout.
func Render(name, to string, data any) (Message, error) {
	subject, err := execText(name+".subject", data)
	if err != nil {
		return Message{}, err
	}
	text, err := execText(name+".text", data)
	if err != nil {
		return Message{}, err
	}
	var body bytes.Buffer
	if err := htmlTemplates.ExecuteTemplate(&body, name+".html", data); err != nil {
		return Message{}, fmt.Errorf("mail %s html: %w", name, err)
	}
	var page bytes.Buffer
	if err := htmlTemplates.ExecuteTemplate(&page, "layout", layoutData{Subject: subject, Body: htmltemplate.HTML(body.String())}); err != nil {
		return Message{}, fmt.Errorf("mail layout: %w", err)
	}
	return Message{To: to, Subject: subject, Text: text, HTML: page.String(), Tag: name}, nil
}

type layoutData struct {
	Subject string
	Body    htmltemplate.HTML
}

func execText(name string, data any) (string, error) {
	var b bytes.Buffer
	if err := textTemplates.ExecuteTemplate(&b, name, data); err != nil {
		return "", fmt.Errorf("mail %s: %w", name, err)
	}
	return strings.TrimSpace(b.String()), nil
}

// ErrHeaderInjection refuses a recipient or subject that would add headers.
var ErrHeaderInjection = errors.New("mail: header contains a line break")

func checkHeader(v string) error {
	if strings.ContainsAny(v, "\r\n") {
		return ErrHeaderInjection
	}
	return nil
}
