// Package config reads the site's settings from the environment.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	RenderConfig
	// Development reads content/ and assets/ from disk on every request,
	// so an edit shows on reload: APP_ENV=development. The image sets
	// APP_ENV=production.
	Development bool
	Port        string
	// OldURLs are addresses the site had before APP_URL, such as
	// https://old.example.com. A request on one goes on to APP_URL.
	OldURLs []string
}

// RenderConfig is what templates may know of the settings: nothing secret.
type RenderConfig struct {
	AppURL string
	// The Umami page counter, on the Umami server UmamiHost, such as
	// umami.example.com. Off without both.
	UmamiWebsiteID string
	UmamiHost      string
}

func Load() (*Config, error) {
	cfg := &Config{
		RenderConfig: RenderConfig{
			AppURL:         env("APP_URL", ""),
			UmamiWebsiteID: env("UMAMI_WEBSITE_ID", ""),
			UmamiHost:      env("UMAMI_HOST", ""),
		},
		Port: env("PORT", "8090"),
	}
	var errs []error
	switch env("APP_ENV", "") {
	case "development":
		cfg.Development = true
	case "production":
	default:
		errs = append(errs, errors.New(`APP_ENV must be "development" or "production"`))
	}
	if !absoluteURL(cfg.AppURL) {
		errs = append(errs, errors.New("APP_URL must be an absolute HTTP or HTTPS URL"))
	}
	for _, old := range strings.Split(env("OLD_URLS", ""), ",") {
		old = strings.TrimRight(strings.TrimSpace(old), "/")
		if old == "" {
			continue
		}
		if !absoluteURL(old) {
			errs = append(errs, fmt.Errorf("OLD_URLS: %q is no absolute HTTP or HTTPS URL", old))
		}
		cfg.OldURLs = append(cfg.OldURLs, old)
	}
	if port, err := strconv.Atoi(cfg.Port); err != nil || port < 1 || port > 65535 {
		errs = append(errs, errors.New("PORT must be a number between 1 and 65535"))
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func absoluteURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}
