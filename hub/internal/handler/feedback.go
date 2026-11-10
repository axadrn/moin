package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/axadrn/moin/hub/internal/api"
	"github.com/axadrn/moin/hub/internal/ui/components"
)

// The answers of DESIGN.md, Feedback: each handler picks one of them, so the
// same thing looks the same on every page and nothing on the page moves.

// answerDone says that an action worked: the button that did it shows a
// check for a moment. title is the thing and the past verb ("blog added"),
// description what follows, if anything.
func answerDone(w http.ResponseWriter, r *http.Request, title, description string) {
	renderOOB(w, r, components.Feedback(components.FeedbackProps{Kind: "done", Title: title, Note: description}), "beforeend:#feedback")
}

// answerNote says that an action worked only in part, and what is left.
func answerNote(w http.ResponseWriter, r *http.Request, title, description string) {
	renderOOB(w, r, components.Feedback(components.FeedbackProps{Kind: "note", Title: title, Note: description}), "beforeend:#feedback")
}

// answerFailed says why an action failed when no field is to blame, in a line
// under the button that sent it. title is what did not happen ("Rollback not
// started"), description why. The page stays as it was.
func answerFailed(w http.ResponseWriter, r *http.Request, title, description string) {
	w.Header().Set("HX-Reswap", "none")
	answerError(w, r, title, description)
}

// answerError is the line of answerFailed for an answer that also swaps in
// the part as it is now, like a switch that flips back.
func answerError(w http.ResponseWriter, r *http.Request, title, description string) {
	renderOOB(w, r, components.Feedback(components.FeedbackProps{Kind: "failed", Title: title, Note: description}), "beforeend:#feedback")
}

// fieldError marks the field with the ID field as wrong: it turns red and
// takes the focus, and message shows in its hint line, or in a line under
// its form when the field has none (a one-row form). The page stays as it was,
// so what was typed stays too.
func fieldError(w http.ResponseWriter, title, field, message string) {
	invalid(w, map[string]string{"field": field, "title": title, "message": message})
}

// fieldRefused marks the field as wrong and says why under its form, also when
// the field has a hint line: for long reasons, like a key a provider refused.
func fieldRefused(w http.ResponseWriter, r *http.Request, title, field, message string) {
	invalid(w, map[string]string{"field": field})
	answerFailed(w, r, title, message)
}

// failed answers an action that failed with err: the field err is about, when
// fields maps its API field name to the ID of a field on the page, else a
// line with the message the API gives for err.
func failed(w http.ResponseWriter, r *http.Request, title string, err error, fields map[string]string) {
	message, field := errorFields(err)
	if id, ok := fields[field]; ok && field != "" {
		fieldError(w, title, id, message)
		return
	}
	answerFailed(w, r, title, message)
}

// invalid sends the moin:invalid event hub.js handles.
func invalid(w http.ResponseWriter, detail map[string]string) {
	event, err := json.Marshal(map[string]map[string]string{"moin:invalid": detail})
	if err != nil {
		slog.Error("encode field error failed", "error", err)
		return
	}
	w.Header().Set("HX-Trigger", asciiJSON(event))
	w.Header().Set("HX-Reswap", "none")
}

// startDownload sends the moin:download event hub.js handles.
func startDownload(w http.ResponseWriter, link string) {
	event, err := json.Marshal(map[string]map[string]string{"moin:download": {"link": link}})
	if err != nil {
		slog.Error("encode download failed", "error", err)
		return
	}
	w.Header().Set("HX-Trigger", asciiJSON(event))
}

// asciiJSON writes every rune beyond ASCII as \uXXXX: a header carries
// only ASCII safely, so "…" would arrive as "â€¦". JSON.parse in the
// browser reads the escapes back.
func asciiJSON(data []byte) string {
	var b strings.Builder
	for _, r := range string(data) {
		switch {
		case r < utf8.RuneSelf:
			b.WriteRune(r)
		case r > 0xFFFF:
			high, low := utf16.EncodeRune(r)
			fmt.Fprintf(&b, "\\u%04x\\u%04x", high, low)
		default:
			fmt.Fprintf(&b, "\\u%04x", r)
		}
	}
	return b.String()
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
