// This file holds the tests for the findings the preview derives: the layouts
// the scanner reports as records, the two root structures only the root pass
// can classify, and the pruning rules that must stay silent.
package inspect

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/Zendevve/astradew/internal/manifest"
)

// TestInspectReportsHiddenManifestWithoutInventingAUnit covers the poisoned
// organizer: a file beside a nested mod folder makes SMAPI read the parent as
// one manifest-less folder, so the nested mod is reported — with re-pack
// guidance — and never promoted to a unit. The preview reports; it never fixes.
func TestInspectReportsHiddenManifestWithoutInventingAUnit(t *testing.T) {
	tree := manifest.NewFixture().
		File("Wrapper/Mod.dll", "MZ").
		Mod("Wrapper/ModA", "ModA.Id").
		FS()
	got := Inspect(tree, Options{})
	if len(got.Units) != 0 {
		t.Fatalf("units = %+v, want none: the nested manifest is reported, never repaired", got.Units)
	}
	hidden := findingsOf(got, FindingHiddenManifests)
	if len(hidden) != 1 {
		t.Fatalf("hidden-manifests findings = %+v, want one", hidden)
	}
	if hidden[0].Path != "Wrapper/ModA/manifest.json" {
		t.Fatalf("hidden-manifests path = %q, want %q", hidden[0].Path, "Wrapper/ModA/manifest.json")
	}
	if !strings.Contains(hidden[0].Message, "Re-pack") {
		t.Fatalf("hidden-manifests message = %q, want re-pack guidance", hidden[0].Message)
	}
	if len(got.Findings) != 1 {
		t.Fatalf("findings = %+v, want only the hidden-manifests finding", got.Findings)
	}
	if got.Installable {
		t.Fatal("package installable = true, want false")
	}
}

// TestInspectProbePrunesMetadataAndDotFolders pins the probe's pruning: a
// manifest inside __MACOSX noise or a dot-prefixed folder below a manifest-less
// folder must never report a phantom mod, and the same walk stays silent in the
// Content tree's own search.
func TestInspectProbePrunesMetadataAndDotFolders(t *testing.T) {
	tests := []struct {
		name string
		tree *manifest.Fixture
	}{
		{
			name: "macOS metadata folder",
			tree: manifest.NewFixture().
				File("Packed/Mod.dll", "MZ").
				File("Packed/__MACOSX/Mod/manifest.json", rootManifestJSON("Ghost", "Ghost.Mod")),
		},
		{
			name: "dot-prefixed folder",
			tree: manifest.NewFixture().
				File("Packed/Mod.dll", "MZ").
				File("Packed/.hidden/manifest.json", rootManifestJSON("Ghost", "Ghost.Mod")),
		},
		{
			name: "Vortex marker beside the files",
			tree: manifest.NewFixture().
				File("Packed/Mod.dll", "MZ").
				File("Packed/__folder_managed_by_vortex/manifest.json", rootManifestJSON("Ghost", "Ghost.Mod")),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Inspect(test.tree.FS(), Options{})
			if len(got.Units) != 0 {
				t.Fatalf("units = %+v, want none", got.Units)
			}
			if len(got.Findings) != 0 {
				t.Fatalf("findings = %+v, want none: manager metadata is pruned, never reported", got.Findings)
			}
		})
	}
}

// TestInspectManifestLessFolderOfOrdinaryFilesIsSilent pins the honest empty
// answer: a folder holding ordinary files and no manifest is not a unit and not
// a finding, so the package simply shows as holding nothing usable.
func TestInspectManifestLessFolderOfOrdinaryFilesIsSilent(t *testing.T) {
	tree := manifest.NewFixture().
		MissingManifest("Packed").
		File("Packed/assets.json", "{}").
		FS()
	got := Inspect(tree, Options{})
	if len(got.Units) != 0 {
		t.Fatalf("units = %+v, want none", got.Units)
	}
	if len(got.Findings) != 0 {
		t.Fatalf("findings = %+v, want none", got.Findings)
	}
	if got.Installable {
		t.Fatal("package installable = true, want false")
	}
}

