package mailer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

// Cloudflare sends through Cloudflare Email Sending over its HTTPS API.
type Cloudflare struct {
	accountID string
	token     string
	from      string
	api       string
	client    *http.Client
}

// NewCloudflare sends from from, with the API token of the account. api
// is https://api.cloudflare.com/client/v4.
func NewCloudflare(accountID, token, from, api string) *Cloudflare {
	return &Cloudflare{
		accountID: accountID,
		token:     token,
		from:      from,
		api:       strings.TrimRight(api, "/"),
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

type cloudflareAddress struct {
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
}

type cloudflareRequest struct {
	From    cloudflareAddress `json:"from"`
	To      string            `json:"to"`
	Subject string            `json:"subject"`
	Text    string            `json:"text"`
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

// Send sends one mail. A bounce is an error too.
func (m *Cloudflare) Send(ctx context.Context, message Message) error {
	from := cloudflareAddress{Address: m.from}
	if parsed, err := mail.ParseAddress(m.from); err == nil {
		from = cloudflareAddress{Address: parsed.Address, Name: parsed.Name}
	}
	body, err := json.Marshal(cloudflareRequest{From: from, To: message.To, Subject: message.Subject, Text: message.Text})
	if err != nil {
		return fmt.Errorf("encode mail: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, m.api+"/accounts/"+m.accountID+"/email/sending/send", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build mail request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+m.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := m.client.Do(request)
	if err != nil {
		return fmt.Errorf("send mail: %w", err)
	}
	defer response.Body.Close()
	var out cloudflareResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&out); err != nil {
		return fmt.Errorf("send mail: cloudflare status %d", response.StatusCode)
	}
	if !out.Success {
		if len(out.Errors) > 0 {
			return fmt.Errorf("send mail: cloudflare: %s (%d)", out.Errors[0].Message, out.Errors[0].Code)
		}
		return fmt.Errorf("send mail: cloudflare status %d", response.StatusCode)
	}
	if out.Result != nil && len(out.Result.PermanentBounces) > 0 {
		return fmt.Errorf("send mail: %s bounced", message.To)
	}
	return nil
}
