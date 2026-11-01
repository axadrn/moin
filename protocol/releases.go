package protocol

import (
	"cmp"
	"slices"
)

// KeepReleases is how many releases that brought something new, a source
// build or an image, an agent keeps per app. Releases without a build, like
// restarts, domain changes and rollbacks, are small: the agent keeps them as
// long as they are newer than the oldest build it keeps. It always keeps the
// current release. The control plane offers rollbacks by the same rule.
const KeepReleases = 5

// Release is one release of an app, for KeptReleases. Build is true when it
// brought something new.
type Release struct {
	Number int
	Build  bool
}

// KeptReleases returns the numbers of the releases an agent keeps of
// releases, which may come in any order, with current the running one.
func KeptReleases(releases []Release, current int) map[int]bool {
	sorted := slices.Clone(releases)
	slices.SortFunc(sorted, func(a, b Release) int { return cmp.Compare(b.Number, a.Number) })
	kept := map[int]bool{}
	if current > 0 {
		kept[current] = true
	}
	builds := 0
	for _, release := range sorted {
		if builds == KeepReleases {
			break
		}
		kept[release.Number] = true
		if release.Build {
			builds++
		}
	}
	return kept
}
