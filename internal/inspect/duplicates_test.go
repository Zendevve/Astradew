// This file holds the tests for the preview's package-level judgments:
// duplicate Unique IDs, the scan limit's effect on installability, and the wire
// shape's always-non-nil slices.
package inspect

import (
	"fmt"
	"slices"
	"testing"

	"github.com/Zendevve/astradew/internal/manifest"
)

// TestInspectDuplicateUniqueIDs pins the launch blocker: one Unique ID in three
// folders is one group — compared case-insensitively, as SMAPI compares
// identities — with its entries in scan order and each version as authored.
func TestInspectDuplicateUniqueIDs(t *testing.T) {
	tree := manifest.NewFixture().
		ModVersion("ModA", "Shared.Id", "1.0.0").
		ModVersion("ModB", "shared.id", "2.0.0").
		ModVersion("ModC", "SHARED.ID", "3.0.0").
		FS()
	got := Inspect(tree, Options{})
	if len(got.Units) != 3 {
		t.Fatalf("units = %+v, want three", got.Units)
	}
	if len(got.Duplicates) != 1 {
		t.Fatalf("duplicates = %+v, want one group", got.Duplicates)
	}
	group := got.Duplicates[0]
	if group.UniqueID != "Shared.Id" {
		t.Fatalf("group.UniqueID = %q, want the first spelling as authored", group.UniqueID)
	}
	if !group.LaunchBlocker {
		t.Fatal("group.LaunchBlocker = false, want true")
	}
	want := []DuplicateEntry{
		{Path: "ModA", Version: "1.0.0"},
		{Path: "ModB", Version: "2.0.0"},
		{Path: "ModC", Version: "3.0.0"},
	}
	if !slices.Equal(group.Entries, want) {
		t.Fatalf("group.Entries = %+v, want scan order %+v", group.Entries, want)
	}
	if !got.Installable {
		t.Fatal("package installable = false, want true: duplicates are shown, not refused")
	}
}

// TestInspectSingleUnitHasNoDuplicates pins the group-of-one rule: the
// scanner's own extraction reports every participating unit as a group, and the
// preview's duplicate section is about duplicates only.
func TestInspectSingleUnitHasNoDuplicates(t *testing.T) {
	got := Inspect(manifest.NewFixture().Mod("SomeMod", "Some.Mod").FS(), Options{})
	if len(got.Duplicates) != 0 {
		t.Fatalf("duplicates = %+v, want none for a single unit", got.Duplicates)
	}
	if got.Duplicates == nil {
		t.Fatal("Duplicates is nil, want an empty slice")
	}
}

// TestInspectRootUnitParticipatesInDuplicates pins the synthetic record: a root
// manifest collides like any other unit, and it lands first in its group,
// matching its place in the units list.
func TestInspectRootUnitParticipatesInDuplicates(t *testing.T) {
	tree := manifest.NewFixture().
		File("manifest.json", rootManifestJSON("RootMod", "Shared.Id")).
		File("RootMod.dll", "MZ").
		Mod("ModA", "Shared.Id").
		FS()
	got := Inspect(tree, Options{})
	if paths := unitPaths(got); !slices.Equal(paths, []string{"", "ModA"}) {
		t.Fatalf("unit paths = %+v, want the root unit then ModA", paths)
	}
	if len(got.Duplicates) != 1 {
		t.Fatalf("duplicates = %+v, want one group", got.Duplicates)
	}
	want := []DuplicateEntry{{Path: "", Version: "1.0.0"}, {Path: "ModA", Version: "1.0.0"}}
	if !slices.Equal(got.Duplicates[0].Entries, want) {
		t.Fatalf("group.Entries = %+v, want %+v", got.Duplicates[0].Entries, want)
	}
}

// TestInspectScanLimitBlocksInstallability pins the honesty rule at the preview
// level. The scanner's work bounds are unexported — 10,000 folder listings and
// 100,000 entries by default — so the only honest trigger is a tree wide enough
// to spend them: AAAMod sorts first and is scanned before the bound is gone,
// and the filler folders exhaust it (each empty folder costs one listing when
// it is opened and another when its files are walked; AAAMod is found by
// manifest and costs one). A bounded scan cannot claim the package is complete,
// so the package is not installable even though its unit is.
func TestInspectScanLimitBlocksInstallability(t *testing.T) {
	const fillers = 10_050 // comfortably over the 10,000-listing bound
	fixture := manifest.NewFixture().Mod("AAAMod", "AAA.Mod")
	for i := 0; i < fillers; i++ {
		fixture.Dir(fmt.Sprintf("Filler%05d", i))
	}
	got := Inspect(fixture.FS(), Options{})
	if paths := unitPaths(got); !slices.Equal(paths, []string{"AAAMod"}) {
		t.Fatalf("unit paths = %+v, want [AAAMod]", paths)
	}
	if !got.Units[0].Installable {
		t.Fatalf("unit = %+v, want the unit itself installable", got.Units[0])
	}
	limit := findingsOf(got, FindingScanLimit)
	if len(limit) != 1 {
		t.Fatalf("scan-limit findings = %+v, want one", limit)
	}
	if limit[0].Path == "" || limit[0].Message == "" {
		t.Fatalf("scan-limit finding = %+v, want the folder it stopped on and the scanner's explanation", limit[0])
	}
	if got.Installable {
		t.Fatal("package installable = true, want false: the scan stopped early")
	}
}

// TestInspectSlicesAreNeverNil pins the wire shape: every slice the preview
// carries is non-nil, so the JSON is [] rather than null whatever the package
// turned out to be.
func TestInspectSlicesAreNeverNil(t *testing.T) {
	previews := map[string]Preview{
		"empty stage":   Inspect(manifest.NewFixture().FS(), Options{}),
		"findings only": Inspect(manifest.NewFixture().XnbMod("SomeXnb").FS(), Options{}),
		"a unit":        Inspect(manifest.NewFixture().Mod("SomeMod", "Some.Mod").FS(), Options{}),
	}
	for name, preview := range previews {
		if preview.Units == nil || preview.Findings == nil || preview.Duplicates == nil {
			t.Fatalf("%s: slices = %v/%v/%v, want empty slices rather than nil", name, preview.Units, preview.Findings, preview.Duplicates)
		}
	}
	for _, unit := range previews["a unit"].Units {
		if unit.Dependencies == nil || unit.FieldErrors == nil {
			t.Fatalf("unit slices = %v/%v, want empty slices rather than nil", unit.Dependencies, unit.FieldErrors)
		}
	}
}
