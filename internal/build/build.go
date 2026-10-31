// Package build holds the product name and the version of this binary.
package build

// Name is the working name of the product and the binary. It changes once
// before the public beta, here and nowhere else.
const Name = "ops"

// Version is set at release time with -ldflags "-X github.com/axadrn/ops/internal/build.Version=v1.2.3".
var Version = "dev"
