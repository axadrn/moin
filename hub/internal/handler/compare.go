package handler

import (
	"net/http"

	"github.com/axadrn/moin/hub/internal/ui/pages"
)

// Compare shows one tool next to moin at /compare/{name}.
func Compare(w http.ResponseWriter, r *http.Request) {
	competitor, ok := pages.FindCompetitor(r.PathValue("name"))
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		render(w, r, pages.NotFound())
		return
	}
	render(w, r, pages.Compare(competitor))
}
