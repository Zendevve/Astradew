package manifest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestFixtureComposesEveryScannerState(t *testing.T) {
	tree := NewFixture().
		Mod("Alpha", "Alpha.Mod").
		Pack("SVE", "FlashShifter.SVE", "Pathoschild.ContentPatcher").
		Partial("Partial", "Partial.Mod").
		BrokenManifest("Broken", `{"Name":"Broken"`).
		File("Broken/Mod.dll", "MZ").
		MissingManifest("Leftovers").
		XnbMod("Legacy").
		Ignored(".Trash", "Trash.Mod").
		Mod("Group/Sub/Nested", "Nested.Mod").
		Mod("CopyA", "Duplicate.Mod").
		Mod("CopyB", "Duplicate.Mod").
		Mod("CopyC", "DUPLICATE.MOD").
		SystemMod("Renamed Bundle").
		Mod("SMAPI.ConsoleCommands", "ThirdParty.Fake").
		FS()

	records := Scan(tree, Options{})
	got := byPath(records)
	if len(records) != 13 {
		t.Fatalf("records = %v, want the thirteen folders the fixture declares", scanPaths(records))
	}

	if r := got["Alpha"]; r.Outcome != OutcomeSmapi || r.Unit == nil || r.Unit.Verdict != VerdictValid {
		t.Fatalf("Alpha = %+v, want a valid smapi unit", r)
	}
	if r := got["SVE"]; r.Outcome != OutcomeContentPack || r.Unit.Manifest.ContentPackFor.UniqueID != "Pathoschild.ContentPatcher" {
		t.Fatalf("SVE = %+v, want a content pack for Content Patcher", r)
	}
	if r := got["Partial"]; r.Outcome != OutcomeSmapi || r.Unit.Verdict != VerdictPartial || len(r.Unit.Errors) == 0 {
		t.Fatalf("Partial = %+v, want a partial unit carrying its warning", r)
	}
	if r := got["Broken"]; r.Outcome != OutcomeInvalid || r.Reason != ReasonManifestInvalid || r.Unit == nil {
		t.Fatalf("Broken = %+v, want invalid/manifest-invalid with its detail", r)
	} else if !strings.HasPrefix(r.Note, "parsing its manifest failed: ") {
		t.Fatalf("Broken note = %q, want the failure named", r.Note)
	}
	if r := got["Leftovers"]; r.Outcome != OutcomeInvalid || r.Reason != ReasonManifestMissing {
		t.Fatalf("Leftovers = %+v, want invalid/manifest-missing", r)
	}
	if r := got["Legacy"]; r.Outcome != OutcomeXnb || r.Reason != ReasonXnbMod {
		t.Fatalf("Legacy = %+v, want xnb/xnb-mod", r)
	}
	if r := got[".Trash"]; r.Outcome != OutcomeIgnored || r.Reason != ReasonIgnoredFolder || r.Unit != nil {
		t.Fatalf(".Trash = %+v, want ignored/ignored-folder without a unit", r)
	}
	if r := got["Group/Sub/Nested"]; r.Outcome != OutcomeSmapi {
		t.Fatalf("nested unit = %+v, want smapi", r)
	}

	if r := got["CopyA"]; r.Unit == nil || r.Unit.Manifest.UniqueID != got["CopyB"].Unit.Manifest.UniqueID {
		t.Fatalf("CopyA/CopyB = %+v/%+v, want the same Unique ID in both folders", got["CopyA"], got["CopyB"])
	}
	if r := got["CopyC"]; r.Unit == nil || !strings.EqualFold(r.Unit.Manifest.UniqueID, got["CopyA"].Unit.Manifest.UniqueID) {
		t.Fatalf("CopyC = %+v, want the same Unique ID in another case", r)
	}

	if r := got["Renamed Bundle"]; r.Unit == nil || !r.Unit.SystemMod {
		t.Fatalf("Renamed Bundle = %+v, want a System Mod: identity, not the folder name, confers it", r)
	}
	if r := got["SMAPI.ConsoleCommands"]; r.Unit == nil || r.Unit.SystemMod {
		t.Fatalf("SMAPI.ConsoleCommands = %+v, want an ordinary unit: a forged folder name confers nothing", r)
	}
}

