// This file holds the tests for the package-level layouts: the four PRD §15
// shapes, the stage root's own manifest, the loose-root-files record it
// displaces, and the manifest detail a unit carries.
package inspect

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Zendevve/astradew/internal/manifest"
)

// unitPaths lists the units' relative paths, in order: most layout assertions
// are about which folders the preview found, not about every field on them.
func unitPaths(preview Preview) []string {
	paths := make([]string, 0, len(preview.Units))
	for _, unit := range preview.Units {
		paths = append(paths, unit.RelativePath)
	}
	return paths
}

// folderNames lists the units' folder names, in order.
func folderNames(preview Preview) []string {
	names := make([]string, 0, len(preview.Units))
	for _, unit := range preview.Units {
		names = append(names, unit.FolderName)
	}
	return names
}

// findingsOf returns the findings of one kind, in order.
func findingsOf(preview Preview, kind FindingKind) []Finding {
	found := []Finding{}
	for _, finding := range preview.Findings {
		if finding.Kind == kind {
			found = append(found, finding)
		}
	}
	return found
}

// rootManifestJSON is a minimal valid manifest for the layouts whose manifest
// sits at the stage root.
func rootManifestJSON(name, uid string) string {
	return `{"Name":"` + name + `","Author":"A","Version":"1.0.0","UniqueID":"` + uid + `","EntryDll":"Mod.dll"}`
}

// wantLegacyXnbNotice is PRD §16's display copy, spelled out here rather than
// referenced from the package: the verbatim text is a contract, and a change to
// it has to be a deliberate change to this test too.
const wantLegacyXnbNotice = "This package replaces Stardew Valley content files directly.\nAstradew does not install XNB mods automatically."

// TestInspectLayouts covers PRD §15's four archive layouts: the shapes a
// package arrives in, and the units and folder names the library will install
// from each.
func TestInspectLayouts(t *testing.T) {
	tests := []struct {
		name        string
		tree        *manifest.Fixture
		wantPaths   []string
		wantFolders []string
	}{
		{
			name:        "example A: one mod folder",
			tree:        manifest.NewFixture().Mod("SomeMod", "Some.Mod"),
			wantPaths:   []string{"SomeMod"},
			wantFolders: []string{"SomeMod"},
		},
		{
			name: "example B: the manifest sits at the root",
			tree: manifest.NewFixture().
				File("manifest.json", rootManifestJSON("RootMod", "Root.Mod")).
				File("SomeMod.dll", "MZ"),
			wantPaths:   []string{""},
			wantFolders: []string{"RootMod"},
		},
		{
			name: "example C: several mods beside a readme",
			tree: manifest.NewFixture().
				Mod("ModA", "A.Mod").
				Mod("ModB", "B.Mod").
				File("README.md", "hi"),
			wantPaths:   []string{"ModA", "ModB"},
			wantFolders: []string{"ModA", "ModB"},
		},
		{
			name: "example D: several mods inside a wrapper folder",
			tree: manifest.NewFixture().
				Mod("WrapperFolder/ModA", "A.Mod").
				Mod("WrapperFolder/ModB", "B.Mod"),
			wantPaths:   []string{"WrapperFolder/ModA", "WrapperFolder/ModB"},
			wantFolders: []string{"ModA", "ModB"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Inspect(test.tree.FS(), Options{})
			if paths := unitPaths(got); !slices.Equal(paths, test.wantPaths) {
				t.Fatalf("unit paths = %+v, want %+v", paths, test.wantPaths)
			}
			if names := folderNames(got); !slices.Equal(names, test.wantFolders) {
				t.Fatalf("folder names = %+v, want %+v", names, test.wantFolders)
			}
			for i, unit := range got.Units {
				if unit.Kind != KindCodeMod {
					t.Fatalf("units[%d].Kind = %q, want %q", i, unit.Kind, KindCodeMod)
				}
				if unit.Verdict != manifest.VerdictValid {
					t.Fatalf("units[%d].Verdict = %q, want %q", i, unit.Verdict, manifest.VerdictValid)
				}
				if !unit.Installable {
					t.Fatalf("units[%d].Installable = false, want true (note %q)", i, unit.Note)
				}
			}
			if len(got.Findings) != 0 {
				t.Fatalf("findings = %+v, want none", got.Findings)
			}
			if !got.Installable {
				t.Fatal("package installable = false, want true")
			}
		})
	}
}

