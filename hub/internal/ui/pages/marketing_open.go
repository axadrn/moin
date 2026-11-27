package pages

import (
	"strconv"
	"time"

	"github.com/axadrn/moin/hub/internal/service"
)

// HomeOpen is what the changelog says about how moin ships: how many
// versions since the first, and the newest one. Read from the changelog on
// every visit, so it is always true.
type HomeOpen struct {
	Releases int
	Since    time.Time
	Latest   service.Release
}

// NewHomeOpen counts the versions, newest first as the changelog lists them.
func NewHomeOpen(releases []service.Release) HomeOpen {
	if len(releases) == 0 {
		return HomeOpen{}
	}
	return HomeOpen{Releases: len(releases), Since: releases[len(releases)-1].Date, Latest: releases[0]}
}

func homeVersions(n int) string {
	if n == 1 {
		return "1 version"
	}
	return strconv.Itoa(n) + " versions"
}