func TestFixtureFSHandsOutACopy(t *testing.T) {
	fixture := NewFixture().Mod("Alpha", "Alpha.Mod")
	scan := fixture.FS()
	delete(scan, "Alpha/manifest.json")
	scan["Alpha/manifest.json"] = &fstest.MapFile{Data: []byte(`{"Name":"Alpha","Author":"A","Version":"2.0.0","UniqueID":"Alpha.Mod","EntryDll":"Mod.dll"}`)}

	records := byPath(Scan(fixture.FS(), Options{}))
	if unit := records["Alpha"].Unit; unit == nil || unit.Manifest.Version != "1.0.0" {
		t.Fatalf("unit = %+v, want the fixture's own version: a tree handed out must not alias the fixture", unit)
	}

	// An in-place byte edit must not reach the fixture either: staging a
	// manifest rewrite between scans is exactly how a test would do it.
	inPlace := fixture.FS()
	inPlace["Alpha/manifest.json"].Data[0] = '_'
	again := byPath(Scan(fixture.FS(), Options{}))
	if unit := again["Alpha"].Unit; unit == nil || unit.Manifest.Version != "1.0.0" {
		t.Fatalf("unit = %+v, want the fixture's own bytes: file Data must not alias the fixture", unit)
	}
}

// TestFixtureRealDirectoryRoundTrip is the fixture library's one anchor on a
// real filesystem. It proves that the in-memory trees the rest of the suite
// composes match what the OS reports — separators, entry order, mtimes — and
// that the cache follows edits made on disk. Everything here is unprivileged,
// so it runs on every CI platform.
func TestFixtureRealDirectoryRoundTrip(t *testing.T) {
	tree := NewFixture().
		Mod("Alpha", "Alpha.Mod").
		Mod("Gamma", "Gamma.Mod").
		Pack("SVE", "FlashShifter.SVE", "Pathoschild.ContentPatcher").
		Partial("Partial", "Partial.Mod").
		BrokenManifest("Broken", `{"Name":"Broken"`).
		File("Broken/Mod.dll", "MZ").
		MissingManifest("Leftovers").
		XnbMod("Legacy").
		Ignored(".Trash", "Trash.Mod").
		SystemMod("Renamed Bundle").
		Dir("Empty").
		FS()

	memory := Scan(tree, Options{Cache: NewCache()})

	dir := t.TempDir()
	writeTree(t, dir, tree)
	root := os.DirFS(dir)
	opts := Options{Cache: NewCache()}
	onDisk := Scan(root, opts)

	if !reflect.DeepEqual(memory, onDisk) {
		t.Fatalf("in-memory and on-disk scans disagree:\nmemory:  %+v\non disk: %+v", memory, onDisk)
	}

	// A longer rewrite on disk is a size change: the cache must re-parse that
	// manifest, and only that one.
	alpha := filepath.Join(dir, "Alpha", "manifest.json")
	grown := `{"Name":"Alpha","Author":"A","Version":"1.0.0","Description":"a longer body","UniqueID":"Alpha.Mod","EntryDll":"Mod.dll"}`
	if err := os.WriteFile(alpha, []byte(grown), 0o644); err != nil {
		t.Fatalf("rewrite %s: %v", alpha, err)
	}
	afterEdit := byPath(Scan(root, opts))
	if unit := afterEdit["Alpha"].Unit; unit == nil || unit.Manifest.Description != "a longer body" {
		t.Fatalf("Alpha after an edit = %+v, want the rewritten body's description", unit)
	}
	if afterEdit["Gamma"].Unit != byPath(onDisk)["Gamma"].Unit {
		t.Fatal("Gamma was re-parsed although only Alpha changed on disk")
	}

	// The same length with a new modification time: SMAPI's own rule says a
	// moved timestamp is an edit, so the parse is redone even though the size
	// the stat reports didn't move.
	sameSize := `{"Name":"Alpha","Author":"A","Version":"2.0.0","Description":"a longer body","UniqueID":"Alpha.Mod","EntryDll":"Mod.dll"}`
	if len(sameSize) != len(grown) {
		t.Fatal("the rewrite must keep the file's size for this test to mean anything")
	}
	if err := os.WriteFile(alpha, []byte(sameSize), 0o644); err != nil {
		t.Fatalf("rewrite %s: %v", alpha, err)
	}
	when := time.Date(2001, time.February, 3, 4, 5, 6, 0, time.UTC)
	if err := os.Chtimes(alpha, when, when); err != nil {
		t.Fatalf("chtimes %s: %v", alpha, err)
	}
	afterTouch := byPath(Scan(root, opts))
	if unit := afterTouch["Alpha"].Unit; unit == nil || unit.Manifest.Version != "2.0.0" {
		t.Fatalf("Alpha after a same-size rewrite with a new mtime = %+v, want the new version", unit)
	}
}

// writeTree materializes an in-memory fixture tree on a real filesystem: the
// one test-local writer the round-trip anchor needs. MapFS models nothing
// beyond the directory bit that a real folder needs, and mtimes are applied
// where the fixture asked for one.
func writeTree(t *testing.T, dir string, tree fstest.MapFS) {
	t.Helper()
	for name, file := range tree {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if file.Mode.IsDir() {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", full, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, file.Data, 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
		if !file.ModTime.IsZero() {
			if err := os.Chtimes(full, file.ModTime, file.ModTime); err != nil {
				t.Fatalf("chtimes %s: %v", full, err)
			}
		}
	}
}
