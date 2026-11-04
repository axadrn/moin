package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/moin/controlplane/internal/brand"
	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/axadrn/moin/internal/build"
	"github.com/google/uuid"
)

var (
	ErrAlertKind     = errors.New("choose slack, discord, telegram or webhook")
	ErrAlertName     = errors.New("alert destination name must be 1 to 63 lowercase letters, digits or dashes")
	ErrAlertURL      = errors.New("alert destination URL does not fit its kind")
	ErrAlertBotToken = errors.New("telegram bot token looks like 123456:ABC-DEF…")
	ErrAlertChatID   = errors.New("telegram chat ID is a number like -1001234567890 or @channel")
	ErrAlertEvents   = errors.New("unknown alert event")
	ErrAlertLimit    = errors.New("a workspace has at most 20 alert destinations")
)

// AlertSendError says why a destination did not take an alert, without
// its URL or token.
type AlertSendError struct {
	Message string
}

func (e *AlertSendError) Error() string { return e.Message }

const (
	// alertTimeout is how long one destination has to take an alert.
	alertTimeout = 10 * time.Second
	// alertDestinationLimit is how many destinations a workspace may have.
	alertDestinationLimit = 20
	// AlertSignatureHeader carries the HMAC-SHA256 of a webhook body.
	AlertSignatureHeader = "X-Moin-Signature"
	// legacySignatureHeader is the same under the old name, sent too
	// until build.LegacyUntil so receivers have time to switch.
	legacySignatureHeader = "X-Ops-Signature"
)

var (
	alertNamePattern     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	telegramTokenPattern = regexp.MustCompile(`^[0-9]{3,20}:[A-Za-z0-9_-]{20,100}$`)
	telegramChatPattern  = regexp.MustCompile(`^(-?[0-9]{1,20}|@[A-Za-z][A-Za-z0-9_]{4,31})$`)
	discordHosts         = []string{"discord.com", "discordapp.com", "ptb.discord.com", "canary.discord.com"}
)

// AlertDestinationInput is a new alert destination. URL is the incoming
// webhook of Slack or Discord, or the webhook; BotToken and ChatID are for
// Telegram. No events means all of them.
type AlertDestinationInput struct {
	Kind     string
	Name     string
	URL      string
	BotToken string
	ChatID   string
	Events   []string
}

// AllowOnlyPublicTargets makes alerts refuse private and local addresses.
// The hosted control plane turns it on: otherwise a webhook could call into
// its own network. A self-hosted one may post to a service next to it.
func (s *AlertService) AllowOnlyPublicTargets() { s.sender = newAlertSender(true) }

// Destinations lists the alert destinations of a workspace. Every member
// may see them; the targets stay sealed.
func (s *AlertService) Destinations(workspaceID string) ([]model.AlertDestination, error) {
	return s.destinations.List(workspaceID)
}

// manager is the actor of an owner or admin, who manage the destinations.
func manager(member *model.WorkspaceMember, source Source) (Actor, error) {
	if member == nil || !member.Can(model.WorkspacePermissionManageWorkspace) {
		return Actor{}, ErrWorkspacePermission
	}
	return Actor{WorkspaceID: member.WorkspaceID, UserID: member.UserID, Source: source}, nil
}

