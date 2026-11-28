// Package alert sends one alert to one destination: Telegram, Slack,
// Discord or a webhook. The daemon alerts what it sees while its server
// lives; the watcher alerts silence and a domain that is down from outside.
package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/report"
)

// Destination is where alerts go: the same thing a report carries to the
// watchers. Kind is "telegram", "slack", "discord" or "webhook". Mail is
// not a kind: only moin cloud sends mail.
type Destination = report.Alert

// Message is one alert. A webhook gets it as JSON.
type Message struct {
	Server string `json:"server"` // "prod"
	App    string `json:"app"`    // "" for the whole server
	Title  string `json:"title"`  // one line: "shop: the backup of today did not restore"
	Text   string `json:"text"`   // a few lines more, may be ""
	OK     bool   `json:"ok"`     // true when something came back, like "shop is up again"
}

// telegramAPI is the Bot API; tests point it at a fake.
var telegramAPI = "https://api.telegram.org"

// client follows no redirects: a destination that moved answers wrong.
var client = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
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
	}
	return fmt.Errorf("unknown alert kind %q: use telegram, slack, discord or webhook", d.Kind)
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

// Send sends m to d. The error never holds the token or URL of d: both
// are secrets.
func Send(ctx context.Context, d Destination, m Message) error {
	return SendWith(ctx, client, d, m)
}

// SendWith is Send through the client of the caller: moin cloud brings
// one that connects to public addresses only. It must follow no redirects.
func SendWith(ctx context.Context, client *http.Client, d Destination, m Message) error {
	if err := Check(d); err != nil {
		return err
	}
	switch d.Kind {
	case "telegram":
		body := map[string]any{"chat_id": d.ChatID, "text": telegramText(m), "parse_mode": "HTML", "disable_web_page_preview": true}
		return post(ctx, client, telegramAPI+"/bot"+d.Token+"/sendMessage", body, d.Token)
	case "slack":
		return post(ctx, client, d.URL, map[string]any{"text": slackText(m)}, d.URL)
	case "discord":
		body := map[string]any{"content": discordText(m), "allowed_mentions": map[string]any{"parse": []string{}}}
		return post(ctx, client, d.URL, body, d.URL)
	}
	return post(ctx, client, d.URL, m, d.URL)
}

// post sends body as JSON to address and wants a 2xx back. The secret
// never shows in the error.
func post(ctx context.Context, client *http.Client, address string, body any, secret string) error {
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
