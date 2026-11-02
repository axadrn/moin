package ctxkeys

import "context"

const ClientKey contextKey = "client"

// Client is who sent a request: the client IP and the user agent.
type Client struct {
	IP        string
	UserAgent string
}

// WithClient stores the client of a request.
func WithClient(ctx context.Context, client Client) context.Context {
	return context.WithValue(ctx, ClientKey, client)
}

// ClientOf returns the client of a request, empty outside one.
func ClientOf(ctx context.Context) Client {
	client, _ := ctx.Value(ClientKey).(Client)
	return client
}