// TestInspectLegacyXnbPackage covers PRD §16: an XNB drop beside its folder
// finding, plus the package-level mark carrying the verbatim copy as the last
// finding.
func TestInspectLegacyXnbPackage(t *testing.T) {
	tree := manifest.NewFixture().XnbMod("SomeXnb").FS()
	got := Inspect(tree, Options{})
	if len(got.Units) != 0 {
		t.Fatalf("units = %+v, want none", got.Units)
	}
	xnb := findingsOf(got, FindingLegacyXnb)
	if len(xnb) != 2 {
		t.Fatalf("legacy-xnb findings = %+v, want the folder finding then the package mark", xnb)
	}
	if xnb[0].Path != "SomeXnb" {
		t.Fatalf("folder finding path = %q, want %q", xnb[0].Path, "SomeXnb")
	}
	if xnb[0].Message == "" {
		t.Fatal("folder finding message is empty, want the scanner's XNB explanation")
	}
	if xnb[1].Path != "" {
		t.Fatalf("package mark path = %q, want the stage root's empty path", xnb[1].Path)
	}
	if xnb[1].Message != wantLegacyXnbNotice {
		t.Fatalf("package mark message = %q, want PRD §16's copy verbatim", xnb[1].Message)
	}
	if got.Findings[len(got.Findings)-1].Kind != FindingLegacyXnb {
		t.Fatalf("last finding = %+v, want the package-level XNB mark", got.Findings[len(got.Findings)-1])
	}
	if got.Installable {
		t.Fatal("package installable = true, want false")
	}
}

// TestInspectRootContentTree covers the root Content tree: game-folder content
// parked where mods go, reported as one finding in the place the tree's records
// would have held, with bonus content beside it not counting.
func TestInspectRootContentTree(t *testing.T) {
	tree := manifest.NewFixture().
		File("Content/Characters/Abigail.xnb", "xnb").
		File("Content/Data/Objects.json", "{}").
		FS()
	got := Inspect(tree, Options{})
	if len(got.Units) != 0 {
		t.Fatalf("units = %+v, want none", got.Units)
	}
	trees := findingsOf(got, FindingRootContentTree)
	if len(trees) != 1 {
		t.Fatalf("root-content-tree findings = %+v, want one", trees)
	}
	if trees[0].Path != "Content" {
		t.Fatalf("root-content-tree path = %q, want %q", trees[0].Path, "Content")
	}
	if !strings.Contains(trees[0].Message, wantLegacyXnbNotice) {
		t.Fatalf("root-content-tree message = %q, want PRD §16's copy in it", trees[0].Message)
	}
	if !strings.Contains(trees[0].Message, "It belongs in the game folder, not in Mods.") {
		t.Fatalf("root-content-tree message = %q, want the game-folder sentence", trees[0].Message)
	}
	// The tree's records are replaced by that one finding, so no per-folder
	// legacy-xnb for it survives: the remaining legacy-xnb finding is the
	// package-level mark.
	xnb := findingsOf(got, FindingLegacyXnb)
	if len(xnb) != 1 || xnb[0].Path != "" {
		t.Fatalf("legacy-xnb findings = %+v, want only the package-level mark", xnb)
	}
	if len(got.Findings) != 2 {
		t.Fatalf("findings = %+v, want the tree finding and the mark", got.Findings)
	}
	if got.Findings[0].Kind != FindingRootContentTree {
		t.Fatalf("first finding = %+v, want the tree finding where its records were", got.Findings[0])
	}
	if got.Installable {
		t.Fatal("package installable = true, want false")
	}
}

// TestInspectRootContentTreeBesideAUnit pins the tree finding's condition: with
// a unit in the package the tree is not the package's whole story, so the
// scanner's per-folder XNB finding stands in its place and the package still
// installs.
func TestInspectRootContentTreeBesideAUnit(t *testing.T) {
	tree := manifest.NewFixture().
		Mod("ModA", "A.Mod").
		File("Content/Characters/Abigail.xnb", "xnb").
		FS()
	got := Inspect(tree, Options{})
	if trees := findingsOf(got, FindingRootContentTree); len(trees) != 0 {
		t.Fatalf("root-content-tree findings = %+v, want none beside a unit", trees)
	}
	xnb := findingsOf(got, FindingLegacyXnb)
	if len(xnb) != 1 || xnb[0].Path != "Content/Characters" {
		t.Fatalf("legacy-xnb findings = %+v, want the folder finding only", xnb)
	}
	if !got.Installable {
		t.Fatal("package installable = false, want true: the unit installs")
	}
}

