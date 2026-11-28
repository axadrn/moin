// Package alert sends one alert to one destination: Telegram, Slack,
// Discord, a webhook or an email. The daemon alerts what it sees while its server
// lives; the watcher alerts silence and a domain that is down from outside.
package alert

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/report"
)

// Destination is where alerts go: the same thing a report carries to the
// watchers. Kind is "telegram", "slack", "discord", "webhook" or "email":
// a mail through the SMTP server of the destination. The mail moin cloud
// sends to an account is not a kind here.
type Destination = report.Alert

// Message is one alert. A webhook gets it as JSON.
type Message struct {
	// Event is the kind of what happened, the same when it starts and
	// when it is over (OK): "disk", "crash", "down", "silent" and more,
	// listed in the docs of the watcher.
	Event  string `json:"event"`
	Server string `json:"server"` // "my-server"
	App    string `json:"app"`    // "" for the whole server
	Title  string `json:"title"`  // one line: "my-app: the backup of today did not restore"
	Text   string `json:"text"`   // a few lines more, may be ""
	OK     bool   `json:"ok"`     // true when something came back, like "my-app is up again"
}

// SignatureHeader carries the signature of a webhook post: sha256= and
// the hex HMAC-SHA256 of the body under the webhook's secret.
var SignatureHeader = http.CanonicalHeaderKey("X-" + build.Name + "-Signature")

// telegramAPI is the Bot API; tests point it at a fake.
var telegramAPI = "https://api.telegram.org"

// Sender sends alerts over connections it dials itself.
type Sender struct {
	client *http.Client
	// dialer connects to the SMTP server of an email.
	dialer *net.Dialer
}

// plain is the sender of Send: it connects anywhere.
var plain = &Sender{
	client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: noRedirects},
	dialer: &net.Dialer{Timeout: 5 * time.Second},
}

// noRedirects follows no redirects: a destination that moved answers wrong.
func noRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// NewSender is a sender whose every connection passes control first, the
// Control of a net.Dialer. A watcher brings one that refuses private
// addresses, for the destinations a report named. It uses no proxy: a
// proxy would connect in its place, past control.
func NewSender(control func(network, address string, conn syscall.RawConn) error) *Sender {
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: control}
	transport := &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: 5 * time.Second}
	return &Sender{client: &http.Client{Timeout: 10 * time.Second, Transport: transport, CheckRedirect: noRedirects}, dialer: dialer}
}

// Check says whether d can work: a known kind with what that kind needs.
func Check(d Destination) error {
	switch d.Kind {
	case "telegram":
		if d.Token == "" || d.ChatID == "" {
			return errors.New("a telegram alert needs a token and a chat_id")
		}
		return nil
	case "slack", "discord", "webhook":
		return checkURL(d.URL)
	case "email":
		return checkEmail(d)
	}
	return fmt.Errorf("unknown alert kind %q: use telegram, slack, discord, webhook or email", d.Kind)
}

// checkURL wants https, or http to this machine or a tailnet only: an
// alert says what is down where, and a URL may hold a token.
func checkURL(address string) error {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" {
		return errors.New("the alert url is not a valid URL")
	}
	if parsed.Scheme == "https" || parsed.Scheme == "http" && report.PlainHTTP(parsed.Hostname()) {
		return nil
	}
	return errors.New("the alert url must start with https:// (http:// only to this machine or a tailnet address)")
}

// Send sends m to d. The error never holds the token, URL or password
// of d: they are secrets.
func Send(ctx context.Context, d Destination, m Message) error {
	return plain.Send(ctx, d, m)
}

// Send sends m to d over the connections of s.
func (s *Sender) Send(ctx context.Context, d Destination, m Message) error {
	if err := Check(d); err != nil {
		return err
	}
	switch d.Kind {
	case "telegram":
		body := map[string]any{"chat_id": d.ChatID, "text": telegramText(m), "parse_mode": "HTML", "disable_web_page_preview": true}
		return post(ctx, s.client, telegramAPI+"/bot"+d.Token+"/sendMessage", body, d.Token, "")
	case "slack":
		return post(ctx, s.client, d.URL, map[string]any{"text": slackText(m)}, d.URL, "")
	case "discord":
		body := map[string]any{"content": discordText(m), "allowed_mentions": map[string]any{"parse": []string{}}}
		return post(ctx, s.client, d.URL, body, d.URL, "")
	case "email":
		if err := s.sendEmail(ctx, d, m); err != nil {
			return errors.New(scrub(err.Error(), d.Password))
		}
		return nil
	}
	return post(ctx, s.client, d.URL, m, d.URL, d.Secret)
}

// post sends body as JSON to address and wants a 2xx back, signed when
// signWith is not "". The secret never shows in the error.
func post(ctx context.Context, client *http.Client, address string, body any, secret, signWith string) error {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(body); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, &buffer)
	if err != nil {
		return errors.New("the alert url is not valid")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", build.Name+"/"+build.Version)
	if signWith != "" {
		mac := hmac.New(sha256.New, []byte(signWith))
		mac.Write(buffer.Bytes())
		request.Header.Set(SignatureHeader, "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	response, err := client.Do(request)
	if err != nil {
		return errors.New(scrub(err.Error(), secret))
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return nil
	}
	answer, _ := io.ReadAll(io.LimitReader(response.Body, 200))
	message := fmt.Sprintf("HTTP %d", response.StatusCode)
	if text := strings.Join(strings.Fields(string(answer)), " "); text != "" {
		message += ": " + text
	}
	return errors.New(scrub(message, secret))
}

// scrub takes the secret out of text.
func scrub(text, secret string) string {
	if secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "…")
}

// heading is the first line of every alert: a red or green dot and the title.
func heading(m Message) string {
	if m.OK {
		return "🟢 " + m.Title
	}
	return "🔴 " + m.Title
}

// lines are the lines under the heading: the text, then the server and app.
func lines(m Message) []string {
	var out []string
	for line := range strings.SplitSeq(strings.TrimSpace(m.Text), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	where := m.Server
	if m.App != "" {
		where += " · " + m.App
	}
	if where != "" {
		out = append(out, where)
	}
	return out
}

// telegramText is an alert in Telegram's HTML.
func telegramText(m Message) string {
	out := []string{"<b>" + html.EscapeString(heading(m)) + "</b>"}
	for _, line := range lines(m) {
		out = append(out, html.EscapeString(line))
	}
	return strings.Join(out, "\n")
}

// slackText is an alert in Slack's mrkdwn.
func slackText(m Message) string {
	escape := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace
	out := []string{"*" + escape(heading(m)) + "*"}
	for _, line := range lines(m) {
		out = append(out, escape(line))
	}
	return strings.Join(out, "\n")
}

// discordText is an alert in Discord's markdown.
func discordText(m Message) string {
	escape := strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "~", `\~`, "`", "\\`", "|", `\|`).Replace
	out := []string{"**" + escape(heading(m)) + "**"}
	for _, line := range lines(m) {
		out = append(out, escape(line))
	}
	return strings.Join(out, "\n")
}
