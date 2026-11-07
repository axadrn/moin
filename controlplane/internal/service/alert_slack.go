package service

import (
	"context"
	"strings"

	"github.com/axadrn/moin/controlplane/internal/brand"
)

// slackDestination posts to an incoming webhook of Slack.
type slackDestination struct{}

func (slackDestination) check(input AlertDestinationInput) (alertTarget, error) {
	target := strings.TrimSpace(input.URL)
	parsed, ok := alertURL(target)
	if !ok || parsed.Scheme != "https" || parsed.Hostname() != "hooks.slack.com" || !strings.HasPrefix(parsed.Path, "/services/") {
		return alertTarget{}, ErrAlertURL
	}
	return alertTarget{target: target}, nil
}

func (slackDestination) hint(to alertTarget) string { return chatHint(to.target) }

func (slackDestination) send(ctx context.Context, sender *alertSender, to alertTarget, message alertMessage) error {
	return sender.post(ctx, to.target, map[string]any{"text": slackText(message)}, nil, to.target)
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
