package protocol

import (
	"maps"
	"slices"
	"testing"
)

func TestKeptReleases(t *testing.T) {
	numbers := func(kept map[int]bool) []int { return slices.Sorted(maps.Keys(kept)) }

	// v1 to v5 are builds, v6 to v12 restarts: every one stays.
	var releases []Release
	for number := 1; number <= 12; number++ {
		releases = append(releases, Release{Number: number, Build: number <= 5})
	}
	if got := numbers(KeptReleases(releases, 12)); len(got) != 12 {
		t.Fatalf("five builds and restarts: kept %v, want all", got)
	}

	// A sixth build pushes out v1 and the restarts older than v2 only.
	releases = append(releases, Release{Number: 13, Build: true})
	got := numbers(KeptReleases(releases, 13))
	if want := []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}; !slices.Equal(got, want) {
		t.Fatalf("a sixth build: kept %v, want %v", got, want)
	}

	// Restarts between old builds go with them; the current one stays.
	releases = []Release{{1, true}, {2, false}, {3, true}, {4, true}, {5, true}, {6, true}, {7, true}, {8, false}}
	got = numbers(KeptReleases(releases, 2))
	if want := []int{2, 3, 4, 5, 6, 7, 8}; !slices.Equal(got, want) {
		t.Fatalf("current v2: kept %v, want %v", got, want)
	}
	got = numbers(KeptReleases(releases, 8))
	if want := []int{3, 4, 5, 6, 7, 8}; !slices.Equal(got, want) {
		t.Fatalf("current v8: kept %v, want %v", got, want)
	}
}
