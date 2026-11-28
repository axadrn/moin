package alert

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"mime"
	"net"
	"net/http/httptest"
	"net/textproto"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
)

// mailbox is a tiny SMTP server for one test. It keeps every line a
// client said: its commands, then the lines of the mail.
type mailbox struct {
	address string
	// startTLS is whether it offers STARTTLS.
	startTLS    bool
	certificate tls.Certificate

	mu   sync.Mutex
	said []string
}

// newMailbox starts a mailbox on this machine, with the certificate of a
// test web server, which the sender trusts until the test ends.
func newMailbox(t *testing.T, startTLS bool) *mailbox {
	t.Helper()
	web := httptest.NewTLSServer(nil)
	t.Cleanup(web.Close)
	emailRoots = x509.NewCertPool()
	emailRoots.AddCert(web.Certificate())
	t.Cleanup(func() { emailRoots = nil })

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	box := &mailbox{address: listener.Addr().String(), startTLS: startTLS, certificate: web.TLS.Certificates[0]}
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			box.talk(connection)
		}
	}()
	return box
}

// talk answers one client until it says QUIT.
func (b *mailbox) talk(connection net.Conn) {
	defer connection.Close()
	text := textproto.NewConn(connection)
	_ = text.PrintfLine("220 mailbox")
	encrypted := false
	for {
		line, err := text.ReadLine()
		if err != nil {
			return
		}
		b.keep(line)
		verb, _, _ := strings.Cut(strings.ToUpper(line), " ")
		switch {
		case verb == "EHLO" && b.startTLS && !encrypted:
			_ = text.PrintfLine("250-mailbox\r\n250 STARTTLS")
		case verb == "EHLO" && encrypted:
			_ = text.PrintfLine("250-mailbox\r\n250 AUTH PLAIN")
		case verb == "STARTTLS":
			_ = text.PrintfLine("220 go ahead")
			text = textproto.NewConn(tls.Server(connection, &tls.Config{Certificates: []tls.Certificate{b.certificate}}))
			encrypted = true
		case verb == "AUTH":
			_ = text.PrintfLine("235 welcome")
		case verb == "DATA":
			_ = text.PrintfLine("354 send it")
			lines, _ := text.ReadDotLines()
			b.keep(lines...)
			_ = text.PrintfLine("250 taken")
		case verb == "QUIT":
			_ = text.PrintfLine("221 bye")
			return
		default:
			_ = text.PrintfLine("250 ok")
		}
	}
}

func (b *mailbox) keep(lines ...string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.said = append(b.said, lines...)
}

// line is the first line the client said that starts with prefix, without it.
func (b *mailbox) line(prefix string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, line := range b.said {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			return rest
		}
	}
	return ""
}

// An email goes through the SMTP server of the destination: signed in
// with the user and password after STARTTLS, from the user to the address.
func TestSendEmail(t *testing.T) {
	box := newMailbox(t, true)
	d := Destination{Kind: "email", To: "ada@example.com", SMTP: box.address, User: "alerts@example.com", Password: "secret-password"}
	if err := Send(context.Background(), d, down); err != nil {
		t.Fatal(err)
	}
	login, _ := base64.StdEncoding.DecodeString(box.line("AUTH PLAIN "))
	if string(login) != "\x00alerts@example.com\x00secret-password" {
		t.Errorf("signed in with %q", login)
	}
	if !strings.HasPrefix(box.line("MAIL FROM:"), "<alerts@example.com>") {
		t.Errorf("mail from %q", box.line("MAIL FROM:"))
	}
	if box.line("RCPT TO:") != "<ada@example.com>" {
		t.Errorf("mail to %q", box.line("RCPT TO:"))
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(box.line("Subject: "))
	if err != nil || subject != "🔴 shop.example.com is down" {
		t.Errorf("subject = %q, %v", subject, err)
	}
	if box.line("To: ") != "ada@example.com" || !strings.Contains(box.line("From: "), "<alerts@example.com>") {
		t.Errorf("headers: to %q, from %q", box.line("To: "), box.line("From: "))
	}
	if !slices.Contains(box.said, "No answer within 10s.") || !slices.Contains(box.said, "prod · shop") {
		t.Errorf("the mail = %q", box.said)
	}

	// From says who the mail is from, when the user is not an address.
	other := newMailbox(t, true)
	d = Destination{Kind: "email", To: "ada@example.com", SMTP: other.address, User: "apikey", Password: "secret-password", From: "alerts@example.com"}
	if err := Send(context.Background(), d, down); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(other.line("MAIL FROM:"), "<alerts@example.com>") {
		t.Errorf("mail from %q", other.line("MAIL FROM:"))
	}
}

// A server without STARTTLS gets no password and no mail: nothing may
// travel in the clear.
func TestSendEmailNeedsStartTLS(t *testing.T) {
	box := newMailbox(t, false)
	d := Destination{Kind: "email", To: "ada@example.com", SMTP: box.address, User: "alerts@example.com", Password: "secret-password"}
	err := Send(context.Background(), d, down)
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("err = %v, want one about STARTTLS", err)
	}
	box.mu.Lock()
	defer box.mu.Unlock()
	for _, line := range box.said {
		if !strings.HasPrefix(line, "EHLO") {
			t.Errorf("the client went on without TLS: %q", line)
		}
	}
}

// A sender with a control connects to an SMTP server only when the
// control lets it: a watcher refuses private addresses this way.
func TestSenderControlStopsEmail(t *testing.T) {
	box := newMailbox(t, true)
	refuse := NewSender(func(string, string, syscall.RawConn) error { return errors.New("refused") })
	d := Destination{Kind: "email", To: "ada@example.com", SMTP: box.address, User: "alerts@example.com", Password: "secret-password"}
	if err := refuse.Send(context.Background(), d, down); err == nil {
		t.Error("the mail went out past the control")
	}
	if box.line("EHLO") != "" {
		t.Error("the sender connected past the control")
	}
}

func TestCheckEmail(t *testing.T) {
	good := Destination{Kind: "email", To: "ada@example.com", SMTP: "smtp.example.com:587", User: "alerts@example.com", Password: "x"}
	if err := Check(good); err != nil {
		t.Errorf("Check(%+v) = %v", good, err)
	}
	bad := map[string]func(*Destination){
		"no password":             func(d *Destination) { d.Password = "" },
		"no port":                 func(d *Destination) { d.SMTP = "smtp.example.com" },
		"to with a name":          func(d *Destination) { d.To = "Ada <ada@example.com>" },
		"to with a second header": func(d *Destination) { d.To = "ada@example.com\r\nBcc: eve@example.com" },
		"user is no address":      func(d *Destination) { d.User = "apikey" },
		"from is no address":      func(d *Destination) { d.From = "alerts" },
	}
	for name, change := range bad {
		d := good
		change(&d)
		if err := Check(d); err == nil {
			t.Errorf("%s: Check = nil, want an error", name)
		}
	}
}
