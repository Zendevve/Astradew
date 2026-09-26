// This file holds the tests for the scan's work bounds: the trio that keeps a
// link loop, or any other pathological tree, from expanding a scan without end.
// The double behind them follows links but cannot name their canonical identity,
// which is the shape a plain os.DirFS presents — and exactly the bridge the
// visited-set guard cannot help.
package manifest

import (
	"fmt"
	"path"
	"testing"
	"testing/fstest"
)

// stopRecords returns the records that mark where a bound ended the scan.
func stopRecords(records []Record) []Record {
	var out []Record
	for _, r := range records {
		if r.Reason == ReasonScanLimit {
			out = append(out, r)
		}
	}
	return out
}

// linkFarm returns a filesystem whose single folder holds width links back to
// the Mods root: the shape that made a listing-only bound multiply its records
// by the number of passes over that folder.
func linkFarm(width int) uncanonicalFS {
	tree := fstest.MapFS{"Farm/.keep": &fstest.MapFile{Data: []byte{}}}
	links := make(map[string]string, width)
	for i := range width {
		links[fmt.Sprintf("Farm/L%04d", i)] = "."
	}
	return uncanonicalFS{aliasFS{MapFS: tree, links: links}}
}

// TestScanBudgetBoundsBranchingLinkLoop is the regression for the tree that
// hung the scanner: two links back to an ancestor, which the fallback
// visited-set key can never match, so recursion fans out ~2^depth until a bound
// stops it. Without the bounds this test does not return.
func TestScanBudgetBoundsBranchingLinkLoop(t *testing.T) {
	const limit = 12
	probe := newProbe(uncanonicalFS{aliasFS{
		MapFS: fstest.MapFS{"A/.keep": &fstest.MapFile{Data: []byte{}}},
		links: map[string]string{"A/L1": ".", "A/L2": "."},
	}})

	records := Scan(probe, Options{scanLimit: limit})

	// The Mods root listing is free; every folder below it draws from the
	// bound, so the scan reads exactly limit+1 listings and then stops.
	if want := limit + 1; len(probe.dirs) != want {
		t.Fatalf("read %d folder listings, want %d: the bound must stop the walk (reads: %v)", len(probe.dirs), want, probe.dirs)
	}
	stops := stopRecords(records)
	if len(stops) != 1 {
		t.Fatalf("records = %v, want one scan-limit record naming where the scan stopped", scanPaths(records))
	}
	if stops[0].Outcome != OutcomeIgnored || stops[0].Note != noteScanLimit {
		t.Fatalf("stop record = %+v, want ignored with the limit note", stops[0])
	}
}

// TestScanBudgetStopsAtTheEntryBound pins the fix for the amplification a
// listing-only bound allowed: one wide folder of links is re-read on every pass,
// so charging per entry — not per listing — is what keeps the records and
// visited keys from scaling with listings x width.
func TestScanBudgetStopsAtTheEntryBound(t *testing.T) {
	const (
		width = 500
		limit = 200
	)
	probe := newProbe(linkFarm(width))

	records := Scan(probe, Options{entryLimit: limit})

	if got := len(records); got > limit+1 {
		t.Fatalf("records = %d, want at most the entry bound plus one stop record", got)
	}
	if stops := stopRecords(records); len(stops) != 1 {
		t.Fatalf("stop records = %+v, want exactly one: the scan ends at the bound", stops)
	}
	if got := len(probe.dirs); got > 2 {
		t.Fatalf("read %d folder listings, want the root and Farm only: the entry bound ends the scan inside its first listing", got)
	}
}

// TestScanBudgetStopsAtTheListingBound runs the same farm against the listing
// bound instead: however wide the folder is, the scan stops after its budget of
// listings and reports where.
func TestScanBudgetStopsAtTheListingBound(t *testing.T) {
	const (
		width = 500
		limit = 5
	)
	probe := newProbe(linkFarm(width))

	records := Scan(probe, Options{scanLimit: limit})

	if want := limit + 1; len(probe.dirs) != want {
		t.Fatalf("read %d folder listings, want %d (reads: %v)", len(probe.dirs), want, probe.dirs)
	}
	stops := stopRecords(records)
	if len(records) != 1 || len(stops) != 1 || stops[0].Outcome != OutcomeIgnored {
		t.Fatalf("records = %+v, want one ignored/scan-limit record", records)
	}
}

