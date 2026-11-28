// Package ctxkeys holds what a request carries for the templates.
package ctxkeys

import (
	"context"

	"github.com/axadrn/moin/cloud/internal/config"
)

type contextKey string

const (
	urlPathKey      contextKey = "url_path"
	renderConfigKey contextKey = "render_config"
	signedInKey     contextKey = "signed_in"
)

func URLPath(ctx context.Context) string {
	path, _ := ctx.Value(urlPathKey).(string)
	return path
}

func WithURLPath(ctx context.Context, path string) context.Context {
	return context.WithValue(ctx, urlPathKey, path)
}

func RenderConfig(ctx context.Context) config.RenderConfig {
	cfg, _ := ctx.Value(renderConfigKey).(config.RenderConfig)
	return cfg
}

func WithRenderConfig(ctx context.Context, cfg config.RenderConfig) context.Context {
	return context.WithValue(ctx, renderConfigKey, cfg)
}

// SignedIn says whether the request carries a valid moin cloud session.
func SignedIn(ctx context.Context) bool {
	in, _ := ctx.Value(signedInKey).(bool)
	return in
}

func WithSignedIn(ctx context.Context, in bool) context.Context {
	return context.WithValue(ctx, signedInKey, in)
}