// AddDestination checks and stores a new alert destination of the member's
// workspace. A webhook gets a signing secret, returned this once.
func (s *AlertService) AddDestination(member *model.WorkspaceMember, source Source, input AlertDestinationInput) (model.AlertDestination, string, error) {
	actor, err := manager(member, source)
	if err != nil {
		return model.AlertDestination{}, "", err
	}
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	if !slices.Contains(model.AlertKinds, kind) {
		return model.AlertDestination{}, "", ErrAlertKind
	}
	name := strings.TrimSpace(input.Name)
	if !alertNamePattern.MatchString(name) {
		return model.AlertDestination{}, "", ErrAlertName
	}
	events, err := alertEvents(input.Events)
	if err != nil {
		return model.AlertDestination{}, "", err
	}
	target, chatID := strings.TrimSpace(input.URL), strings.TrimSpace(input.ChatID)
	if kind == model.AlertTelegram {
		target = strings.TrimSpace(input.BotToken)
		if !telegramTokenPattern.MatchString(target) {
			return model.AlertDestination{}, "", ErrAlertBotToken
		}
		if !telegramChatPattern.MatchString(chatID) {
			return model.AlertDestination{}, "", ErrAlertChatID
		}
	} else {
		chatID = ""
		if !validAlertURL(kind, target) {
			return model.AlertDestination{}, "", ErrAlertURL
		}
	}
	count, err := s.destinations.Count(actor.WorkspaceID)
	if err != nil {
		return model.AlertDestination{}, "", err
	}
	if count >= alertDestinationLimit {
		return model.AlertDestination{}, "", ErrAlertLimit
	}
	destination := model.AlertDestination{
		ID: uuid.New().String(), WorkspaceID: actor.WorkspaceID, Name: name, Kind: kind,
		ChatID: chatID, Hint: alertHint(kind, target, chatID), Events: strings.Join(events, ","),
		CreatedAt: s.now().UTC(),
	}
	destination.Target = s.box.Seal(target, destination.ID+"/target")
	signingSecret := ""
	if kind == model.AlertWebhook {
		raw := make([]byte, 32)
		_, _ = rand.Read(raw)
		signingSecret = hex.EncodeToString(raw)
		destination.Secret = s.box.Seal(signingSecret, destination.ID+"/secret")
	}
	if err := s.destinations.Create(destination); err != nil {
		return model.AlertDestination{}, "", err
	}
	recordActivity(s.audit, s.now(), actor, "alert.added", "alert", name, map[string]string{"kind": kind, "events": destination.Events})
	return destination, signingSecret, nil
}

// RemoveDestination deletes an alert destination by ID or name.
func (s *AlertService) RemoveDestination(member *model.WorkspaceMember, source Source, idOrName string) error {
	actor, err := manager(member, source)
	if err != nil {
		return err
	}
	destination, err := s.destinations.ByIDOrName(actor.WorkspaceID, idOrName)
	if err != nil {
		return err
	}
	if err := s.destinations.Delete(actor.WorkspaceID, destination.ID); err != nil {
		return err
	}
	recordActivity(s.audit, s.now(), actor, "alert.removed", "alert", destination.Name, map[string]string{"kind": destination.Kind})
	return nil
}

// TestDestination sends a test alert to a destination and returns it as it
// is after. A failure is an *AlertSendError.
func (s *AlertService) TestDestination(ctx context.Context, member *model.WorkspaceMember, source Source, idOrName string) (model.AlertDestination, error) {
	actor, err := manager(member, source)
	if err != nil {
		return model.AlertDestination{}, err
	}
	destination, err := s.destinations.ByIDOrName(actor.WorkspaceID, idOrName)
	if err != nil {
		return model.AlertDestination{}, err
	}
	workspace, err := s.workspaces.ByID(actor.WorkspaceID)
	if err != nil {
		return model.AlertDestination{}, err
	}
	labels := []string{}
	for _, event := range destination.EventList() {
		labels = append(labels, model.AlertEventLabel(event))
	}
	message := alertMessage{
		Symbol:  "🔔",
		Heading: "Test alert from " + s.email.appName,
		Lines:   []string{"Alerts of " + workspace.Name + " come here: " + strings.Join(labels, ", ") + "."},
		URL:     s.email.appURL + "/app/" + workspace.Slug + "/alerts",
	}
	message.Payload = alertPayload{Event: "test", Workspace: workspace.Slug, URL: message.URL}
	sendErr := s.deliver(ctx, *destination, message)
	s.result(*destination, sendErr, false)
	result := "ok"
	if sendErr != nil {
		result = sendErr.Error()
	}
	recordActivity(s.audit, s.now(), actor, "alert.tested", "alert", destination.Name, map[string]string{"kind": destination.Kind, "result": result})
	if updated, err := s.destinations.ByIDOrName(actor.WorkspaceID, destination.ID); err == nil {
		destination = updated
	}
	if sendErr != nil {
		return *destination, sendErr
	}
	return *destination, nil
}

