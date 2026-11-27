// Package config reads the site's settings from the environment.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/axadrn/moin/hub/internal/brand"
)

type Environment string

const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentProduction  Environment = "production"
)

type Config struct {
	AppEnv        Environment
	AppURL        string
	Port          string
	SupportEmail  string
	RobotsNoIndex bool
	// OldURLs are addresses the site had before APP_URL, such as
	// https://old.example.com. A request on one goes on to APP_URL.
	OldURLs []string
	LogHTTP bool

	// Analytics
	UmamiWebsiteID    string
	UmamiHost         string
	PlausibleDomain   string
	PlausibleHost     string
	GoogleAnalyticsID string
}

// RenderConfig is what templates may know of the configuration.
type RenderConfig struct {
	AppEnv            Environment
	AppURL            string
	SupportEmail      string
	RobotsNoIndex     bool
	UmamiWebsiteID    string
	UmamiHost         string
	PlausibleDomain   string
	PlausibleHost     string
	GoogleAnalyticsID string
}

func Load() (*Config, error) {
	cfg := &Config{
		AppEnv:            Environment(env("APP_ENV", "")),
		AppURL:            env("APP_URL", ""),
		Port:              env("PORT", "8090"),
		SupportEmail:      env("SUPPORT_EMAIL", brand.SupportEmail),
		UmamiWebsiteID:    env("UMAMI_WEBSITE_ID", ""),
		UmamiHost:         env("UMAMI_HOST", "cloud.umami.is"),
		PlausibleDomain:   env("PLAUSIBLE_DOMAIN", ""),
		PlausibleHost:     env("PLAUSIBLE_HOST", "plausible.io"),
		GoogleAnalyticsID: env("GOOGLE_ANALYTICS_ID", ""),
	}
	var errs []error
	boolean := func(key string, fallback bool) bool {
		value := env(key, "")
		if value == "" {
			return fallback
		}
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s must be a boolean: %w", key, err))
		}
		return parsed
	}
	cfg.RobotsNoIndex = boolean("ROBOTS_NOINDEX", false)
	cfg.LogHTTP = boolean("LOG_HTTP", true)
	for _, old := range strings.Split(env("OLD_URLS", ""), ",") {
		if old = strings.TrimRight(strings.TrimSpace(old), "/"); old != "" {
			cfg.OldURLs = append(cfg.OldURLs, old)
		}
	}

	if cfg.AppEnv != EnvironmentDevelopment && cfg.AppEnv != EnvironmentProduction {
		errs = append(errs, fmt.Errorf("APP_ENV must be %q or %q", EnvironmentDevelopment, EnvironmentProduction))
	}
	if err := absoluteURL("APP_URL", cfg.AppURL); err != nil {
		errs = append(errs, err)
	}
	for _, old := range cfg.OldURLs {
		if err := absoluteURL("OLD_URLS", old); err != nil {
			errs = append(errs, err)
		}
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

func absoluteURL(key, value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an absolute HTTP or HTTPS URL", key)
	}
	return nil
}

func (c *Config) IsDevelopment() bool {
	return c.AppEnv == EnvironmentDevelopment
}

func (c *Config) IsProduction() bool {
	return c.AppEnv == EnvironmentProduction
}

func (c *Config) Render() RenderConfig {
	return RenderConfig{
		AppEnv:            c.AppEnv,
		AppURL:            c.AppURL,
		SupportEmail:      c.SupportEmail,
		RobotsNoIndex:     c.RobotsNoIndex,
		UmamiWebsiteID:    c.UmamiWebsiteID,
		UmamiHost:         c.UmamiHost,
		PlausibleDomain:   c.PlausibleDomain,
		PlausibleHost:     c.PlausibleHost,
		GoogleAnalyticsID: c.GoogleAnalyticsID,
	}
}

func (c RenderConfig) IsProduction() bool {
	return c.AppEnv == EnvironmentProduction
}
