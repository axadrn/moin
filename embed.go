// Package moin holds what the repo root gives the programs in it.
package moin

import "embed"

// Kit is the kit folder. The hub serves what pages need under /assets/kit/
// and the whole folder, with its showroom, under /kit/.
//
//go:embed kit
var Kit embed.FS