// TestInspectSmapiBundles covers both SMAPI bundles: the installer the user
// unzipped (the scanner's own installer record) and the extracted payload
// directory the root pass recognizes, neither of them a Mod Unit.
func TestInspectSmapiBundles(t *testing.T) {
	tree := manifest.NewFixture().
		File("SMAPI Installer/install on Windows.bat", "x").
		File("SMAPI Installer/internal/0Harmony.dll", "MZ").
		File("smapi-internal/0Harmony.dll", "MZ").
		FS()
	got := Inspect(tree, Options{})
	if len(got.Units) != 0 {
		t.Fatalf("units = %+v, want none", got.Units)
	}
	installer := findingsOf(got, FindingSMAPIInstaller)
	if len(installer) != 1 || installer[0].Path != "SMAPI Installer" {
		t.Fatalf("smapi-installer findings = %+v, want one at SMAPI Installer", installer)
	}
	if installer[0].Message == "" {
		t.Fatal("smapi-installer message is empty, want the scanner's explanation")
	}
	payload := findingsOf(got, FindingSMAPIPayload)
	if len(payload) != 1 || payload[0].Path != "smapi-internal" {
		t.Fatalf("smapi-payload findings = %+v, want one at smapi-internal", payload)
	}
	if !strings.Contains(payload[0].Message, "install.dat") {
		t.Fatalf("smapi-payload message = %q, want the installer payload explained", payload[0].Message)
	}
	if len(got.Findings) != 2 {
		t.Fatalf("findings = %+v, want the installer record then the payload", got.Findings)
	}
	if got.Findings[0].Kind != FindingSMAPIInstaller || got.Findings[1].Kind != FindingSMAPIPayload {
		t.Fatalf("findings = %+v, want the record-derived finding before the root pass's", got.Findings)
	}
	if got.Installable {
		t.Fatal("package installable = true, want false")
	}
}

// TestInspectPrunesManagerMetadata keeps metadata from ever reaching the
// preview: the junk an archive picks up along the way is not a unit, not a
// finding, and not a reason to refuse the package.
func TestInspectPrunesManagerMetadata(t *testing.T) {
	tree := manifest.NewFixture().
		Mod("GoodMod", "Good.Mod").
		File("__MACOSX/x", "x").
		File("._resource", "x").
		File(".DS_Store", "x").
		File("mcs", "x").
		File("desktop.ini", "x").
		File("Thumbs.db", "x").
		FS()
	got := Inspect(tree, Options{})
	if len(got.Units) != 1 || got.Units[0].RelativePath != "GoodMod" {
		t.Fatalf("units = %+v, want only GoodMod", got.Units)
	}
	if len(got.Findings) != 0 {
		t.Fatalf("findings = %+v, want none from the junk", got.Findings)
	}
	if !got.Installable {
		t.Fatal("package installable = false, want true")
	}
}

// TestInspectReportsIgnoredEmptyAndVortexFolders covers the three folders SMAPI
// skips, each reported once, by path, with the scanner's own explanation.
func TestInspectReportsIgnoredEmptyAndVortexFolders(t *testing.T) {
	tree := manifest.NewFixture().
		Mod("GoodMod", "Good.Mod").
		Ignored("Hidden", "Hidden.Mod").
		Dir("EmptyFolder").
		File("VortexLeft/__folder_managed_by_vortex", "").
		FS()
	got := Inspect(tree, Options{})
	if len(got.Units) != 1 || got.Units[0].RelativePath != "GoodMod" {
		t.Fatalf("units = %+v, want only GoodMod", got.Units)
	}
	if kinds := findingKinds(got); !slices.Equal(kinds, []FindingKind{
		FindingIgnoredFolder, FindingEmptyFolder, FindingVortexLeftover,
	}) {
		t.Fatalf("finding kinds = %+v, want the scan's three skips in order", kinds)
	}
	if got.Findings[0].Path != ".Hidden" {
		t.Fatalf("ignored-folder path = %q, want %q", got.Findings[0].Path, ".Hidden")
	}
	for i, finding := range got.Findings {
		if finding.Message == "" {
			t.Fatalf("findings[%d] = %+v, want the scanner's explanation", i, finding)
		}
	}
	if !got.Installable {
		t.Fatal("package installable = false, want true")
	}
}

// findingKinds lists the findings' kinds, in order.
func findingKinds(preview Preview) []FindingKind {
	kinds := make([]FindingKind, 0, len(preview.Findings))
	for _, finding := range preview.Findings {
		kinds = append(kinds, finding.Kind)
	}
	return kinds
}

