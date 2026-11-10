package handler

import (
	"net/http"

	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// Next is the page about what grows next to deploys.
func Next(w http.ResponseWriter, r *http.Request) {
	render(w, r, pages.Next())
}
