package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/a-h/templ"
)

func isAction(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// confirmation is the name typed into the confirm dialog, for the service
// to check before what is big and cannot be undone. kit.ConfirmDialog sends
// it as X-Moin-Confirm, URI-encoded, since a header carries no UTF-8.
func confirmation(r *http.Request) string {
	typed, err := url.PathUnescape(r.Header.Get("X-Moin-Confirm"))
	if err != nil {
		return ""
	}
	return typed
}

func render(w http.ResponseWriter, r *http.Request, component templ.Component) {
	if err := component.Render(r.Context(), w); err != nil {
		renderError(w, "render failed", err)
	}
}

func renderFragment(w http.ResponseWriter, r *http.Request, component templ.Component, fragmentIDs ...any) {
	if err := templ.RenderFragments(r.Context(), w, component, fragmentIDs...); err != nil {
		renderError(w, "render fragment failed", err)
	}
}

func renderOOB(w http.ResponseWriter, r *http.Request, component templ.Component, target string) {
	if _, err := fmt.Fprintf(w, `<div hx-swap-oob="%s">`, target); err != nil {
		slog.Error("render oob wrapper failed", "error", err)
		return
	}
	if err := component.Render(r.Context(), w); err != nil {
		renderError(w, "render oob failed", err)
		return
	}
	if _, err := w.Write([]byte(`</div>`)); err != nil {
		slog.Error("render oob wrapper failed", "error", err)
	}
}

func redirect(w http.ResponseWriter, r *http.Request, location string, status int) {
	if isAction(r) {
		w.Header().Set("HX-Redirect", location)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, location, status)
}

func renderError(w http.ResponseWriter, message string, err error) {
	slog.Error(message, "error", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

// renderAuthPage answers the sign-in form with another auth page: its <main>
// takes the place of the sign-in page's, instead of the whole page landing
// inside the form.
func renderAuthPage(w http.ResponseWriter, r *http.Request, component templ.Component) {
	if isAction(r) {
		w.Header().Set("HX-Retarget", "main")
		w.Header().Set("HX-Reselect", "main")
		w.Header().Set("HX-Reswap", "outerHTML")
	}
	render(w, r, component)
}
