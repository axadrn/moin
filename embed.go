// Package moin holds what the repo root gives the programs in it.
package moin

import "embed"

// Kit is what a page needs of the kit folder. The hub serves it under
// /assets/kit/, and its docs, examples and showroom stay out of the binary.
//
//go:embed kit/*.css kit/kit.js kit/parts kit/icons
var Kit embed.FS
