package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/axadrn/ops/controlplane/internal/api"
	"github.com/axadrn/ops/controlplane/internal/ui/components/toast"
)

// The answers of DESIGN.md, Feedback: each handler picks one of them, so the
// same thing looks the same on every page and nothing on the page moves.

// toastDone says that an action worked. title is the thing and the past verb
// ("blog added"), description what follows, if anything.
func toastDone(w http.ResponseWriter, r *http.Request, title, description string) {
	renderOOB(w, r, toast.Toast(toast.Props{Title: title, Description: description, Type: toast.TypeSuccess}), "beforeend:#toast-container")
}

// toastWarning says that an action worked only in part, and what is left.
func toastWarning(w http.ResponseWriter, r *http.Request, title, description string) {
	renderOOB(w, r, toast.Toast(toast.Props{Title: title, Description: description, Type: toast.TypeWarning, Timeout: 8000}), "beforeend:#toast-container")
}

// toastFailed says why an action failed when no field is to blame. title is
// what did not happen ("Rollback not started"), description why. The page
// stays as it was.
func toastFailed(w http.ResponseWriter, r *http.Request, title, description string) {
	w.Header().Set("HX-Reswap", "none")
	toastError(w, r, title, description)
}

// toastError is the toast of toastFailed for an answer that also swaps in
// the part as it is now, like a switch that flips back.
func toastError(w http.ResponseWriter, r *http.Request, title, description string) {
	renderOOB(w, r, toast.Toast(toast.Props{Title: title, Description: description, Type: toast.TypeError}), "beforeend:#toast-container")
}

// fieldError marks the field with the ID field as wrong: it turns red and
// takes the focus, and message shows in its hint line, or as a toast with
// title when the field has none (a one-row form). The page stays as it was,
// so what was typed stays too.
func fieldError(w http.ResponseWriter, title, field, message string) {
	invalid(w, map[string]string{"field": field, "title": title, "message": message})
}

// fieldRefused marks the field as wrong and says why in a toast, also when
// the field has a hint line: for long reasons, like a key a provider refused.
func fieldRefused(w http.ResponseWriter, r *http.Request, title, field, message string) {
	invalid(w, map[string]string{"field": field})
	toastFailed(w, r, title, message)
}

// failed answers an action that failed with err: the field err is about, when
// fields maps its API field name to the ID of a field on the page, else a
// toast with the message the API gives for err.
func failed(w http.ResponseWriter, r *http.Request, title string, err error, fields map[string]string) {
	message, field := errorFields(err)
	if id, ok := fields[field]; ok && field != "" {
		fieldError(w, title, id, message)
		return
	}
	toastFailed(w, r, title, message)
}

// invalid sends the ops:invalid event the base layout handles (feedbackScript).
func invalid(w http.ResponseWriter, detail map[string]string) {
	event, err := json.Marshal(map[string]map[string]string{"ops:invalid": detail})
	if err != nil {
		slog.Error("encode field error failed", "error", err)
		return
	}
	w.Header().Set("HX-Trigger", string(event))
	w.Header().Set("HX-Reswap", "none")
}

// errorMessage is the message the API would answer for err.
func errorMessage(err error) string {
	message, _ := errorFields(err)
	return message
}

// errorFields is the message the API would answer for err, and the field it
// is about, if any.
func errorFields(err error) (message, field string) {
	_, detail, known := api.ErrorFor(err)
	if !known {
		slog.Error("request failed", "error", err)
	}
	for name, text := range detail.Fields {
		return text, name
	}
	return detail.Message, ""
}
