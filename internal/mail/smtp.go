package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

// SMTPMailer sends through any provider's SMTP relay (Brevo, Postmark, SES
// and Resend all offer one), so changing provider is a change of settings,
// not of code. Port 465 uses TLS from the first byte; any other port must
// offer STARTTLS — mail is never sent, nor the password given, in the clear.
type SMTPMailer struct {
	Host     string
	Port     string
	Username string
	Password string
	From     *netmail.Address
	ReplyTo  *netmail.Address // optional
	// Timeout bounds one whole send, connection to QUIT.
	Timeout time.Duration
}

const defaultSMTPTimeout = 15 * time.Second

func (m *SMTPMailer) Send(ctx context.Context, msg Message) error {
	if err := checkHeader(msg.To); err != nil {
		return err
	}
	if err := checkHeader(msg.Subject); err != nil {
		return err
	}
	to, err := netmail.ParseAddress(msg.To)
	if err != nil {
		return fmt.Errorf("mail: recipient %q: %w", msg.To, err)
	}
	body, err := m.build(to, msg)
	if err != nil {
		return err
	}

	c, cancel, err := m.connect(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	defer c.Close()

	if err := c.Mail(m.From.Address); err != nil {
		return fmt.Errorf("mail: MAIL FROM: %w", err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("mail: RCPT TO: %w", err)
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("mail: DATA: %w", err)
	}
	if _, err := wc.Write(body); err != nil {
		wc.Close()
		return fmt.Errorf("mail: write: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("mail: provider refused the message: %w", err)
	}
	return c.Quit()
}

// Check signs in to the relay and leaves without sending anything: whether
// the host, port, TLS and credentials are right.
func (m *SMTPMailer) Check(ctx context.Context) error {
	c, cancel, err := m.connect(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	defer c.Close()
	return c.Quit()
}

// connect opens a TLS-protected, authenticated session with the relay.
func (m *SMTPMailer) connect(ctx context.Context) (*smtp.Client, context.CancelFunc, error) {
	timeout := m.Timeout
	if timeout == 0 {
		timeout = defaultSMTPTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	fail := func(err error) (*smtp.Client, context.CancelFunc, error) {
		cancel()
		return nil, nil, err
	}

	addr := net.JoinHostPort(m.Host, m.Port)
	tlsConfig := &tls.Config{ServerName: m.Host, MinVersion: tls.VersionTLS12}
	dialer := &net.Dialer{}
	var conn net.Conn
	var err error
	if m.Port == "465" {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConfig}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fail(fmt.Errorf("mail: connect %s: %w", addr, err))
	}
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	c, err := smtp.NewClient(conn, m.Host)
	if err != nil {
		conn.Close()
		return fail(fmt.Errorf("mail: smtp hello: %w", err))
	}
	if m.Port != "465" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			c.Close()
			return fail(fmt.Errorf("mail: %s does not offer STARTTLS", addr))
		}
		if err := c.StartTLS(tlsConfig); err != nil {
			c.Close()
			return fail(fmt.Errorf("mail: starttls: %w", err))
		}
	}
	if m.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", m.Username, m.Password, m.Host)); err != nil {
			c.Close()
			return fail(fmt.Errorf("mail: auth: %w", err))
		}
	}
	return c, cancel, nil
}

// build renders the message as multipart/alternative: plain text first,
// HTML second, both UTF-8 quoted-printable.
func (m *SMTPMailer) build(to *netmail.Address, msg Message) ([]byte, error) {
	var b bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("From", m.From.String())
	header("To", to.String())
	if m.ReplyTo != nil {
		header("Reply-To", m.ReplyTo.String())
	}
	header("Subject", mime.QEncoding.Encode("UTF-8", msg.Subject))
	header("Date", time.Now().Format(time.RFC1123Z))
	header("Message-ID", messageID(m.From.Address))
	header("MIME-Version", "1.0")
	// Transactional mail: ask auto-responders not to answer it.
	header("Auto-Submitted", "auto-generated")

	mw := multipart.NewWriter(&b)
	header("Content-Type", `multipart/alternative; boundary="`+mw.Boundary()+`"`)
	b.WriteString("\r\n")
	for _, part := range []struct{ ctype, body string }{
		{"text/plain; charset=UTF-8", msg.Text},
		{"text/html; charset=UTF-8", msg.HTML},
	} {
		if part.body == "" {
			continue
		}
		w, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.ctype},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, err
		}
		qp := quotedprintable.NewWriter(w)
		if _, err := qp.Write([]byte(strings.ReplaceAll(part.body, "\n", "\r\n"))); err != nil {
			return nil, err
		}
		if err := qp.Close(); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func messageID(from string) string {
	domain := "localhost"
	if i := strings.LastIndexByte(from, '@'); i >= 0 {
		domain = from[i+1:]
	}
	var r [12]byte
	rand.Read(r[:])
	return "<" + hex.EncodeToString(r[:]) + "@" + domain + ">"
}
