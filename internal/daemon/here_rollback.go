package daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// RollbackHere runs an older release of the app again: release number, or
// with 0 the newest one that ran other images than the live one. The env
// and the domains stay as they are now.
func RollbackHere(ctx context.Context, dir, app string, number int, out io.Writer) (Result, error) {
	a := newApps(dir)
	if err := a.mustHave(app); err != nil {
		return Result{}, err
	}
	current := a.current(app)
	if number == 0 {
		number = a.previousRelease(app, current)
		if number == 0 {
			return Result{}, fmt.Errorf("%s has no older release to go back to", app)
		}
	}
	if number == current {
		return Result{}, fmt.Errorf("v%d is the live release already", number)
	}
	if !a.canRunAgain(app, number) {
		return Result{}, fmt.Errorf("v%d is not on the server. These are: %s", number, a.releasesText(app))
	}
	start := a.newStart(app)
	start.FromNumber = number
	start.Source = "rollback"
	return a.deployHere(ctx, start, out)
}

// Release is a release of an app that can run again, for the picker of
// `moin rollback`: the record of its deploy, with the code it runs.
type Release struct {
	DeployRecord
	// Before is the one a rollback without --to goes back to.
	Before bool `json:"before,omitempty"`
}

// ReleasesHere lists the releases of app that can run again, newest
// first.
func ReleasesHere(dir, app string) ([]Release, error) {
	a := newApps(dir)
	if err := a.mustHave(app); err != nil {
		return nil, err
	}
	current := a.current(app)
	before := a.previousRelease(app, current)
	numbers := a.releases(app)
	slices.Reverse(numbers)
	list := []Release{}
	for _, number := range numbers {
		if !a.canRunAgain(app, number) {
			continue
		}
		release := Release{DeployRecord: a.record(app, number), Before: number == before}
		release.Live = number == current
		list = append(list, release)
	}
	return list, nil
}

// previousRelease is the newest release below current whose images differ
// from the live ones: a restart or a domain change makes a release with
// the same images, and going back to it would change nothing.
func (a *apps) previousRelease(app string, current int) int {
	live := a.images(app, current)
	releases := a.releases(app)
	slices.Reverse(releases)
	for _, number := range releases {
		if number < current && a.canRunAgain(app, number) && a.images(app, number) != live {
			return number
		}
	}
	return 0
}

// canRunAgain reports whether release number of app can start again: it
// has its compose file and did not fail.
func (a *apps) canRunAgain(app string, number int) bool {
	dir := a.releaseDir(app, number)
	kind, _ := os.ReadFile(filepath.Join(dir, "kind"))
	return exists(filepath.Join(dir, "compose.json")) && string(kind) != releaseFailed
}

// images are the images a release runs, as one sorted line.
func (a *apps) images(app string, number int) string {
	body, _ := os.ReadFile(filepath.Join(a.releaseDir(app, number), "compose.json"))
	var images []string
	for _, match := range imagePattern.FindAllStringSubmatch(string(body), -1) {
		images = append(images, match[1])
	}
	slices.Sort(images)
	return strings.Join(images, " ")
}

// releasesText names the releases that can start again, like "v3, v4".
func (a *apps) releasesText(app string) string {
	var names []string
	for _, number := range a.releases(app) {
		if a.canRunAgain(app, number) {
			names = append(names, fmt.Sprintf("v%d", number))
		}
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}
