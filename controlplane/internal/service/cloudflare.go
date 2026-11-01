package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Cloudflare talks to the Cloudflare API with the user's own token: it
// creates a tunnel per server, routes the app domains through it, points
// their DNS at it and turns on Always Use HTTPS.
type Cloudflare struct {
	apiURL string
	http   *http.Client
}

// NewCloudflare talks to apiURL, https://api.cloudflare.com/client/v4 by
// default.
func NewCloudflare(apiURL string) *Cloudflare {
	if apiURL == "" {
		apiURL = "https://api.cloudflare.com/client/v4"
	}
	return &Cloudflare{apiURL: strings.TrimRight(apiURL, "/"), http: &http.Client{Timeout: 30 * time.Second}}
}

// CloudflareError is an answer of the Cloudflare API that is not a success.
type CloudflareError struct {
	Status  int
	Message string
}

func (e *CloudflareError) Error() string {
	return fmt.Sprintf("cloudflare: %d %s", e.Status, e.Message)
}

// Account returns the account the token may use.
func (c *Cloudflare) Account(ctx context.Context, token string) (string, error) {
	var accounts []struct {
		ID string `json:"id"`
	}
	err := c.call(ctx, token, http.MethodGet, "/accounts", nil, &accounts)
	if err != nil {
		return "", err
	}
	if len(accounts) == 0 {
		return "", errors.New("the token sees no account")
	}
	return accounts[0].ID, nil
}

// CreateTunnel creates a tunnel whose routes Cloudflare keeps, and returns
// its ID and the token cloudflared runs with.
func (c *Cloudflare) CreateTunnel(ctx context.Context, token, account, name string) (string, string, error) {
	var tunnel struct {
		ID string `json:"id"`
	}
	err := c.call(ctx, token, http.MethodPost, "/accounts/"+account+"/cfd_tunnel", map[string]string{"name": name, "config_src": "cloudflare"}, &tunnel)
	if err != nil {
		return "", "", err
	}
	var tunnelToken string
	err = c.call(ctx, token, http.MethodGet, "/accounts/"+account+"/cfd_tunnel/"+tunnel.ID+"/token", nil, &tunnelToken)
	return tunnel.ID, tunnelToken, err
}

// DeleteTunnel deletes a tunnel and its connections.
func (c *Cloudflare) DeleteTunnel(ctx context.Context, token, account, tunnelID string) error {
	return c.call(ctx, token, http.MethodDelete, "/accounts/"+account+"/cfd_tunnel/"+tunnelID+"?cascade=true", nil, nil)
}

// RouteTunnel sends every host to Traefik's tunnel entrypoint and
// everything else to a 404.
func (c *Cloudflare) RouteTunnel(ctx context.Context, token, account, tunnelID string, hosts []string) error {
	ingress := []map[string]string{}
	for _, host := range hosts {
		ingress = append(ingress, map[string]string{"hostname": host, "service": "http://ops-traefik:8080"})
	}
	ingress = append(ingress, map[string]string{"service": "http_status:404"})
	body := map[string]any{"config": map[string]any{"ingress": ingress}}
	return c.call(ctx, token, http.MethodPut, "/accounts/"+account+"/cfd_tunnel/"+tunnelID+"/configurations", body, nil)
}

// PointAtTunnel makes host a proxied CNAME to the tunnel, in the zone of
// the account that holds it, and turns on Always Use HTTPS there.
func (c *Cloudflare) PointAtTunnel(ctx context.Context, token, host, tunnelID string) error {
	zone, err := c.zone(ctx, token, host)
	if err != nil {
		return err
	}
	var records []struct {
		ID string `json:"id"`
	}
	err = c.call(ctx, token, http.MethodGet, "/zones/"+zone+"/dns_records?name="+url.QueryEscape(host), nil, &records)
	if err != nil {
		return err
	}
	record := map[string]any{"type": "CNAME", "name": host, "content": tunnelID + ".cfargotunnel.com", "proxied": true, "comment": "ops tunnel"}
	if len(records) > 0 {
		err = c.call(ctx, token, http.MethodPut, "/zones/"+zone+"/dns_records/"+records[0].ID, record, nil)
	} else {
		err = c.call(ctx, token, http.MethodPost, "/zones/"+zone+"/dns_records", record, nil)
	}
	if err != nil {
		return err
	}
	return c.call(ctx, token, http.MethodPatch, "/zones/"+zone+"/settings/always_use_https", map[string]string{"value": "on"}, nil)
}

// zone finds the zone of a host: blog.example.com is in example.com.
func (c *Cloudflare) zone(ctx context.Context, token, host string) (string, error) {
	labels := strings.Split(host, ".")
	for index := 0; index < len(labels)-1; index++ {
		var zones []struct {
			ID string `json:"id"`
		}
		err := c.call(ctx, token, http.MethodGet, "/zones?name="+url.QueryEscape(strings.Join(labels[index:], ".")), nil, &zones)
		if err != nil {
			return "", err
		}
		if len(zones) > 0 {
			return zones[0].ID, nil
		}
	}
	return "", fmt.Errorf("%s is in no zone of this Cloudflare account", host)
}

func (c *Cloudflare) call(ctx context.Context, token, method, path string, body, result any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.apiURL+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var envelope struct {
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&envelope)
	if err != nil || !envelope.Success {
		message := response.Status
		if len(envelope.Errors) > 0 {
			message = envelope.Errors[0].Message
		}
		return &CloudflareError{Status: response.StatusCode, Message: message}
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(envelope.Result, result)
}
