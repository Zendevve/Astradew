package settings

import (
	"context"
	"testing"

	"github.com/Zendevve/astradew/internal/archive"
)

// archiveLimitKey pairs one registry key with the archive.Limits field it must
// mirror, the inclusive ceiling issue #55 fixes for it, and a reader for the
// field. Keeping the field name and reader side by side is what lets a failure
// name the field, and lets a renamed or mistyped key fail rather than silently
// drop out of the correspondence.
var archiveLimitKeys = []struct {
	name  string
	field string
	read  func(archive.Limits) int64
	// low and high are the inclusive bounds Validate must accept; high+1 and
	// 0 must be refused.
	low, high int
}{
	{"archive-limit-archive-bytes", "MaxArchiveBytes", func(l archive.Limits) int64 { return l.MaxArchiveBytes }, 1, 1 << 40},
	{"archive-limit-expanded-bytes", "MaxExpandedBytes", func(l archive.Limits) int64 { return l.MaxExpandedBytes }, 1, 1 << 40},
	{"archive-limit-entries", "MaxEntries", func(l archive.Limits) int64 { return int64(l.MaxEntries) }, 1, 1_000_000},
	{"archive-limit-ratio", "MaxRatio", func(l archive.Limits) int64 { return int64(l.MaxRatio) }, 1, 10_000},
	{"archive-limit-path-depth", "MaxPathDepth", func(l archive.Limits) int64 { return int64(l.MaxPathDepth) }, 1, 256},
	{"archive-limit-path-bytes", "MaxPathBytes", func(l archive.Limits) int64 { return int64(l.MaxPathBytes) }, 1, 4_096},
}

// TestArchiveLimitDefaultsMatchArchive is the cross-package guard: every one of
// the six registry keys declares the same default, kind, and inclusive ceiling
// as the archive package's own budget. internal/archive does not import
// settings, so this test is the only thing that keeps the two tables from
// drifting apart — the settings numbers are what the inspection service will
// read, and DefaultLimits is what the extractor will apply when it reads them.
func TestArchiveLimitDefaultsMatchArchive(t *testing.T) {
	limits := archive.DefaultLimits()
	for _, tc := range archiveLimitKeys {
		decl, err := lookup(tc.name)
		if err != nil {
			t.Fatalf("lookup(%q) error = %v, want a declared archive limit key", tc.name, err)
		}
		if decl.Kind != KindInt {
			t.Fatalf("%s Kind = %q, want %q", tc.name, decl.Kind, KindInt)
		}
		if decl.Validate == nil {
			t.Fatalf("%s has no Validate, want the ceiling check", tc.name)
		}
		got, ok := decl.Default.(int)
		if !ok {
			t.Fatalf("%s Default = %#v (%T), want an int", tc.name, decl.Default, decl.Default)
		}
		if want := tc.read(limits); int64(got) != want {
			t.Fatalf("%s Default = %d, want archive.DefaultLimits().%s = %d", tc.name, got, tc.field, want)
		}
		if err := decl.Validate(decl.Default); err != nil {
			t.Fatalf("%s Validate(default) error = %v, want the declared default to pass its own ceiling", tc.name, err)
		}
		// The ceilings are hard limits, so they must be accepted at the
		// boundary and refused one past it, at the low end, and at zero —
		// otherwise a setting could widen a budget past what issue #55 fixes.
		for _, accept := range []int{tc.low, tc.high} {
			if err := decl.Validate(accept); err != nil {
				t.Fatalf("%s Validate(%d) error = %v, want the inclusive ceiling to accept it", tc.name, accept, err)
			}
		}
		for _, refuse := range []int{0, tc.high + 1} {
			if err := decl.Validate(refuse); err == nil {
				t.Fatalf("%s Validate(%d) = nil, want the hard ceiling to refuse it", tc.name, refuse)
			}
		}
	}
}

// TestArchiveLimitKeysResolveToArchiveDefaults resolves each key through the
// public Service.Get over a real store — the path the inspection service will
// use, so a key name typo cannot pass as a default. The value that comes back
// is compared to the archive field again, so the live read and the declaration
// are both pinned to archive.DefaultLimits.
func TestArchiveLimitKeysResolveToArchiveDefaults(t *testing.T) {
	svc, _, _ := openService(t)
	limits := archive.DefaultLimits()
	for _, tc := range archiveLimitKeys {
		got, err := svc.Get(context.Background(), tc.name)
		if err != nil {
			t.Fatalf("Get(%q) error = %v, want the declared default", tc.name, err)
		}
		number, ok := got.(int)
		if !ok {
			t.Fatalf("Get(%q) = %#v (%T), want an int", tc.name, got, got)
		}
		if want := tc.read(limits); int64(number) != want {
			t.Fatalf("Get(%q) = %d, want archive.DefaultLimits().%s = %d", tc.name, number, tc.field, want)
		}
	}
}
