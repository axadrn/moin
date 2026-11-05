// Package brand holds the words the site shows about the product.
package brand

import "github.com/axadrn/moin/internal/build"

const (
	Name    = build.Name
	Tagline = "Deploy anything. Lose nothing."
	// Promise is the line under the tagline, the same on the home page,
	// the sign-in page, the docs, the README and the CLI.
	Promise = "Your apps on your servers. Every change backed up. Every restore tested."
	// Company runs the hosted service and holds the copyright.
	Company = "Berry Good Ideas LLC"
	// SupportEmail is where people write about moin, on every install;
	// SUPPORT_EMAIL overrides it.
	SupportEmail = "hello@moin.run"
	// Description is Promise with a few words for search engines, at most
	// 160 characters: the description of the home page.
	Description = Promise + " Open source, for SQLite and Postgres, with web, CLI, terminal UI and MCP."
)
