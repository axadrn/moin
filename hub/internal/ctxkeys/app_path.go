package ctxkeys

import "context"

// AppBasePath is the address of the request's workspace in the web,
// /app/<slug>.
func AppBasePath(ctx context.Context) string {
	return Workspace(ctx).Path()
}