// TestInspectRootManifestBecomesThePackagesUnit pins the root pass's first job:
// a manifest at the stage root is a unit whose RelativePath is "" — the stage
// root itself — named from the manifest, because a root manifest ships no
// folder name. The scanner's loose-root-files record, which would call that
// same manifest ignored, must not survive beside the unit.
func TestInspectRootManifestBecomesThePackagesUnit(t *testing.T) {
	tests := []struct {
		name       string
		uid        string
		wantSystem bool
	}{
		{name: "ordinary unit", uid: "Root.Mod"},
		{name: "bundled unit", uid: "SMAPI.ConsoleCommands", wantSystem: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tree := manifest.NewFixture().
				File("manifest.json", rootManifestJSON("RootMod", test.uid)).
				File("SomeMod.dll", "MZ").
				FS()
			got := Inspect(tree, Options{})
			if len(got.Units) != 1 {
				t.Fatalf("units = %+v, want one root unit", got.Units)
			}
			unit := got.Units[0]
			if unit.RelativePath != "" {
				t.Fatalf("RelativePath = %q, want the empty path for the stage root", unit.RelativePath)
			}
			if unit.FolderName != "RootMod" {
				t.Fatalf("FolderName = %q, want %q (the manifest's Name)", unit.FolderName, "RootMod")
			}
			if unit.Name != "RootMod" || unit.UniqueID != test.uid {
				t.Fatalf("identity = %q/%q, want RootMod/%q", unit.Name, unit.UniqueID, test.uid)
			}
			if unit.Kind != KindCodeMod || unit.Verdict != manifest.VerdictValid || !unit.Installable {
				t.Fatalf("unit = %+v, want a valid installable code mod", unit)
			}
			if unit.SystemMod != test.wantSystem {
				t.Fatalf("SystemMod = %v, want %v", unit.SystemMod, test.wantSystem)
			}
			if loose := findingsOf(got, FindingLooseRootFiles); len(loose) != 0 {
				t.Fatalf("loose-root-files findings = %+v, want none: the root unit accounts for the root manifest", loose)
			}
			if len(got.Findings) != 0 {
				t.Fatalf("findings = %+v, want none", got.Findings)
			}
			if !got.Installable {
				t.Fatal("package installable = false, want true")
			}
		})
	}
}

// TestInspectFolderNamedManifestJSONIsNotARootManifest pins the root pass's
// symmetry with the scanner: a folder called manifest.json is an ordinary
// folder, so a mod inside it stays a unit and there is no unreadable-manifest
// finding.
func TestInspectFolderNamedManifestJSONIsNotARootManifest(t *testing.T) {
	tree := manifest.NewFixture().
		Mod("manifest.json", "Odd.Mod").
		FS()
	got := Inspect(tree, Options{})
	if paths := unitPaths(got); !slices.Equal(paths, []string{"manifest.json"}) {
		t.Fatalf("unit paths = %+v, want the folder's own unit", paths)
	}
	if len(got.Findings) != 0 {
		t.Fatalf("findings = %+v, want none", got.Findings)
	}
}

// TestInspectOverOnDiskStage runs the preview over a real directory, the seam
// the archive layer actually hands it. fstest.MapFS and os.DirFS agree on the
// shapes this package reads; only one of them is how staging arrives.
func TestInspectOverOnDiskStage(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	write("Wrapper/ModA/manifest.json", rootManifestJSON("ModA", "Shared.Id"))
	write("Wrapper/ModA/Mod.dll", "MZ")
	write("Wrapper/ModB/manifest.json", rootManifestJSON("ModB", "Shared.Id"))
	write("Wrapper/ModB/Mod.dll", "MZ")
	write("Content/Characters/Abigail.xnb", "xnb")

	got := Inspect(os.DirFS(dir), Options{})
	if paths := unitPaths(got); !slices.Equal(paths, []string{"Wrapper/ModA", "Wrapper/ModB"}) {
		t.Fatalf("unit paths = %+v, want the two wrapper units", paths)
	}
	if len(got.Duplicates) != 1 || !got.Duplicates[0].LaunchBlocker {
		t.Fatalf("duplicates = %+v, want one launch-blocking group", got.Duplicates)
	}
	xnb := findingsOf(got, FindingLegacyXnb)
	if len(xnb) != 1 || xnb[0].Path != "Content/Characters" {
		t.Fatalf("legacy-xnb findings = %+v, want the Content tree's folder finding only", xnb)
	}
	if !got.Installable {
		t.Fatal("package installable = false, want true")
	}
}

