package mail

import (
	"bufio"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	netmail "net/mail"
	"strings"
	"testing"
	"time"
)

func TestRender_Reset(t *testing.T) {
	m, err := Render("reset", "ana@firma.rs", ResetData{Name: "Ana <b>", URL: "https://app.dani90.rs/nova-lozinka?t=abc&x=1", ValidFor: "1 sat"})
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "Nova lozinka za Dani90" || m.Tag != "reset" || m.To != "ana@firma.rs" {
		t.Errorf("subject/tag/to = %q/%q/%q", m.Subject, m.Tag, m.To)
	}
	if !strings.Contains(m.Text, "https://app.dani90.rs/nova-lozinka?t=abc&x=1") || !strings.Contains(m.Text, "važi 1 sat") {
		t.Errorf("text lacks link or validity:\n%s", m.Text)
	}
	if strings.Contains(m.HTML, "Ana <b>") || !strings.Contains(m.HTML, "Ana &lt;b&gt;") {
		t.Error("name is not escaped in HTML")
	}
	if !strings.Contains(m.HTML, `href="https://app.dani90.rs/nova-lozinka?t=abc&amp;x=1"`) {
		t.Errorf("HTML lacks the link:\n%s", m.HTML)
	}
}

func TestRender_EveryTemplate(t *testing.T) {
	cases := map[string]any{
		"reset":      ResetData{Name: "A", URL: "https://x", ValidFor: "1 sat"},
		"reset_done": ResetDoneData{Name: "A", When: "15.10.2026. u 14:05", ResetURL: "https://x"},
	}
	for name, data := range cases {
		m, err := Render(name, "a@b.rs", data)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if m.Subject == "" || m.Text == "" || m.HTML == "" {
			t.Errorf("%s: empty part", name)
		}
	}
}

func TestSMTP_BuildIsValidMultipart(t *testing.T) {
	from, _ := netmail.ParseAddress("Dani90 <nalog@dani90.rs>")
	m := &SMTPMailer{From: from}
	to, _ := netmail.ParseAddress("ana@firma.rs")
	raw, err := m.build(to, Message{Subject: "Šifra č ž", Text: "Zdravo,\nlink: https://x", HTML: "<p>Zdravo</p>"})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := netmail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	subj, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || subj != "Šifra č ž" {
		t.Errorf("subject = %q, %v", subj, err)
	}
	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var types []string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(p)
		types = append(types, p.Header.Get("Content-Type"))
		if strings.HasPrefix(p.Header.Get("Content-Type"), "text/plain") && !strings.Contains(string(body), "link: https://x") {
			t.Errorf("plain part = %q", body)
		}
	}
	if len(types) != 2 {
		t.Errorf("parts = %v, want text and html", types)
	}
}

func TestSMTP_RefusesHeaderInjection(t *testing.T) {
	from, _ := netmail.ParseAddress("nalog@dani90.rs")
	m := &SMTPMailer{Host: "127.0.0.1", Port: "1", From: from}
	err := m.Send(context.Background(), Message{To: "a@b.rs\r\nBcc: x@y.rs", Subject: "x"})
	if !errors.Is(err, ErrHeaderInjection) {
		t.Errorf("err = %v, want ErrHeaderInjection", err)
	}
	err = m.Send(context.Background(), Message{To: "a@b.rs", Subject: "x\nBcc: x@y.rs"})
	if !errors.Is(err, ErrHeaderInjection) {
		t.Errorf("subject: err = %v, want ErrHeaderInjection", err)
	}
}

// A relay that does not offer STARTTLS gets nothing: no password, no mail.
func TestSMTP_RefusesServerWithoutSTARTTLS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(5 * time.Second))
		r := bufio.NewReader(c)
		c.Write([]byte("220 fake ESMTP\r\n"))
		var seen []string
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				break
			}
			seen = append(seen, strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(line, "EHLO"):
				c.Write([]byte("250-fake\r\n250 AUTH PLAIN\r\n"))
			case strings.HasPrefix(line, "QUIT"):
				c.Write([]byte("221 bye\r\n"))
			default:
				c.Write([]byte("250 ok\r\n"))
			}
		}
		got <- strings.Join(seen, "|")
	}()

	from, _ := netmail.ParseAddress("nalog@dani90.rs")
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	m := &SMTPMailer{Host: "127.0.0.1", Port: port, Username: "u", Password: "tajna", From: from, Timeout: 3 * time.Second}
	err = m.Send(context.Background(), Message{To: "a@b.rs", Subject: "x", Text: "y"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("err = %v, want refusal for missing STARTTLS", err)
	}
	ln.Close()
	select {
	case seen := <-got:
		if strings.Contains(seen, "AUTH") || strings.Contains(seen, "MAIL FROM") {
			t.Errorf("client went on without TLS: %s", seen)
		}
	case <-time.After(5 * time.Second):
	}
}
