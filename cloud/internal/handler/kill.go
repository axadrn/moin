package handler

import (
	"io/fs"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/axadrn/moin/cloud/internal/brand"
	"github.com/axadrn/moin/cloud/internal/catalog"
	"github.com/axadrn/moin/cloud/internal/service"
	"github.com/axadrn/moin/cloud/internal/ui/pages"
)

// Kill is /kill-your-subscriptions: the prices of content/prices.json,
// read on every request, each with the catalog app that replaces it.
func Kill(files fs.FS, entries []catalog.Entry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		prices, err := service.Prices(files)
		if err != nil {
			renderError(w, "prices", err)
			return
		}
		render(w, r, pages.Kill(KillSwaps(prices, entries), len(entries)))
	}
}

// notID is what an ID of the page leaves out of a tool's name.
var notID = regexp.MustCompile(`[^a-z0-9]+`)

// KillSwaps puts each price next to what replaces it. A price whose app
// is not in the catalog is left out: the page shows only commands that
// work.
func KillSwaps(prices []service.Price, entries []catalog.Entry) []pages.KillSwap {
	swaps := make([]pages.KillSwap, 0, len(prices))
	for _, price := range prices {
		swap := pages.KillSwap{Price: price, ID: strings.Trim(notID.ReplaceAllString(strings.ToLower(price.Tool), "-"), "-")}
		if price.App == "" {
			swap.Name = brand.Name + " cloud"
			swap.Command = brand.Name + " watcher connect my-server"
		} else {
			at := slices.IndexFunc(entries, func(e catalog.Entry) bool { return e.Slug == price.App })
			if at < 0 {
				slog.Error("prices: no such catalog app", "tool", price.Tool, "app", price.App)
				continue
			}
			swap.Name = entries[at].Name
			swap.Href = "/catalog/" + price.App
			swap.Command = brand.Name + " deploy my-server --catalog " + price.App
		}
		swaps = append(swaps, swap)
	}
	return swaps
}
