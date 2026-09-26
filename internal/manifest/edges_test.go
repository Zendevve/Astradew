package manifest

import (
	"reflect"
	"testing"
)

// edgeRecord parses a manifest body and wraps it in the record shape a scan
// produces. It fails the test when the body is invalid: an invalid manifest
// never loads, so it declares no edges, and every fixture that uses this
// helper is meant to participate.
func edgeRecord(t *testing.T, path, body string) Record {
	t.Helper()
	m, verdict, errs := Parse([]byte(body))
	if verdict == VerdictInvalid {
		t.Fatalf("fixture %q unexpectedly invalid: %v", path, errs)
	}
	return Record{Path: path, Outcome: OutcomeContentPack, Unit: &Unit{Manifest: m, Verdict: verdict}}
}

// edgeCodeMod is a minimal valid code-mod manifest body at a chosen version.
func edgeCodeMod(uid, version string) string {
	return `{"Name":"` + uid + `","Author":"A","Version":"` + version + `","UniqueID":"` + uid + `","EntryDll":"Mod.dll"}`
}

func TestEdgesExtractDependenciesThenHostLink(t *testing.T) {
	pack := edgeRecord(t, "Packs/SVE", `{
		"Name": "SVE",
		"Author": "FlashShifter",
		"Version": "1.15.11",
		"Description": "An expansive fanmade mod.",
		"UniqueID": "FlashShifter.StardewValleyExpandedCP",
		"ContentPackFor": {"UniqueID": "Pathoschild.ContentPatcher", "MinimumVersion": "2.0.0"},
		"Dependencies": [
			{"UniqueID": "FlashShifter.SVE-FTM"},
			{"UniqueID": "MoreFish", "IsRequired": false},
			{"UniqueID": "Pathoschild.ContentPatcher", "MinimumVersion": "1.2.3"}
		]
	}`)
	hostOnly := edgeRecord(t, "Packs/Simple", `{
		"Name": "Simple",
		"Author": "A",
		"Version": "1.0.0",
		"Description": "A pack.",
		"UniqueID": "Author.SimpleCP",
		"ContentPackFor": {"UniqueID": "Pathoschild.ContentPatcher"}
	}`)

	want := []Edge{
		{
			From: "Packs/SVE", FromUniqueID: "FlashShifter.StardewValleyExpandedCP",
			Target: "FlashShifter.SVE-FTM", MinimumVersion: "", Required: true,
			Kind: EdgeManifestDependency,
		},
		{
			From: "Packs/SVE", FromUniqueID: "FlashShifter.StardewValleyExpandedCP",
			Target: "MoreFish", MinimumVersion: "", Required: false,
			Kind: EdgeManifestDependency,
		},
		{
			From: "Packs/SVE", FromUniqueID: "FlashShifter.StardewValleyExpandedCP",
			Target: "Pathoschild.ContentPatcher", MinimumVersion: "1.2.3", Required: true,
			Kind: EdgeManifestDependency,
		},
		{
			From: "Packs/SVE", FromUniqueID: "FlashShifter.StardewValleyExpandedCP",
			Target: "Pathoschild.ContentPatcher", MinimumVersion: "2.0.0", Required: true,
			Kind: EdgeContentPackHost,
		},
		{
			From: "Packs/Simple", FromUniqueID: "Author.SimpleCP",
			Target: "Pathoschild.ContentPatcher", MinimumVersion: "", Required: true,
			Kind: EdgeContentPackHost,
		},
	}
	if got := Edges([]Record{pack, hostOnly}); !reflect.DeepEqual(got, want) {
		t.Fatalf("Edges() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestEdgesSkipNonParticipatingRecords(t *testing.T) {
	good := edgeRecord(t, "Good", `{
		"Name": "Good",
		"Author": "A",
		"Version": "1.0.0",
		"UniqueID": "Author.Good",
		"EntryDll": "Mod.dll",
		"Dependencies": [{"UniqueID": "Author.Target"}]
	}`)

	// A manifest without its required identity never loads in SMAPI.
	m, verdict, _ := Parse([]byte(`{"Name":"B","Version":"1.0.0"}`))
	if verdict != VerdictInvalid {
		t.Fatalf("identity-less manifest verdict = %q, want invalid", verdict)
	}

	records := []Record{
		{Path: "Legacy", Outcome: OutcomeXnb, Reason: ReasonXnbMod},
		{Path: ".git", Outcome: OutcomeIgnored, Reason: ReasonIgnoredFolder},
		{Path: "Empty", Outcome: OutcomeInvalid, Reason: ReasonEmptyFolder},
		{Path: "Broken", Outcome: OutcomeInvalid, Reason: ReasonManifestInvalid, Unit: &Unit{Manifest: m, Verdict: verdict}},
		good,
	}

	want := []Edge{{
		From: "Good", FromUniqueID: "Author.Good", Target: "Author.Target",
		MinimumVersion: "", Required: true, Kind: EdgeManifestDependency,
	}}
	if got := Edges(records); !reflect.DeepEqual(got, want) {
		t.Fatalf("Edges() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestDuplicateGroupsSameIDAcrossFolders(t *testing.T) {
	records := []Record{
		edgeRecord(t, "Mods/A/Harvest", edgeCodeMod("Author.Mod", "1.0.0")),
		edgeRecord(t, "Mods/B/Harvest", edgeCodeMod("Author.Mod", "2.0.0")),
		edgeRecord(t, "Mods/C/Other", edgeCodeMod("Other.Mod", "0.5.0")),
	}

	want := []DuplicateGroup{
		{
			UniqueID: "Author.Mod",
			Entries: []DuplicateEntry{
				{Path: "Mods/A/Harvest", Version: "1.0.0"},
				{Path: "Mods/B/Harvest", Version: "2.0.0"},
			},
			LaunchBlocker: true,
		},
		{
			UniqueID:      "Other.Mod",
			Entries:       []DuplicateEntry{{Path: "Mods/C/Other", Version: "0.5.0"}},
			LaunchBlocker: false,
		},
	}
	if got := DuplicateGroups(records); !reflect.DeepEqual(got, want) {
		t.Fatalf("DuplicateGroups() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestDuplicateGroupsMixedCaseIDsAreOneGroup(t *testing.T) {
	records := []Record{
		edgeRecord(t, "Mods/First", edgeCodeMod("Author.Harvest", "1.0.0")),
		edgeRecord(t, "Mods/Second", edgeCodeMod("author.harvest", "2.0.0")),
	}

	got := DuplicateGroups(records)
	if len(got) != 1 {
		t.Fatalf("groups = %+v, want exactly one (Unique IDs compare case-insensitively)", got)
	}
	if got[0].UniqueID != "Author.Harvest" {
		t.Fatalf("group UniqueID = %q, want the first occurrence's spelling", got[0].UniqueID)
	}
	wantEntries := []DuplicateEntry{
		{Path: "Mods/First", Version: "1.0.0"},
		{Path: "Mods/Second", Version: "2.0.0"},
	}
	if !reflect.DeepEqual(got[0].Entries, wantEntries) {
		t.Fatalf("entries = %+v, want %+v", got[0].Entries, wantEntries)
	}
	if !got[0].LaunchBlocker {
		t.Fatal("LaunchBlocker = false, want true for a two-entry group")
	}
}

func TestDuplicateGroupsExcludeInvalidManifests(t *testing.T) {
	// The invalid manifest still carries the ID, so including it would flip
	// the group to a launch blocker; an invalid manifest never loads.
	const broken = `{"Name":"B","Version":"1.0.0","UniqueID":"Author.Mod","EntryDll":"bad/name.dll"}`
	m, verdict, errs := Parse([]byte(broken))
	if verdict != VerdictInvalid {
		t.Fatalf("fixture verdict = %q (%v), want invalid", verdict, errs)
	}
	if m.UniqueID != "Author.Mod" {
		t.Fatalf("fixture UniqueID = %q, want Author.Mod retained", m.UniqueID)
	}

	records := []Record{
		edgeRecord(t, "Mods/Good", edgeCodeMod("Author.Mod", "1.0.0")),
		{Path: "Mods/Broken", Outcome: OutcomeInvalid, Reason: ReasonManifestInvalid, Unit: &Unit{Manifest: m, Verdict: verdict}},
	}

	want := []DuplicateGroup{{
		UniqueID:      "Author.Mod",
		Entries:       []DuplicateEntry{{Path: "Mods/Good", Version: "1.0.0"}},
		LaunchBlocker: false,
	}}
	if got := DuplicateGroups(records); !reflect.DeepEqual(got, want) {
		t.Fatalf("DuplicateGroups() =\n%+v\nwant\n%+v", got, want)
	}
}