// post sends an alert to every destination of the workspace that gets it,
// one after the other, each within alertTimeout.
func (s *AlertService) post(workspace *model.Workspace, action string, message alertMessage) {
	destinations, err := s.destinations.List(workspace.ID)
	if err != nil {
		slog.Error("alert: list the destinations failed", "workspace_id", workspace.ID, "action", action, "error", err)
		return
	}
	for _, destination := range destinations {
		if !destination.Gets(action) {
			continue
		}
		err := s.deliver(context.Background(), destination, message)
		if err != nil {
			slog.Warn("alert: post failed", "workspace_id", workspace.ID, "action", action, "destination", destination.Name, "kind", destination.Kind, "error", err)
		}
		s.result(destination, err, true)
	}
}

// result stores how a send went. A destination that begins to fail is in
// the activity once, and once more when it works again; a test says so
// itself.
func (s *AlertService) result(destination model.AlertDestination, err error, record bool) {
	failure := ""
	if err != nil {
		failure = clipText(err.Error(), 300)
	}
	if storeErr := s.destinations.SetResult(destination.ID, failure, s.now().UTC()); storeErr != nil {
		slog.Error("alert: store the result failed", "destination", destination.Name, "error", storeErr)
	}
	if !record {
		return
	}
	system := System(destination.WorkspaceID)
	switch {
	case err != nil && destination.FailingSince == nil:
		recordActivity(s.audit, s.now(), system, "alert.failed", "alert", destination.Name, map[string]string{"kind": destination.Kind, "error": failure})
	case err == nil && destination.FailingSince != nil:
		recordActivity(s.audit, s.now(), system, "alert.recovered", "alert", destination.Name, map[string]string{"kind": destination.Kind})
	}
}

// deliver posts one alert to one destination.
func (s *AlertService) deliver(ctx context.Context, destination model.AlertDestination, message alertMessage) error {
	target, err := s.box.Open(destination.Target, destination.ID+"/target")
	if err != nil {
		return &AlertSendError{Message: "the destination cannot be opened with this control plane's key; add it again"}
	}
	switch destination.Kind {
	case model.AlertSlack:
		return s.sender.post(ctx, target, map[string]any{"text": slackText(message)}, nil, target)
	case model.AlertDiscord:
		body := map[string]any{"content": discordText(message), "allowed_mentions": map[string]any{"parse": []string{}}}
		return s.sender.post(ctx, target, body, nil, target)
	case model.AlertTelegram:
		body := map[string]any{"chat_id": destination.ChatID, "text": telegramText(message), "parse_mode": "HTML", "disable_web_page_preview": true}
		return s.sender.post(ctx, s.sender.telegramAPI+"/bot"+target+"/sendMessage", body, nil, target)
	case model.AlertWebhook:
		signingSecret, err := s.box.Open(destination.Secret, destination.ID+"/secret")
		if err != nil {
			return &AlertSendError{Message: "the signing secret cannot be opened with this control plane's key; add the destination again"}
		}
		return s.sender.post(ctx, target, message.Payload, func(body []byte) http.Header {
			signature := Signature(signingSecret, body)
			header := http.Header{AlertSignatureHeader: {signature}}
			if build.Legacy() {
				header.Set(legacySignatureHeader, signature)
			}
			return header
		}, target, signingSecret)
	}
	return &AlertSendError{Message: "unknown kind " + destination.Kind}
}

