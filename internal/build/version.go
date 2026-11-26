package build

import (
	"cmp"
	"strconv"
	"strings"
)

// IsRelease reports whether version is a release, like v1.2.3 or
// v1.0.0-beta.3, and not a build without one, like "dev" or main-b4be272.
func IsRelease(version string) bool {
	return parseVersion(version) != nil
}

// NewerVersion reports whether candidate is a newer release than current,
// both like v1.2.3 or v1.0.0-beta.3, by the rules of semantic versioning.
// Anything else, such as "dev", is never newer and never older.
func NewerVersion(candidate, current string) bool {
	a, b := parseVersion(candidate), parseVersion(current)
	if a == nil || b == nil {
		return false
	}
	return compareVersions(a, b) > 0
}

type semver struct {
	core       [3]int
	prerelease []string
}

func parseVersion(value string) *semver {
	value, ok := strings.CutPrefix(value, "v")
	if !ok {
		return nil
	}
	value, metadata, hasMetadata := strings.Cut(value, "+")
	if hasMetadata && !identifiers(metadata) {
		return nil
	}
	core, prerelease, hasPrerelease := strings.Cut(value, "-")
	if hasPrerelease && !identifiers(prerelease) {
		return nil
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return nil
	}
	var version semver
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 || part == "" || (len(part) > 1 && part[0] == '0') {
			return nil
		}
		version.core[index] = number
	}
	if hasPrerelease {
		version.prerelease = strings.Split(prerelease, ".")
	}
	return &version
}

// identifiers reports whether value is dot-separated semver identifiers:
// ASCII letters, digits and hyphens, none empty. A daemon puts the version
// in a download URL, so nothing else may pass.
func identifiers(value string) bool {
	for identifier := range strings.SplitSeq(value, ".") {
		if identifier == "" || strings.Trim(identifier, "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz-") != "" {
			return false
		}
	}
	return true
}

func compareVersions(a, b *semver) int {
	for index := range a.core {
		if a.core[index] != b.core[index] {
			return cmp.Compare(a.core[index], b.core[index])
		}
	}
	// A release is newer than its prereleases.
	switch {
	case len(a.prerelease) == 0 && len(b.prerelease) == 0:
		return 0
	case len(a.prerelease) == 0:
		return 1
	case len(b.prerelease) == 0:
		return -1
	}
	for index := 0; index < len(a.prerelease) && index < len(b.prerelease); index++ {
		x, y := a.prerelease[index], b.prerelease[index]
		xNumber, xErr := strconv.Atoi(x)
		yNumber, yErr := strconv.Atoi(y)
		switch {
		case xErr == nil && yErr == nil:
			if xNumber != yNumber {
				return cmp.Compare(xNumber, yNumber)
			}
		case xErr == nil:
			return -1
		case yErr == nil:
			return 1
		case x != y:
			return strings.Compare(x, y)
		}
	}
	return cmp.Compare(len(a.prerelease), len(b.prerelease))
}
