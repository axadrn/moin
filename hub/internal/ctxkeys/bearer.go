package ctxkeys

import (
	"context"

	"github.com/axadrn/moin/hub/internal/model"
)

const BearerKey contextKey = "bearer"

// WithBearer marks a request as authenticated by a connection or a CI
// token. Such a request has a user but no session.
func WithBearer(ctx context.Context, bearer *model.Bearer) context.Context {
	return context.WithValue(ctx, BearerKey, bearer)
}

// Bearer returns the connection or CI token that authenticated the request,
// or nil for session and guest requests.
func Bearer(ctx context.Context) *model.Bearer {
	bearer, _ := ctx.Value(BearerKey).(*model.Bearer)
	return bearer
}
