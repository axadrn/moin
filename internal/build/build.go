// Package build holds the product name and the version of this binary.
package build

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Name is the name of the product and the binary, here and nowhere else.
const Name = "moin"

// Version is set at release time with -ldflags "-X github.com/axadrn/moin/internal/build.Version=v1.2.3".
var Version = "dev"

// LegacyName is the name the product had before. Servers, configs and
// scripts that still use it keep working until LegacyUntil; agents move to
// Name by themselves. Remove it, and everything that reads it, after then.
const LegacyName = "ops"

// LegacyUntil is when the old name stops working.
var LegacyUntil = time.Date(2026, 12, 4, 0, 0, 0, 0, time.UTC)

// Legacy reports whether the old name still works.
func Legacy() bool { return time.Now().Before(LegacyUntil) }

// LegacyWhileWorks is LegacyName while the old name works, else "".
func LegacyWhileWorks() string {
	if Legacy() {
		return LegacyName
	}
	return ""
}

// Getenv reads NAME_key, then LEGACYNAME_key while the old name works:
// MOIN_URL, then OPS_URL.
func Getenv(key string) string {
	if value := os.Getenv(strings.ToUpper(Name) + "_" + key); value != "" {
		return value
	}
	if Legacy() {
		return os.Getenv(strings.ToUpper(LegacyName) + "_" + key)
	}
	return ""
}

// Invoked is the name the binary was started by: LegacyName when it runs
// as the old binary or as the update check of an old agent, which expects
// "ops <version>" from the new binary before it swaps it in.
func Invoked(arg0 string) string {
	base := filepath.Base(arg0)
	if base == LegacyName || strings.HasPrefix(base, "."+LegacyName+"-update-") {
		return LegacyName
	}
	return Name
}
