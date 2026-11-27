// Package brand holds the words the site shows about the product.
package brand

import "github.com/axadrn/moin/internal/build"

const (
	Name    = build.Name
	Tagline = "Deploy anything. Lose nothing."
	// Company runs moin cloud and holds the copyright.
	Company = "Berry Good Ideas LLC"
	// Site is the one address of the site: it serves the install script
	// and moin cloud. The landing page shows it, in development too.
	Site = "https://moin.run"
	// SupportEmail is where people write about moin.
	SupportEmail = "hello@moin.run"
	// Description is what search engines show for the home page, at most
	// 160 characters.
	Description = Tagline + " Open source. SQLite and Postgres. Over SSH, no dashboard."
)
