// Package ctxkeys holds what a request carries for the templates.
package ctxkeys

import (
	"context"

	"github.com/axadrn/moin/hub/internal/config"
)

type contextKey string

const (
	URLPathKey      contextKey = "url_path"
	RenderConfigKey contextKey = "render_config"
)

func URLPath(ctx context.Context) string {
	path, _ := ctx.Value(URLPathKey).(string)
	return path
}

func WithURLPath(ctx context.Context, path string) context.Context {
	return context.WithValue(ctx, URLPathKey, path)
}

func RenderConfig(ctx context.Context) config.RenderConfig {
	cfg, _ := ctx.Value(RenderConfigKey).(config.RenderConfig)
	return cfg
}

func WithRenderConfig(ctx context.Context, cfg config.RenderConfig) context.Context {
	return context.WithValue(ctx, RenderConfigKey, cfg)
}
