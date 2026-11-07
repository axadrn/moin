package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// AlertSignatureHeader carries the HMAC-SHA256 of a webhook body.
const AlertSignatureHeader = "X-Moin-Signature"

// webhookDestination posts the alert as JSON to any http or https URL,
// signed with the secret of the destination.
type webhookDestination struct{}

func (webhookDestination) check(input AlertDestinationInput) (alertTarget, error) {
	target := strings.TrimSpace(input.URL)
	parsed, ok := alertURL(target)
	if !ok || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return alertTarget{}, ErrAlertURL
	}
	return alertTarget{target: target}, nil
}

// hint shows the scheme and the host; a path may hold a token.
func (webhookDestination) hint(to alertTarget) string {
	parsed, ok := alertURL(to.target)
	if !ok {
		return ""
	}
	if parsed.Path == "" || parsed.Path == "/" {
		return parsed.Scheme + "://" + parsed.Host
	}
	return parsed.Scheme + "://" + parsed.Host + "/…"
}

func (webhookDestination) send(ctx context.Context, sender *alertSender, to alertTarget, message alertMessage) error {
	return sender.post(ctx, to.target, message.Payload, func(body []byte) http.Header {
		return http.Header{AlertSignatureHeader: {Signature(to.signingSecret, body)}}
	}, to.target, to.signingSecret)
}

// Signature is the value of the signature header of a webhook body:
// sha256= and the hex HMAC-SHA256 of the body under the signing secret.
func Signature(signingSecret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