// TestInspectLooseRootFilesReportedAndNeverUnits covers the archive whose mod
// files sit directly in the stage root: SMAPI never loads them, so the preview
// names them and holds no unit.
func TestInspectLooseRootFilesReportedAndNeverUnits(t *testing.T) {
	tree := manifest.NewFixture().File("Mod.dll", "MZ").FS()
	got := Inspect(tree, Options{})
	if len(got.Units) != 0 {
		t.Fatalf("units = %+v, want none", got.Units)
	}
	loose := findingsOf(got, FindingLooseRootFiles)
	if len(loose) != 1 {
		t.Fatalf("loose-root-files findings = %+v, want one", loose)
	}
	if loose[0].Path != "" {
		t.Fatalf("loose-root-files path = %q, want the stage root's empty path", loose[0].Path)
	}
	if !strings.Contains(loose[0].Message, "Mod.dll") {
		t.Fatalf("loose-root-files message = %q, want it to name Mod.dll", loose[0].Message)
	}
	if got.Installable {
		t.Fatal("package installable = true, want false: loose root files are not a unit")
	}
}

// failOpenFS fails one path's Open, the door fs.ReadFile goes through. The root
// manifest read cannot be made to fail with fstest.MapFS alone, and a manifest
// that cannot be read is a finding, never a silent "no manifest here".
type failOpenFS struct {
	fs.FS
	name string
}

// Open implements fs.FS: the named path fails, every other read is the tree's.
func (f failOpenFS) Open(name string) (fs.File, error) {
	if name == f.name {
		return nil, fs.ErrPermission
	}
	return f.FS.Open(name)
}

// TestInspectUnreadableRootManifestIsAFinding pins the root manifest's read
// failure: it is reported at the manifest's path, and because no unit accounted
// for the root's files, the loose-root-files record still stands.
func TestInspectUnreadableRootManifestIsAFinding(t *testing.T) {
	tree := manifest.NewFixture().
		File("manifest.json", rootManifestJSON("RootMod", "Root.Mod")).
		File("SomeMod.dll", "MZ").
		FS()
	got := Inspect(failOpenFS{FS: tree, name: "manifest.json"}, Options{})
	if len(got.Units) != 0 {
		t.Fatalf("units = %+v, want none", got.Units)
	}
	unreadable := findingsOf(got, FindingUnreadable)
	if len(unreadable) != 1 {
		t.Fatalf("unreadable findings = %+v, want one", unreadable)
	}
	if unreadable[0].Path != "manifest.json" {
		t.Fatalf("unreadable path = %q, want %q", unreadable[0].Path, "manifest.json")
	}
	if unreadable[0].Message == "" {
		t.Fatal("unreadable message is empty, want the filesystem's reason")
	}
	if loose := findingsOf(got, FindingLooseRootFiles); len(loose) != 1 {
		t.Fatalf("loose-root-files findings = %+v, want one: no unit accounts for the root's files", loose)
	}
	if got.Installable {
		t.Fatal("package installable = true, want false")
	}
}

// TestInspectCaseInsensitivePathsOption pins the option on both lookups it
// governs: the stage root's own manifest and the scanner's per-folder one. The
// zero value leaves a mis-cased manifest unrecognized, which is SMAPI's default
// on Windows.
func TestInspectCaseInsensitivePathsOption(t *testing.T) {
	tree := manifest.NewFixture().
		File("MANIFEST.JSON", rootManifestJSON("RootMod", "Root.Mod")).
		File("RootMod.dll", "MZ").
		File("NestedMod/MANIFEST.JSON", rootManifestJSON("NestedMod", "Nested.Mod")).
		File("NestedMod/Mod.dll", "MZ").
		FS()

	exact := Inspect(tree, Options{})
	if len(exact.Units) != 0 {
		t.Fatalf("units with the zero Options = %+v, want none: MANIFEST.JSON is not manifest.json", exact.Units)
	}
	if loose := findingsOf(exact, FindingLooseRootFiles); len(loose) != 1 {
		t.Fatalf("loose-root-files findings = %+v, want one", loose)
	}

	folded := Inspect(tree, Options{CaseInsensitivePaths: true})
	if paths := unitPaths(folded); !slices.Equal(paths, []string{"", "NestedMod"}) {
		t.Fatalf("unit paths = %+v, want the root unit then NestedMod", paths)
	}
	if names := folderNames(folded); !slices.Equal(names, []string{"RootMod", "NestedMod"}) {
		t.Fatalf("folder names = %+v, want [RootMod NestedMod]", names)
	}
	if len(folded.Findings) != 0 {
		t.Fatalf("findings = %+v, want none", folded.Findings)
	}
	if !folded.Installable {
		t.Fatal("package installable = false, want true")
	}
}

