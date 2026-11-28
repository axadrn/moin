package handler

import (
	"net/http"

	"github.com/axadrn/moin/cloud/internal/ui/pages"
)

// Brand is /brand: the mark, the colors, the type, the kit and the voice.
func Brand(w http.ResponseWriter, r *http.Request) {
	render(w, r, pages.Brand())
}
