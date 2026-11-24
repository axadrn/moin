package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/axadrn/moin/hub/internal/testutil"
)

func TestEmailServiceBuildsInvitationMessage(t *testing.T) {
	provider := testutil.NewMemoryMailer()
	service := NewEmailService(provider, "https://app.example.com", "Example App")

	if err := service.SendWorkspaceInvitationEmail("reader@example.com", "secret-token", "Example Team", "Owner"); err != nil {
		t.Fatalf("SendWorkspaceInvitationEmail() error = %v", err)
	}
	messages := provider.Messages()
	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(messages))
	}
	message := messages[0]
	if message.To != "reader@example.com" {
		t.Fatalf("recipient = %q", message.To)
	}
	if !strings.Contains(message.Text, "https://app.example.com/workspaces/invitations/secret-token") {
		t.Fatalf("plain text message does not contain the invitation link: %q", message.Text)
	}
	if !strings.Contains(message.HTML, `href="https://app.example.com/workspaces/invitations/secret-token"`) {
		t.Fatalf("HTML message does not contain the clickable invitation link: %q", message.HTML)
	}
}

func TestEmailRendererEscapesHTML(t *testing.T) {
	body, err := renderEmail(emailContent{
		Subject:     "Welcome",
		Heading:     `<script>alert("heading")</script>`,
		Intro:       []string{`Hello <strong>reader</strong>`},
		ActionLabel: "Open",
		ActionURL:   "https://example.com/path?one=1&two=2",
		AppName:     `<script>alert("app")</script>`,
	})
	if err != nil {
		t.Fatalf("renderEmail() error = %v", err)
	}
	if strings.Contains(body.HTML, "<script>") || strings.Contains(body.HTML, "<strong>") {
		t.Fatalf("rendered HTML contains unescaped input: %q", body.HTML)
	}
	for _, expected := range []string{"&lt;script&gt;", "&lt;strong&gt;", "one=1&amp;two=2"} {
		if !strings.Contains(body.HTML, expected) {
			t.Fatalf("rendered HTML does not contain %q: %q", expected, body.HTML)
		}
	}
}

func TestActionEmailTemplatesRenderClickableLinks(t *testing.T) {
	const actionURL = "https://app.example.com/action/secret-token"
	tests := []struct {
		name    string
		content emailContent
	}{
		{name: "Welcome", content: welcomeEmailTemplate("Reader", actionURL, "Example App")},
		{name: "Workspace invitation", content: workspaceInvitationEmailTemplate(actionURL, "Example Team", "Owner", "Example App")},
		{name: "Email change verification", content: emailChangeVerificationTemplate("Reader", actionURL, "Example App")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := renderEmail(test.content)
			if err != nil {
				t.Fatalf("renderEmail() error = %v", err)
			}
			if !strings.Contains(body.Text, actionURL) {
				t.Fatalf("plain text message does not contain action URL: %q", body.Text)
			}
			if !strings.Contains(body.HTML, `href="`+actionURL+`"`) {
				t.Fatalf("HTML message does not contain clickable action URL: %q", body.HTML)
			}
		})
	}
}

func TestEmailServicePropagatesMailerError(t *testing.T) {
	providerError := errors.New("mailer unavailable")
	provider := testutil.NewMemoryMailer()
	provider.SetError(providerError)
	service := NewEmailService(provider, "https://app.example.com", "Example App")

	err := service.SendWelcomeEmail("reader@example.com", "Reader")
	if !errors.Is(err, providerError) {
		t.Fatalf("SendWelcomeEmail() error = %v, want provider error", err)
	}
}