// Signature is the value of the signature header of a webhook body:
// sha256= and the hex HMAC-SHA256 of the body under the signing secret.
func Signature(signingSecret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// alertSender posts alerts over HTTP.
type alertSender struct {
	client *http.Client
	// telegramAPI is the Bot API; tests point it at a fake.
	telegramAPI string
}

// newAlertSender follows no redirects, and with publicOnly connects only
// to public addresses: the check runs when the connection opens, after
// DNS, so a name that points inside cannot slip past it.
func newAlertSender(publicOnly bool) *alertSender {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if publicOnly {
		dialer.Control = refusePrivate
		transport.Proxy = nil
	}
	transport.DialContext = dialer.DialContext
	return &alertSender{
		client: &http.Client{
			Timeout:       alertTimeout,
			Transport:     transport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		telegramAPI: "https://api.telegram.org",
	}
}

// post sends body as JSON to address. headers adds headers for the body.
// secrets never show in the error: an address holds a token.
func (a *alertSender) post(ctx context.Context, address string, body any, headers func([]byte) http.Header, secrets ...string) error {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(body); err != nil {
		return err
	}
	encoded := bytes.TrimSuffix(buffer.Bytes(), []byte("\n"))
	ctx, cancel := context.WithTimeout(ctx, alertTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(encoded))
	if err != nil {
		return &AlertSendError{Message: "the address is not a valid URL"}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", brand.Name+"-alerts")
	if headers != nil {
		for key, values := range headers(encoded) {
			request.Header[key] = values
		}
	}
	response, err := a.client.Do(request)
	if err != nil {
		return &AlertSendError{Message: scrub(sendFailure(err), secrets)}
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
	return &AlertSendError{Message: scrub(message, secrets)}
}

// sendFailure says why a request got no answer, without its URL.
func sendFailure(err error) string {
	var netErr net.Error
	switch {
	case errors.Is(err, ErrBucketPrivate):
		return "the address is not on the public internet"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return fmt.Sprintf("no answer within %s", alertTimeout)
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	return err.Error()
}

// scrub takes every secret out of text.
func scrub(text string, secrets []string) string {
	for _, value := range secrets {
		if value != "" {
			text = strings.ReplaceAll(text, value, "…")
		}
	}
	return text
}

// alertEvents checks the events of a destination and puts them in order;
// none means all.
func alertEvents(events []string) ([]string, error) {
	if len(events) == 0 {
		return slices.Clone(model.AlertEvents), nil
	}
	chosen := []string{}
	for _, event := range events {
		event = strings.TrimSpace(event)
		if !slices.Contains(model.AlertEvents, event) {
			return nil, ErrAlertEvents
		}
		chosen = append(chosen, event)
	}
	ordered := []string{}
	for _, event := range model.AlertEvents {
		if slices.Contains(chosen, event) {
			ordered = append(ordered, event)
		}
	}
	return ordered, nil
}

// validAlertURL says whether address fits kind: an incoming webhook of
// Slack or Discord, or any http or https URL for a webhook.
func validAlertURL(kind, address string) bool {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	switch kind {
	case model.AlertSlack:
		return parsed.Scheme == "https" && parsed.Hostname() == "hooks.slack.com" && strings.HasPrefix(parsed.Path, "/services/")
	case model.AlertDiscord:
		return parsed.Scheme == "https" && slices.Contains(discordHosts, parsed.Hostname()) && strings.HasPrefix(parsed.Path, "/api/webhooks/")
	case model.AlertWebhook:
		return parsed.Scheme == "https" || parsed.Scheme == "http"
	}
	return false
}

// alertHint is what lists show of a target: never all of it.
func alertHint(kind, target, chatID string) string {
	if kind == model.AlertTelegram {
		return "chat " + chatID
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return ""
	}
	if kind == model.AlertWebhook {
		if parsed.Path == "" || parsed.Path == "/" {
			return parsed.Scheme + "://" + parsed.Host
		}
		return parsed.Scheme + "://" + parsed.Host + "/…"
	}
	last := target
	if len(last) > 4 {
		last = last[len(last)-4:]
	}
	return parsed.Host + "/…" + last
}

// slackText is an alert in Slack's mrkdwn.
func slackText(message alertMessage) string {
	escape := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace
	lines := []string{"*" + escape(message.Title()) + "*"}
	for _, line := range message.Lines {
		lines = append(lines, escape(line))
	}
	lines = append(lines, "<"+message.URL+"|Open in "+brand.Name+">")
	return strings.Join(lines, "\n")
}

// discordText is an alert in Discord's markdown. The link sits in angle
// brackets, so Discord shows no preview of it.
func discordText(message alertMessage) string {
	escape := strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "~", `\~`, "`", "\\`", "|", `\|`).Replace
	lines := []string{"**" + escape(message.Title()) + "**"}
	for _, line := range message.Lines {
		lines = append(lines, escape(line))
	}
	lines = append(lines, "Open in "+brand.Name+": <"+message.URL+">")
	return strings.Join(lines, "\n")
}

// telegramText is an alert in Telegram's HTML.
func telegramText(message alertMessage) string {
	lines := []string{"<b>" + html.EscapeString(message.Title()) + "</b>"}
	for _, line := range message.Lines {
		lines = append(lines, html.EscapeString(line))
	}
	lines = append(lines, `<a href="`+html.EscapeString(message.URL)+`">Open in `+brand.Name+`</a>`)
	return strings.Join(lines, "\n")
}
