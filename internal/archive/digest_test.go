// This file holds the package digest's tests. The digest is the identity ADR
// 0009's content-addressed store will key on, so what matters is not any one
// value but the three properties: it survives a second extraction of the same
// archive, it moves when any file's bytes move, and it ignores everything that
// is not a file's content.
package archive

import (
	"testing"
)

func TestPackageDigestIsStableAcrossExtractions(t *testing.T) {
	fixture := MultiMod()
	srcPath := writeFixture(t, fixture)

	first := extractInto(t, srcPath, t.TempDir(), DefaultLimits(), nil)
	second := extractInto(t, srcPath, t.TempDir(), DefaultLimits(), nil)

	if first.PackageSHA256 != second.PackageSHA256 {
		t.Fatalf("PackageSHA256 = %q then %q, want the same digest for the same archive",
			first.PackageSHA256, second.PackageSHA256)
	}
	if first.ArchiveSHA256 != second.ArchiveSHA256 {
		t.Fatalf("ArchiveSHA256 = %q then %q, want the same digest for the same file",
			first.ArchiveSHA256, second.ArchiveSHA256)
	}
}

func TestPackageDigestTracksFileBytes(t *testing.T) {
	original := NewFixture().
		File("Mod/manifest.json", `{"UniqueID":"Mushymato.FishZones"}`).
		File("Mod/Mod.dll", "MZ one")
	changed := NewFixture().
		File("Mod/manifest.json", `{"UniqueID":"Mushymato.FishZones"}`).
		File("Mod/Mod.dll", "MZ two")

	first := extractInto(t, writeFixture(t, original), t.TempDir(), DefaultLimits(), nil)
	second := extractInto(t, writeFixture(t, changed), t.TempDir(), DefaultLimits(), nil)

	if first.PackageSHA256 == second.PackageSHA256 {
		t.Fatalf("PackageSHA256 = %q for both archives, want a digest that changes with a file's bytes",
			first.PackageSHA256)
	}
	if first.ArchiveSHA256 == second.ArchiveSHA256 {
		t.Fatalf("ArchiveSHA256 = %q for both archives, want different bytes to hash differently",
			first.ArchiveSHA256)
	}
}

func TestPackageDigestIgnoresEmptyDirectories(t *testing.T) {
	plain := NewFixture().Mod("FishZones", "Mushymato.FishZones")
	withDirs := NewFixture().
		Dir("FishZones").
		Dir("FishZones/empty").
		Mod("FishZones", "Mushymato.FishZones").
		Dir("FishZones/also-empty")

	plainResult := extractInto(t, writeFixture(t, plain), t.TempDir(), DefaultLimits(), nil)
	dirsResult := extractInto(t, writeFixture(t, withDirs), t.TempDir(), DefaultLimits(), nil)

	if dirsResult.PackageSHA256 != plainResult.PackageSHA256 {
		t.Fatalf("PackageSHA256 = %q with empty directories, want the %q of the same files without them",
			dirsResult.PackageSHA256, plainResult.PackageSHA256)
	}
	if len(dirsResult.Files) != len(plainResult.Files) {
		t.Fatalf("Files = %d with empty directories, want the %d files without them",
			len(dirsResult.Files), len(plainResult.Files))
	}
	if got := dirsOf(t, dirsResult.StagePath); len(got) == len(dirsOf(t, plainResult.StagePath)) {
		t.Fatalf("directories = %v, want the explicitly empty ones to exist on disk", got)
	}
}
