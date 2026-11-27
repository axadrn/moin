// Package middleware holds what runs around every request of the site.
package middleware

import "net/http"

// Chain wraps h in the middlewares. The first one runs first.
func Chain(h http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}
