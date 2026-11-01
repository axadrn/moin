package mailer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"time"
)

const cloudflareAPI = "https://api.cloudflare.com/client/v4"

// CloudflareMailer sends through Cloudflare Email Sending over its HTTPS API.
type CloudflareMailer struct {
	accountID string
	token     string
	from      string
	api       string
	client    *http.Client
}

func NewCloudflareMailer(accountID, token, from string) *CloudflareMailer {
	return &CloudflareMailer{
		accountID: accountID,
		token:     token,
		from:      from,
		api:       cloudflareAPI,
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (m *CloudflareMailer) Name() string { return ProviderCloudflare }

type cloudflareAddress struct {
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
}

type cloudflareRequest struct {
	From    cloudflareAddress `json:"from"`
	To      string            `json:"to"`
	Subject string            `json:"subject"`
	Text    string            `json:"text"`
	HTML    string            `json:"html,omitempty"`
}

type cloudflareResponse struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result *struct {
		PermanentBounces []string `json:"permanent_bounces"`
	} `json:"result"`
}

func (m *CloudflareMailer) Send(ctx context.Context, message Message) error {
	from := cloudflareAddress{Address: m.from}
	if parsed, err := mail.ParseAddress(m.from); err == nil {
		from = cloudflareAddress{Address: parsed.Address, Name: parsed.Name}
	}
	body, err := json.Marshal(cloudflareRequest{
		From:    from,
		To:      message.To,
		Subject: message.Subject,
		Text:    message.Text,
		HTML:    message.HTML,
	})
	if err != nil {
		return fmt.Errorf("encode email: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, m.api+"/accounts/"+m.accountID+"/email/sending/send", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build email request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+m.token)
	request.Header.Set("Content-Type", "application/json")

	response, err := m.client.Do(request)
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}
	defer response.Body.Close()

	var out cloudflareResponse
	err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&out)
	if err != nil {
		return fmt.Errorf("send email: cloudflare status %d", response.StatusCode)
	}
	if !out.Success {
		if len(out.Errors) > 0 {
			return fmt.Errorf("send email: cloudflare: %s (%d)", out.Errors[0].Message, out.Errors[0].Code)
		}
		return fmt.Errorf("send email: cloudflare status %d", response.StatusCode)
	}
	if out.Result != nil && len(out.Result.PermanentBounces) > 0 {
		return fmt.Errorf("send email: %s bounced", message.To)
	}
	return nil
}
