// Package brand holds the words the site shows about the product.
package brand

import "github.com/axadrn/moin/internal/build"

const (
	Name    = build.Name
	Tagline = "Deploy anything. Lose nothing."
	// Company runs the hosted service and holds the copyright.
	Company = "Berry Good Ideas LLC"
	// SupportEmail is where people write about moin, on every install;
	// SUPPORT_EMAIL overrides it.
	SupportEmail = "hello@moin.run"
	Description  = "Any app on any server, every SQLite and Postgres change backed up and checked every day. Open source."
)