// TestInspectManifestVerdicts covers the three verdicts side by side: a partial
// unit installs carrying its warnings, a valid one installs with nothing to
// say, and an invalid one is still a unit — surfaced with what its author wrote
// and never installable.
func TestInspectManifestVerdicts(t *testing.T) {
	tree := manifest.NewFixture().
		BrokenManifest("BrokenMod", "{").
		Mod("GoodMod", "Good.Mod").
		Partial("PartialMod", "Partial.Mod").
		FS()
	got := Inspect(tree, Options{})
	if paths := unitPaths(got); !slices.Equal(paths, []string{"BrokenMod", "GoodMod", "PartialMod"}) {
		t.Fatalf("unit paths = %+v, want scan order", paths)
	}
	broken, good, partial := got.Units[0], got.Units[1], got.Units[2]

	if broken.Kind != KindInvalid {
		t.Fatalf("broken.Kind = %q, want %q", broken.Kind, KindInvalid)
	}
	if broken.Verdict != manifest.VerdictInvalid {
		t.Fatalf("broken.Verdict = %q, want %q", broken.Verdict, manifest.VerdictInvalid)
	}
	if broken.Installable {
		t.Fatal("broken.Installable = true, want false")
	}
	if !strings.Contains(broken.Note, "parsing its manifest failed") {
		t.Fatalf("broken.Note = %q, want the parser's failure reason", broken.Note)
	}
	if len(broken.FieldErrors) == 0 {
		t.Fatal("broken.FieldErrors is empty, want the syntax failure")
	}
	if broken.FieldErrors[0].Stage != manifest.StageSyntax {
		t.Fatalf("broken.FieldErrors[0].Stage = %q, want %q", broken.FieldErrors[0].Stage, manifest.StageSyntax)
	}

	if good.Verdict != manifest.VerdictValid || !good.Installable || good.Note != "" {
		t.Fatalf("good = %+v, want a valid installable unit with no note", good)
	}
	if len(good.FieldErrors) != 0 {
		t.Fatalf("good.FieldErrors = %+v, want none", good.FieldErrors)
	}

	if partial.Kind != KindCodeMod || partial.Verdict != manifest.VerdictPartial || !partial.Installable {
		t.Fatalf("partial = %+v, want an installable partial code mod", partial)
	}
	if len(partial.FieldErrors) == 0 {
		t.Fatal("partial.FieldErrors is empty, want the warned field")
	}

	if len(got.Findings) != 0 {
		t.Fatalf("findings = %+v, want none: every folder here is a unit", got.Findings)
	}
	if !got.Installable {
		t.Fatal("package installable = false, want true")
	}
}

// TestInspectMapsContentPackAndDependencies pins the manifest detail that rides
// along with a unit: the host link a pack declares, and the dependency list as
// authored — including a floor, an entry that only names its target, and an
// explicit IsRequired override.
func TestInspectMapsContentPackAndDependencies(t *testing.T) {
	tree := manifest.NewFixture().
		File("DepMod/manifest.json", `{"Name":"DepMod","Author":"A","Version":"1.0.0","UniqueID":"Dep.Id","EntryDll":"Dep.dll",`+
			`"Dependencies":[{"UniqueID":"Host.Id","MinimumVersion":"2.0.0"},{"UniqueID":"Optional.Id","IsRequired":false}]}`).
		File("DepMod/Dep.dll", "MZ").
		Pack("MyPack", "Pack.Id", "Host.Id").
		FS()
	got := Inspect(tree, Options{})
	if paths := unitPaths(got); !slices.Equal(paths, []string{"DepMod", "MyPack"}) {
		t.Fatalf("unit paths = %+v, want [DepMod MyPack]", paths)
	}

	dep, pack := got.Units[0], got.Units[1]
	if dep.Kind != KindCodeMod || dep.EntryDll != "Dep.dll" || dep.ContentPackFor != "" {
		t.Fatalf("dep = %+v, want a code mod with its entry assembly and no host", dep)
	}
	want := []Dependency{
		{UniqueID: "Host.Id", MinimumVersion: "2.0.0", IsRequired: true},
		{UniqueID: "Optional.Id", IsRequired: false},
	}
	if !slices.Equal(dep.Dependencies, want) {
		t.Fatalf("dependencies = %+v, want %+v", dep.Dependencies, want)
	}

	if pack.Kind != KindContentPack {
		t.Fatalf("pack.Kind = %q, want %q", pack.Kind, KindContentPack)
	}
	if pack.ContentPackFor != "Host.Id" {
		t.Fatalf("pack.ContentPackFor = %q, want %q", pack.ContentPackFor, "Host.Id")
	}
	if pack.EntryDll != "" {
		t.Fatalf("pack.EntryDll = %q, want none", pack.EntryDll)
	}
	if len(pack.Dependencies) != 0 {
		t.Fatalf("pack.Dependencies = %+v, want none", pack.Dependencies)
	}
	if !got.Installable {
		t.Fatal("package installable = false, want true")
	}
}
