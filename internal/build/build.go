// Package build holds the product name and the version of this binary.
package build

// Name is the name of the product and the binary, here and nowhere else.
const Name = "moin"

// Version is set at release time with -ldflags "-X github.com/axadrn/moin/internal/build.Version=v1.2.3".
var Version = "dev"