// TestInspectHiddenManifestProbeBounds pins the probe's two bounds and its
// case sensitivity: a manifest eight levels below the folder is still found and
// one nine levels down is out of reach, a manifest behind a thousand examined
// entries is out of budget, and a mis-cased MANIFEST.JSON is only a manifest
// under the option.
func TestInspectHiddenManifestProbeBounds(t *testing.T) {
	t.Run("within the depth bound", func(t *testing.T) {
		tree := manifest.NewFixture().
			File("Packed/Mod.dll", "MZ").
			File(nestedPath("Packed", 8)+"/manifest.json", rootManifestJSON("Deep", "Deep.Mod")).
			FS()
		hidden := findingsOf(Inspect(tree, Options{}), FindingHiddenManifests)
		if len(hidden) != 1 {
			t.Fatalf("hidden-manifests findings = %+v, want one eight levels down", hidden)
		}
		if want := nestedPath("Packed", 8) + "/manifest.json"; hidden[0].Path != want {
			t.Fatalf("hidden-manifests path = %q, want %q", hidden[0].Path, want)
		}
	})

	t.Run("beyond the depth bound", func(t *testing.T) {
		tree := manifest.NewFixture().
			File("Packed/Mod.dll", "MZ").
			File(nestedPath("Packed", 9)+"/manifest.json", rootManifestJSON("Deep", "Deep.Mod")).
			FS()
		got := Inspect(tree, Options{})
		if len(got.Units) != 0 {
			t.Fatalf("units = %+v, want none", got.Units)
		}
		if len(got.Findings) != 0 {
			t.Fatalf("findings = %+v, want none: the probe stops at its depth bound", got.Findings)
		}
	})

	t.Run("beyond the entry bound", func(t *testing.T) {
		fixture := manifest.NewFixture().File("Packed/Mod.dll", "MZ")
		// The walk charges one entry per directory entry it examines, files
		// included, so a thousand junk files spend its whole budget before it
		// reaches the folder that hides the manifest — which sorts last.
		for i := 0; i < walkEntryLimit; i++ {
			fixture.File(fmt.Sprintf("Packed/a%04d.txt", i), "x")
		}
		fixture.File("Packed/zzz/manifest.json", rootManifestJSON("Ghost", "Ghost.Mod"))
		got := Inspect(fixture.FS(), Options{})
		if len(got.Units) != 0 {
			t.Fatalf("units = %+v, want none", got.Units)
		}
		if len(got.Findings) != 0 {
			t.Fatalf("findings = %+v, want none: the probe stops at its entry budget", got.Findings)
		}
	})

	t.Run("case-insensitive under the option", func(t *testing.T) {
		tree := manifest.NewFixture().
			File("Packed/Mod.dll", "MZ").
			File("Packed/ModA/MANIFEST.JSON", rootManifestJSON("Hidden", "Hidden.Mod")).
			FS()
		if got := Inspect(tree, Options{}); len(got.Findings) != 0 {
			t.Fatalf("findings = %+v, want none: MANIFEST.JSON is not manifest.json by default", got.Findings)
		}
		hidden := findingsOf(Inspect(tree, Options{CaseInsensitivePaths: true}), FindingHiddenManifests)
		if len(hidden) != 1 || hidden[0].Path != "Packed/ModA/MANIFEST.JSON" {
			t.Fatalf("hidden-manifests findings = %+v, want one at Packed/ModA/MANIFEST.JSON", hidden)
		}
	})
}

// nestedPath builds a folder path depth levels below dir, named L1, L2, … so a
// depth-bound test states the level it means.
func nestedPath(dir string, depth int) string {
	for level := 1; level <= depth; level++ {
		dir = path.Join(dir, fmt.Sprintf("L%d", level))
	}
	return dir
}

// failReadDirFS fails one folder's listing while every other read succeeds.
// fstest.MapFS cannot fail a read, and an unreadable folder is a state the
// preview must report rather than swallow.
type failReadDirFS struct {
	fs.FS
	folder string
}

// ReadDir implements fs.ReadDirFS: the named folder fails, everything else is
// the tree's own listing.
func (f failReadDirFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == f.folder {
		return nil, fs.ErrPermission
	}
	return fs.ReadDir(f.FS, name)
}

// TestInspectUnreadableFolderIsAFinding pins the rule that nothing about a tree
// is fatal: the folder that cannot be read is one finding, the rest of the
// package still previews, and nothing is classified from a listing that failed.
func TestInspectUnreadableFolderIsAFinding(t *testing.T) {
	tree := manifest.NewFixture().
		Mod("GoodMod", "Good.Mod").
		MissingManifest("BadFolder").
		FS()
	got := Inspect(failReadDirFS{FS: tree, folder: "BadFolder"}, Options{})
	if len(got.Units) != 1 || got.Units[0].RelativePath != "GoodMod" {
		t.Fatalf("units = %+v, want only GoodMod", got.Units)
	}
	unreadable := findingsOf(got, FindingUnreadable)
	if len(unreadable) != 1 {
		t.Fatalf("unreadable findings = %+v, want one", unreadable)
	}
	if unreadable[0].Path != "BadFolder" {
		t.Fatalf("unreadable path = %q, want %q", unreadable[0].Path, "BadFolder")
	}
	if unreadable[0].Message == "" {
		t.Fatal("unreadable message is empty, want the filesystem's reason")
	}
	// Only a scan-limit finding blocks installability: an unreadable folder is
	// unknown, not evidence against the unit that did read.
	if !got.Installable {
		t.Fatal("package installable = false, want true: the unit installs")
	}
}
