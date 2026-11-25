package api

import (
	"net/http"
	"strings"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/model"
)

// NoToken is the right of a route that connections and CI tokens may not
// call: account security, deletions of the account and workspace, roles,
// billing, admin, and the steps of signing in. A signed-in browser, or for
// signing in nobody, calls it.
const NoToken model.Rights = 0

// gate is the route's handler behind its right. Session and guest requests
// pass: only connections and CI tokens have rights.
func (route Route) gate(appID func(workspaceID, app string) (string, error)) http.HandlerFunc {
	next := route.Handler
	oneApp := forOneApp(route.Pattern)
	return func(w http.ResponseWriter, r *http.Request) {
		bearer := ctxkeys.Bearer(r.Context())
		if bearer == nil {
			next(w, r)
			return
		}
		if route.Right == NoToken {
			Error(w, http.StatusForbidden, "forbidden", "Connections and CI tokens cannot use this endpoint. Sign in to the web to continue.")
			return
		}
		if !Allowed(w, r, route.Right) {
			return
		}
		if bearer.AppID != "" {
			if !oneApp {
				Error(w, http.StatusForbidden, "app_only", "This CI token works only for its app.")
				return
			}
			if app := r.PathValue("app"); app != "" {
				id, err := appID(bearer.WorkspaceID, app)
				if err != nil || id != bearer.AppID {
					Error(w, http.StatusNotFound, "not_found", "The app was not found.")
					return
				}
			}
		}
		next(w, r)
	}
}

// forOneApp reports whether a CI token for one app may call a route: the
// routes of an app, and the account it acts for.
func forOneApp(pattern string) bool {
	return strings.Contains(pattern, "/apps/{app}") || pattern == "GET /api/v1/account" || pattern == "GET /api/v1/meta"
}

// Allowed answers 403 missing_right and reports false when the connection
// or CI token of the request lacks a right of need. Sessions are allowed.
// Routes name their right in Routes; a handler asks only when the body
// decides, like SQL that writes.
func Allowed(w http.ResponseWriter, r *http.Request, need model.Rights) bool {
	bearer := ctxkeys.Bearer(r.Context())
	if bearer == nil {
		return true
	}
	missing := bearer.Rights.Missing(need)
	if missing == 0 {
		return true
	}
	detail := MissingRight(*bearer, missing)
	JSON(w, http.StatusForbidden, ErrorBody{Error: detail})
	return false
}

// MissingRight is the error of a connection or CI token without a right:
// "This connection may not deploy and restart. Change it in Settings,
// Connections."
func MissingRight(bearer model.Bearer, missing model.Rights) ErrorDetail {
	return ErrorDetail{
		Code:    "missing_right",
		Message: "This " + bearer.Kind() + " may not " + missing.Verb() + ". Change it in Settings, Connections.",
		Right:   missing.Name(),
	}
}
