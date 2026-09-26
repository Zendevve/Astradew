// This file holds the version comparator the dependency edges rely on:
// whether a Mod Unit's version meets another manifest's declared floor. It
// deliberately reuses detect.VersionIdentity (the frozen Phase 1 rules)
// instead of re-deriving build-metadata stripping or zero padding, so a
// version that compares equal here is exactly a version the version detector
// calls the same release. These are identity rules, not the semver
// specification; display always keeps the author's original string.
package manifest

import (
	"strings"

	"github.com/Zendevve/astradew/internal/detect"
)

// CompareVersions orders two authored version strings under the frozen Phase 1
// identity rules (detect.VersionIdentity), not the semver specification. It
// returns -1 when a sorts before b, 0 when they name the same release, and +1
// when a sorts after b.
//
// Identity equality comes first: build metadata is blind and trailing zero
// components are padding, so "4.5.2+build.8" and "4.5.2.0" both compare equal
// to "4.5.2". Otherwise the numeric core components compare left to right,
// zero-filling the shorter side, so "1.10" outranks "1.9" and "1.0" equals
// "1.0.0". When the cores are equal a release outranks any prerelease
// ("4.5.2" > "4.5.2-alpha"), and two prereleases compare by ordinal
// case-sensitive text, matching the frozen identity where prerelease text is
// significant. These are the frozen Phase 1 identity rules, not semver rules
// (semver would also order "." separators and hyphenated prerelease fields
// differently).
//
// The comparator is reachable with authored strings, so it must tolerate
// garbage: a missing, blank, or non-numeric component reads as 0 and nothing
// panics. Two identities that tie numerically but not textually (leading
// zeros, non-numeric components) order by their normalized text, so
// CompareVersions returns 0 exactly when SameVersion is true.
func CompareVersions(a, b string) int {
	identityA, identityB := detect.VersionIdentity(a), detect.VersionIdentity(b)
	if identityA == identityB {
		return 0
	}
	coreA, prereleaseA := splitPrerelease(identityA)
	coreB, prereleaseB := splitPrerelease(identityB)

	partsA, partsB := strings.Split(coreA, "."), strings.Split(coreB, ".")
	for i := range max(len(partsA), len(partsB)) {
		if c := compareComponents(component(partsA, i), component(partsB, i)); c != 0 {
			return c
		}
	}

	// Cores are numerically equal: the release sorts after any prerelease,
	// and two prereleases compare as ordinal text.
	switch {
	case prereleaseA == prereleaseB:
		// Distinct identities that tied numerically ("04" against "4", or a
		// non-numeric component read as 0): the normalized text decides, so
		// this comparator and SameVersion never disagree about equality.
		return ordinal(identityA, identityB)
	case prereleaseA == "":
		return 1
	case prereleaseB == "":
		return -1
	default:
		return ordinal(prereleaseA, prereleaseB)
	}
}

// SameVersion reports whether two authored strings name the same release under
// the frozen Phase 1 identity rules: build metadata and zero padding are
// ignored, prerelease text is significant, and surrounding space is trimmed.
func SameVersion(a, b string) bool {
	return detect.VersionIdentity(a) == detect.VersionIdentity(b)
}

// SatisfiesMinimum reports whether version meets a declared minimum version
// floor. An absent or blank floor means no floor and is always satisfied (the
// parser drops blank floors the same way). Otherwise the comparison is
// CompareVersions(version, minimum) >= 0, so "4.5.2-alpha" does not satisfy
// floor "4.5.2" (a prerelease never satisfies the release it precedes) and
// "4.5.2" does not satisfy floor "4.5.3".
func SatisfiesMinimum(version, minimum string) bool {
	if strings.TrimSpace(minimum) == "" {
		return true
	}
	return CompareVersions(version, minimum) >= 0
}

// ordinal orders two strings into the comparator's -1/0/+1 convention, by
// plain byte order: prerelease text and the identity tie-break are both
// case-sensitive, as the frozen identity treats them.
func ordinal(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// splitPrerelease splits a normalized identity into its numeric core and its
// prerelease tag ("" when absent). VersionIdentity already removed build
// metadata and trailing zero components, so the first "-" is the prerelease
// separator; a trailing "-" leaves an empty tag, which reads as no prerelease.
func splitPrerelease(identity string) (core, prerelease string) {
	core, prerelease, _ = strings.Cut(identity, "-")
	return core, prerelease
}

// component returns the i-th numeric component of parts, or "" when the
// version has fewer components: zero-filling by absence is what makes "1.0"
// equal "1.0.0".
func component(parts []string, i int) string {
	if i >= len(parts) {
		return ""
	}
	return parts[i]
}

// compareComponents orders two numeric components as non-negative integers. A
// non-numeric or blank component reads as 0, so the public comparator never
// panics on input such as "", "abc", "1..2", or "-x". Leading zeros are
// insignificant ("04" == "4"), and a component too long for an integer still
// orders by its digits — longer digit strings are larger, then lexicographic —
// instead of overflowing into a wrong answer.
func compareComponents(a, b string) int {
	a, b = digits(a), digits(b)
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// digits reduces a component to its canonical decimal digits: leading zeros
// stripped, any non-digit character collapsing the whole component to "0".
func digits(s string) string {
	i := 0
	for i < len(s) && s[i] == '0' {
		i++
	}
	s = s[i:]
	for j := range s {
		if s[j] < '0' || s[j] > '9' {
			return "0"
		}
	}
	if s == "" {
		return "0"
	}
	return s
}
