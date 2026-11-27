package catalog

import (
	"testing"

	catalogfiles "github.com/axadrn/moin/catalog"
)

// Every app of the catalog loads: its app.md has what the site shows, and
// its compose.yml runs the image at the version.
func TestCatalogLoads(t *testing.T) {
	entries, err := Load(catalogfiles.Files)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no apps")
	}
}
