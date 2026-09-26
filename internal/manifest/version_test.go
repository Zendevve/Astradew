package manifest

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want int
	}{
		// Zero padding and build metadata are identity, not difference.
		{"zero-padded core equal", "4.5.2", "4.5.2.0", 0},
		{"fewer components equal", "1.0", "1.0.0", 0},
		{"metadata blind", "4.5.2", "4.5.2+build.8", 0},
		{"metadata blind reverse", "4.5.2+build.8", "4.5.2", 0},
		{"surrounding space trimmed", " 4.5.2 ", "4.5.2", 0},

		// Numeric ordering, left to right.
		{"ten outranks nine", "1.10", "1.9", 1},
		{"nine behind ten", "1.9", "1.10", -1},
		{"patch bump", "4.5.3", "4.5.2", 1},
		{"two below ten", "2.0", "10.0", -1},

		// Prerelease is identity and sorts before the release it precedes.
		{"prerelease before release", "4.5.2-alpha", "4.5.2", -1},
		{"release after prerelease", "4.5.2", "4.5.2-alpha", 1},
		{"prerelease ordinal text", "4.5.2-alpha", "4.5.2-beta", -1},
		{"prerelease ordinal text reverse", "4.5.2-beta", "4.5.2-alpha", 1},
		{"prerelease ordinal is not numeric", "4.5.2-alpha.10", "4.5.2-alpha.9", -1},

		// Garbage input must be stable, never panicking.
		{"both empty", "", "", 0},
		{"garbage text is not identity-equal", "abc", "", 1},
		{"leading zeros are distinct identities", "04", "4", -1},
		{"leading zeros reverse", "4", "04", 1},
		{"empty component still marks a text difference", "1..2", "1.0.2", -1},
		{"bare prerelease", "-x", "-x", 0},
		{"trailing hyphen is no prerelease", "4.5.2-", "4.5.2", 0},
		{"huge component still orders", "99999999999999999999", "1", 1},
		{"huge components compare by digits", "99999999999999999999", "99999999999999999998", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CompareVersions(c.a, c.b); got != c.want {
				t.Fatalf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestSameVersion(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"zero padding ignored", "4.5.2", "4.5.2.0", true},
		{"metadata ignored", "4.5.2", "4.5.2+build.8", true},
		{"space ignored", "4.5.2", " 4.5.2 ", true},
		{"prerelease significant", "4.5.2-alpha", "4.5.2", false},
		{"different releases", "4.5.2", "4.5.3", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SameVersion(c.a, c.b); got != c.want {
				t.Fatalf("SameVersion(%q, %q) = %t, want %t", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestSatisfiesMinimum(t *testing.T) {
	cases := []struct {
		name           string
		version, floor string
		want           bool
	}{
		{"no floor", "4.5.2", "", true},
		{"blank floor", "4.5.2", "  ", true},
		{"exact match", "4.5.2", "4.5.2", true},
		{"padding satisfies", "4.5.2.0", "4.5.2", true},
		{"metadata satisfies", "4.5.2+build.8", "4.5.2", true},
		{"above floor", "4.5.3", "4.5.2", true},
		{"above floor numerically", "1.10", "1.9", true},
		{"prerelease does not satisfy release floor", "4.5.2-alpha", "4.5.2", false},
		{"below floor", "4.5.2", "4.5.3", false},
		{"empty version never satisfies", "", "1.0.0", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SatisfiesMinimum(c.version, c.floor); got != c.want {
				t.Fatalf("SatisfiesMinimum(%q, %q) = %t, want %t", c.version, c.floor, got, c.want)
			}
		})
	}
}

func FuzzCompareVersions(f *testing.F) {
	seeds := []string{
		"4.5.2", "4.5.2.0", "4.5.2+build.8", "4.5.2-alpha", "4.5.2-beta",
		"1.10", "1.9", "1.0", "1.0.0", "", "abc", "1..2", "-x", "4.5.2-",
		"99999999999999999999",
	}
	for _, a := range seeds {
		for _, b := range seeds {
			f.Add(a, b)
		}
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		ab, ba := CompareVersions(a, b), CompareVersions(b, a)
		if ab != -ba {
			t.Fatalf("antisymmetry broken: CompareVersions(%q, %q) = %d, CompareVersions(%q, %q) = %d", a, b, ab, b, a, ba)
		}
		if ab != -1 && ab != 0 && ab != 1 {
			t.Fatalf("CompareVersions(%q, %q) = %d, want -1, 0, or 1", a, b, ab)
		}
		if got := CompareVersions(a, a); got != 0 {
			t.Fatalf("CompareVersions(%q, %q) = %d, want 0", a, a, got)
		}
		if same := SameVersion(a, b); same != (ab == 0) {
			t.Fatalf("CompareVersions(%q, %q) = %d but SameVersion = %t: the two must agree", a, b, ab, same)
		}
	})
}
