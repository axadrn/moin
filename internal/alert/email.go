package alert

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
)

// emailTimeout is how long one mail may take, from the connection to the
// server's goodbye.
const emailTimeout = 10 * time.Second

// emailRoots are the certificates an SMTP server is checked against: nil
// for the system's. Tests bring their own.
var emailRoots *x509.CertPool

// checkEmail says whether d can send a mail: an address to send to, an
// SMTP server as host:port, a user and a password, and an address the
// mail is from.
func checkEmail(d Destination) error {
	if d.To == "" || d.SMTP == "" || d.User == "" || d.Password == "" {
		return errors.New("an email alert needs to, smtp, user and a password")
	}
	if host, port, err := net.SplitHostPort(d.SMTP); err != nil || host == "" || port == "" {
		return errors.New("the smtp server of an email alert must be host:port, like smtp.example.com:587")
	}
	if !isAddress(d.To) {
		return fmt.Errorf("%q is not an email address", d.To)
	}
	if !isAddress(emailFrom(d)) {
		return fmt.Errorf("%q is not an email address: say who the mail is from", emailFrom(d))
	}
	return nil
}

// isAddress says whether text is one bare email address, like ada@example.com.
func isAddress(text string) bool {
	address, err := mail.ParseAddress(text)
	return err == nil && address.Address == text
}

// emailFrom is who the mail is from: From, or the user, which most SMTP
// servers want to be the same address.
func emailFrom(d Destination) string {
	if d.From != "" {
		return d.From
	}
	return d.User
}

// sendEmail sends m as one mail through the SMTP server of d. The server
// must turn the connection to TLS with STARTTLS before the password goes
// over it: without TLS nothing is sent.
func (s *Sender) sendEmail(ctx context.Context, d Destination, m Message) error {
	ctx, cancel := context.WithTimeout(ctx, emailTimeout)
	defer cancel()
	connection, err := s.dialer.DialContext(ctx, "tcp", d.SMTP)
	if err != nil {
		return err
	}
	defer connection.Close()
	deadline, _ := ctx.Deadline()
	_ = connection.SetDeadline(deadline)

	host, _, _ := net.SplitHostPort(d.SMTP)
	client, err := smtp.NewClient(connection, host)
	if err != nil {
		return err
	}
	if ok, _ := client.Extension("STARTTLS"); !ok {
		return errors.New("the SMTP server offers no STARTTLS, so nothing was sent: the password would travel in the clear. Most servers take it on port 587")
	}
	if err := client.StartTLS(&tls.Config{ServerName: host, RootCAs: emailRoots}); err != nil {
		return err
	}
	if err := client.Auth(smtp.PlainAuth("", d.User, d.Password, host)); err != nil {
		return err
	}
	if err := client.Mail(emailFrom(d)); err != nil {
		return err
	}
	if err := client.Rcpt(d.To); err != nil {
		return err
	}
	body, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := body.Write(emailText(d, m)); err != nil {
		return err
	}
	if err := body.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// emailText is an alert as a mail: the heading is the subject, the lines
// are the text.
func emailText(d Destination, m Message) []byte {
	from := mail.Address{Name: build.Name, Address: emailFrom(d)}
	_, domain, _ := strings.Cut(from.Address, "@")
	headers := []string{
		"From: " + from.String(),
		"To: " + d.To,
		"Subject: " + mime.QEncoding.Encode("utf-8", heading(m)),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"Message-ID: <" + rand.Text() + "@" + domain + ">",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: 8bit",
	}
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + strings.Join(lines(m), "\r\n") + "\r\n")
}
