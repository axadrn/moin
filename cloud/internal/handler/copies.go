package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/axadrn/moin/cloud/internal/copies"
)

// The copies a hacked server can't delete (package copies): a server
// starts one and says when it is done, with its token; the CLI lists them
// and gets an address to download one, with its session. Each answer is
// a status code, and for a server one address to upload to, nothing else.

// StartCopy gives a server an address to upload one copy to.
func (c *Cloud) StartCopy(w http.ResponseWriter, r *http.Request) {
	if !c.takesCopies(w) {
		return
	}
	token, _ := bearer(r)
	var request copies.Request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&request); err != nil {
		http.Error(w, "not a copy", http.StatusBadRequest)
		return
	}
	upload, err := c.Copies.Start(r.Context(), token, request)
	if err != nil {
		copyError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, upload)
}

// CopyDone notes that a server uploaded its copy.
func (c *Cloud) CopyDone(w http.ResponseWriter, r *http.Request) {
	if !c.takesCopies(w) {
		return
	}
	token, _ := bearer(r)
	if err := c.Copies.Done(r.Context(), token, r.PathValue("id")); err != nil {
		copyError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CLICopies answers `moin cloud copies <server> [app]`, and the list
// `moin cloud restore` picks from.
func (c *Cloud) CLICopies(w http.ResponseWriter, r *http.Request) {
	space, ok := c.cliSession(w, r)
	if !ok || !c.takesCopies(w) {
		return
	}
	list, err := c.Copies.List(space.ID, r.URL.Query().Get("server"), r.URL.Query().Get("app"))
	if err != nil {
		renderError(w, "list the copies", err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// CLIDownload gives the CLI an address to download one copy from, for
// five minutes. The CLI hands it to a server, which restores from it.
func (c *Cloud) CLIDownload(w http.ResponseWriter, r *http.Request) {
	space, ok := c.cliSession(w, r)
	if !ok || !c.takesCopies(w) {
		return
	}
	address, err := c.Copies.DownloadURL(r.Context(), space.ID, r.PathValue("id"))
	if err != nil {
		copyError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": address})
}

// takesCopies says whether this moin cloud has a bucket for copies, and
// says no when not.
func (c *Cloud) takesCopies(w http.ResponseWriter) bool {
	if c.Copies == nil {
		http.Error(w, "this moin cloud takes no copies", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// copyError answers what went wrong with a copy, as a status code.
func copyError(w http.ResponseWriter, err error) {
	var status int
	switch {
	case errors.Is(err, copies.ErrUnknownToken):
		status = http.StatusUnauthorized
	case errors.Is(err, copies.ErrNotPaid):
		status = http.StatusPaymentRequired
	case errors.Is(err, copies.ErrFull):
		status = http.StatusInsufficientStorage
	case errors.Is(err, copies.ErrTooOften):
		status = http.StatusTooManyRequests
	case errors.Is(err, copies.ErrNotUploaded):
		status = http.StatusConflict
	case errors.Is(err, copies.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, copies.ErrNotACopy):
		status = http.StatusBadRequest
	default:
		renderError(w, "a copy", err)
		return
	}
	http.Error(w, err.Error(), status)
}
