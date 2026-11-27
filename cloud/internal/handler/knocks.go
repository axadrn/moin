package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/axadrn/moin/cloud/internal/counts"
	"github.com/axadrn/moin/cloud/internal/githubstars"
	"github.com/axadrn/moin/cloud/internal/ui/pages"
	"github.com/axadrn/moin/internal/report"
)

// sampleKnocks stand in for the knocks of the site's own server in
// development, so the section can be seen before a real count exists.
// The page marks them as sample data; production never shows them.
var sampleKnocks = report.Knocks{
	Today: 1843,
	Year:  212_904,
	Total: 389_117,
	TodayByName: map[string]int64{
		"ssh": 1120, "other": 211, "postgres": 142, "redis": 97, "docker": 71,
		"rdp": 61, "mysql": 52, "telnet": 48, "http-alt": 41,
	},
}

// SiteKnocks are the knocks of the site's own server, the one
// KNOCKS_SERVER names, as its last report to moin cloud said. Without a
// count, in development, sample numbers stand in. nil: nothing to show.
func (c *Cloud) SiteKnocks(workspaceID, server string, development bool) func() *pages.HomeKnocks {
	return func() *pages.HomeKnocks {
		if knocks := c.knocksOf(workspaceID, server); knocks != nil {
			return &pages.HomeKnocks{Knocks: *knocks}
		}
		if development {
			return &pages.HomeKnocks{Knocks: sampleKnocks, Sample: true}
		}
		return nil
	}
}

// knocksOf are the knocks of one server of a workspace, when it reports
// and counted some today. A silent server's numbers are old: none.
func (c *Cloud) knocksOf(workspaceID, server string) *report.Knocks {
	if workspaceID == "" {
		return nil
	}
	view, err := c.watchers.View(workspaceID)
	if err != nil {
		slog.Warn("home: knocks", "error", err)
		return nil
	}
	for _, s := range view.Servers {
		if s.Name == server && s.Problem == "" && s.Knocks != nil && s.Knocks.Today > 0 {
			return s.Knocks
		}
	}
	return nil
}

// sampleTotals stand in for the counts of moin cloud in development. The
// page marks them as sample data.
var sampleTotals = counts.Totals{
	Deploys:  counts.Period{Month: 412, Year: 3180, Total: 3180},
	Knocks:   counts.Period{Month: 1_204_311, Year: 9_877_120, Total: 9_877_120},
	Installs: counts.Period{Month: 238, Year: 1912, Total: 1912},
	Servers:  57,
	Apps:     143,
}

// SiteNumbers are the numbers of the landing page: the stars on GitHub,
// and what moin cloud counted. Development shows sample numbers, so every
// number can be seen before it is counted.
func (c *Cloud) SiteNumbers(development bool) func() pages.HomeNumbers {
	return func() pages.HomeNumbers {
		if development {
			return pages.HomeNumbers{Stars: githubstars.Stars(), Totals: sampleTotals, Sample: true}
		}
		totals, err := c.watchers.Counts().Totals(time.Now())
		if err != nil {
			slog.Warn("home: counts", "error", err)
			totals = counts.Totals{}
		}
		return pages.HomeNumbers{Stars: githubstars.Stars(), Totals: totals}
	}
}

// CountInstall counts each request for the install script as one
// install: one number a day, no address, no browser.
func (c *Cloud) CountInstall(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := c.watchers.Counts().Add(time.Now(), counts.Installs, 1); err != nil {
			slog.Warn("count an install", "error", err)
		}
		next(w, r)
	}
}
