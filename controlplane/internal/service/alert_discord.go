package service

import (
	"context"
	"slices"
	"strings"

	"github.com/axadrn/moin/controlplane/internal/brand"
)

var discordHosts = []string{"discord.com", "discordapp.com", "ptb.discord.com", "canary.discord.com"}

// discordDestination posts to a webhook of a Discord channel.
type discordDestination struct{}

func (discordDestination) check(input AlertDestinationInput) (alertTarget, error) {
	target := strings.TrimSpace(input.URL)
	parsed, ok := alertURL(target)
	if !ok || parsed.Scheme != "https" || !slices.Contains(discordHosts, parsed.Hostname()) || !strings.HasPrefix(parsed.Path, "/api/webhooks/") {
		return alertTarget{}, ErrAlertURL
	}
	return alertTarget{target: target}, nil
}

func (discordDestination) hint(to alertTarget) string { return chatHint(to.target) }

func (discordDestination) send(ctx context.Context, sender *alertSender, to alertTarget, message alertMessage) error {
	body := map[string]any{"content": discordText(message), "allowed_mentions": map[string]any{"parse": []string{}}}
	return sender.post(ctx, to.target, body, nil, to.target)
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
