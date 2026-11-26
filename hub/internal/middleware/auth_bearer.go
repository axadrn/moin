package middleware

import (
	"net/http"
	"strings"
)

// hasBearerAuthorization reports whether the request sends an Authorization
// header with the Bearer scheme. Such a request authenticates with that header
// alone: AuthMiddleware never reads its cookies, even when the token is
// invalid, so CSRFProtection may skip it. Any other scheme, such as Basic auth
// added by a proxy, leaves cookie sessions and the CSRF check in place.
func hasBearerAuthorization(r *http.Request) bool {
	scheme, _, _ := strings.Cut(r.Header.Get("Authorization"), " ")
	return strings.EqualFold(scheme, "Bearer")
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" value.
func bearerToken(header string) (string, bool) {
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, " \t") {
		return "", false
	}
	return token, true
}