// TestScanDepthCapRefusesRunawayNesting pins the depth cap: a chain deeper than
// any SMAPI-visible tree is refused with the scan-limit record instead of letting
// the paths the visited set retains grow without bound, and the refusal ends the
// scan — one record says where scanning stopped, whatever came after it.
func TestScanDepthCapRefusesRunawayNesting(t *testing.T) {
	deep := "Deep"
	for i := 0; i < maxScanDepth+4; i++ {
		deep = path.Join(deep, "d")
	}
	tree := fstest.MapFS{
		path.Join(deep, "manifest.json"): &fstest.MapFile{Data: []byte(codeMod("Deep.Mod"))},
		"Ordinary/manifest.json":         &fstest.MapFile{Data: []byte(codeMod("Ordinary.Mod"))},
	}

	records := Scan(tree, Options{})

	if len(records) != 1 {
		t.Fatalf("records = %v, want one record: the chain below the cap is refused, and the refusal ends the scan", scanPaths(records))
	}
	if r := records[0]; r.Outcome != OutcomeIgnored || r.Reason != ReasonScanLimit || r.Note != noteScanLimit {
		t.Fatalf("record = %+v, want ignored/scan-limit at the depth cap", r)
	}
}

// TestScanBudgetNeverClassifiesFromAPartialFileWalk pins the honesty rule: the
// file walk that decides empty-versus-XNB-versus-missing can be cut short by a
// bound, and a folder the scan did not finish reading must never be classified
// from the fragment. Before the rule, the record below came back
// manifest-missing.
func TestScanBudgetNeverClassifiesFromAPartialFileWalk(t *testing.T) {
	probe := newProbe(fstest.MapFS{
		"Mixed/notes.json":     &fstest.MapFile{Data: []byte("{}")},
		"Mixed/Sub/mod.dll":    &fstest.MapFile{Data: []byte("dll")},
		"Mixed/Sub/Deep/x.dll": &fstest.MapFile{Data: []byte("dll")},
	})

	// The root listing is free; "Mixed" spends one listing and the file walk's
	// own listing of "Mixed" spends the second, so "Mixed/Sub" is never reached.
	records := Scan(probe, Options{scanLimit: 2})

	if len(records) != 1 {
		t.Fatalf("records = %v, want the one folder under the root", scanPaths(records))
	}
	if r := records[0]; r.Path != "Mixed" || r.Outcome != OutcomeIgnored || r.Reason != ReasonScanLimit {
		t.Fatalf("record = %+v, want Mixed ignored/scan-limit: a truncated file listing must not classify", r)
	}
}

// TestScanBudgetStopsTheScanAndSaysWhere runs the same rule on a boring tree:
// the folders inside the bound are units, and the folder that hits the bound is
// named once instead of every folder after it being reported.
func TestScanBudgetStopsTheScanAndSaysWhere(t *testing.T) {
	const (
		limit   = 4
		folders = 30
	)
	tree := fstest.MapFS{}
	for i := range folders {
		tree[fmt.Sprintf("Mod%02d/manifest.json", i)] = &fstest.MapFile{Data: []byte(codeMod(fmt.Sprintf("Mod%02d.UID", i)))}
	}
	probe := newProbe(tree)

	records := Scan(probe, Options{scanLimit: limit})

	if want := limit + 1; len(probe.dirs) != want {
		t.Fatalf("read %d folder listings, want %d (reads: %v)", len(probe.dirs), want, probe.dirs)
	}
	units := 0
	for _, r := range records {
		switch {
		case r.Outcome == OutcomeSmapi && r.Reason == "":
			units++
		case r.Outcome == OutcomeIgnored && r.Reason == ReasonScanLimit:
		default:
			t.Fatalf("record %q = %s/%s, want a unit or the stop record", r.Path, r.Outcome, r.Reason)
		}
	}
	if units != limit {
		t.Fatalf("units = %d, want %d: only the folders inside the bound are read", units, limit)
	}
	stops := stopRecords(records)
	if len(stops) != 1 || stops[0].Path != "Mod04" || stops[0].Note != noteScanLimit {
		t.Fatalf("stop records = %+v, want exactly one at Mod04 with the limit note", stops)
	}
}

// TestScanDefaultBudgetLeavesOrdinaryTreesAlone keeps the safety valve from
// becoming product behaviour: an ordinary tree, nested organizers included, must
// scan with no scan-limit record at all.
func TestScanDefaultBudgetLeavesOrdinaryTreesAlone(t *testing.T) {
	fixture := NewFixture().
		Mod("Alpha", "Alpha.Mod").
		MissingManifest("Leftovers").
		XnbMod("Legacy").
		Pack("SVE", "FlashShifter.SVE", "Pathoschild.ContentPatcher").
		Mod("Group/Sub/Nested", "Nested.Mod")
	for i := range 40 {
		fixture.Dir(fmt.Sprintf("Group/Sub%02d", i))
	}

	records := Scan(fixture.FS(), Options{})
	if len(records) == 0 {
		t.Fatal("no records: the fixture must produce a report")
	}
	if stops := stopRecords(records); len(stops) != 0 {
		t.Fatalf("the default bounds truncated an ordinary tree: %+v", stops)
	}
}
