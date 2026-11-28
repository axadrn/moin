package handler

import (
	"io/fs"
	"net/http"

	"github.com/axadrn/moin/cloud/internal/service"
	"github.com/axadrn/moin/cloud/internal/ui/pages"
)

// Legal shows the terms or the privacy policy at /legal/{page}.
func Legal(files fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, err := service.ReadLegalPage(files, r.PathValue("page"))
		if err != nil {
			NotFound(w, r)
			return
		}
		render(w, r, pages.Legal(page))
	}
}
