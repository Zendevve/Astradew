package manifest

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// codeMod is a minimal valid code-mod manifest body.
func codeMod(uid string) string {
	return `{"Name":"` + uid + `","Author":"A","Version":"1.0.0","UniqueID":"` + uid + `","EntryDll":"Mod.dll"}`
}

// contentPack is a minimal valid Content Pack manifest body.
func contentPack(uid string) string {
	return `{"Name":"` + uid + `","Author":"A","Version":"1.0.0","UniqueID":"` + uid + `","ContentPackFor":{"UniqueID":"Pathoschild.ContentPatcher"}}`
}

// byPath indexes records by their root-relative path.
func byPath(records []Record) map[string]Record {
	out := make(map[string]Record, len(records))
	for _, r := range records {
		out[r.Path] = r
	}
	return out
}

// scanPaths lists the record paths in scan order, for order assertions.
func scanPaths(records []Record) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, r.Path)
	}
	return out
}

func TestScanFindsNestedUnitsAtAnyDepth(t *testing.T) {
	records := Scan(fstest.MapFS{
		"LookupAnything/manifest.json":       {Data: []byte(codeMod("Pathoschild.LookupAnything"))},
		"SVE/manifest.json":                  {Data: []byte(contentPack("FlashShifter.SVE"))},
		"Group/Sub/Deep/manifest.json":       {Data: []byte(codeMod("Deep.Mod"))},
		"Group/Other/manifest.json":          {Data: []byte(codeMod("Other.Mod"))},
		"LookupAnything/Mod.dll":             {Data: []byte("dll")},
		"Group/Sub/Deep/Mod.dll":             {Data: []byte("dll")},
		"Group/Other/Mod.dll":                {Data: []byte("dll")},
		"SVE/content.json":                   {Data: []byte("{}")},
		"Group/Sub/Deep/assets/thing.xnb":    {Data: []byte("xnb")},
		"Group/Sub/Deep/assets/thing2.json":  {Data: []byte("{}")},
		"Group/Other/config.json":            {Data: []byte("{}")},
		"Group/Sub/Deep/readme.md":           {Data: []byte("readme")},
		"Group/Sub/Deep/screenshot.PNG":      {Data: []byte("png")},
		"Group/Sub/Deep/.gitignore":          {Data: []byte("ignored")},
		"Group/Sub/Deep/notes/scratch.draft": {Data: []byte("kept")},
	}, Options{})

	got := byPath(records)
	for _, want := range []string{"LookupAnything", "SVE", "Group/Sub/Deep", "Group/Other"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("record %q missing; paths = %v", want, scanPaths(records))
		}
	}
	if len(records) != 4 {
		t.Fatalf("records = %v, want only the four units", scanPaths(records))
	}
	if got["Group/Sub/Deep"].Outcome != OutcomeSmapi {
		t.Fatalf("nested unit outcome = %q, want smapi", got["Group/Sub/Deep"].Outcome)
	}
	if got["SVE"].Outcome != OutcomeContentPack {
		t.Fatalf("content pack outcome = %q, want content-pack", got["SVE"].Outcome)
	}
	if got["SVE"].Unit == nil || got["SVE"].Unit.Manifest.ContentPackFor.UniqueID != "Pathoschild.ContentPatcher" {
		t.Fatalf("content pack host = %+v", got["SVE"].Unit)
	}
	if got["Group"].Outcome != "" {
		t.Fatalf("organizer must not be reported as a record: %+v", got["Group"])
	}
}

func TestScanOrganizerWithFilesPoisonsItsSubtree(t *testing.T) {
	records := Scan(fstest.MapFS{
		"OldMods/notes.json":                   {Data: []byte("{}")},
		"OldMods/ContentPatcher/manifest.json": {Data: []byte(codeMod("Pathoschild.ContentPatcher"))},
	}, Options{})

	if len(records) != 1 {
		t.Fatalf("records = %v, want exactly the poisoned organizer", scanPaths(records))
	}
	got := records[0]
	if got.Path != "OldMods" || got.Outcome != OutcomeInvalid || got.Reason != ReasonManifestMissing {
		t.Fatalf("poisoned organizer = %+v, want OldMods invalid/manifest-missing", got)
	}
	if got.Note != "it contains files, but none of them are manifest.json." {
		t.Fatalf("note = %q, want SMAPI's manifest-missing wording", got.Note)
	}
}

func TestScanRootLooseFilesReportedIgnoredNeverUnits(t *testing.T) {
	records := Scan(fstest.MapFS{
		"SomeMod.dll":        {Data: []byte("dll")},
		"README.md":          {Data: []byte("readme")},
		"Good/manifest.json": {Data: []byte(codeMod("Good.Mod"))},
	}, Options{})

	got := byPath(records)
	loose, ok := got[""]
	if !ok {
		t.Fatalf("no root record; paths = %v", scanPaths(records))
	}
	if loose.Outcome != OutcomeIgnored || loose.Reason != ReasonLooseRootFiles {
		t.Fatalf("root record = %+v, want ignored/loose-root-files", loose)
	}
	if !strings.Contains(loose.Note, "Each mod must have its own subfolder instead.") {
		t.Fatalf("note = %q, want SMAPI's own-subfolder wording", loose.Note)
	}
	if len(loose.Files) != 2 || loose.Files[0] != "README.md" || loose.Files[1] != "SomeMod.dll" {
		t.Fatalf("files = %v, want the loose names sorted case-insensitively", loose.Files)
	}
	if len(records) != 2 {
		t.Fatalf("records = %v, want the root report plus the one unit", scanPaths(records))
	}
	if loose.Unit != nil {
		t.Fatal("a loose root file must never become a unit")
	}
}

func TestScanRootWithoutModLookingLooseFilesReportsNothing(t *testing.T) {
	records := Scan(fstest.MapFS{
		"notes.json":         {Data: []byte("{}")},
		"Good/manifest.json": {Data: []byte(codeMod("Good.Mod"))},
	}, Options{})
	if len(records) != 1 || records[0].Path != "Good" {
		t.Fatalf("records = %v, want only the unit", scanPaths(records))
	}
}

func TestScanDotFoldersIgnoredWithPrunedSubtrees(t *testing.T) {
	records := Scan(fstest.MapFS{
		".OldMods/manifest.json":        {Data: []byte(codeMod("Old.Mod"))},
		"Group/.disabled/manifest.json": {Data: []byte(codeMod("Disabled.Mod"))},
		"Group/Live/manifest.json":      {Data: []byte(codeMod("Live.Mod"))},
	}, Options{})

	got := byPath(records)
	if len(records) != 3 || strings.Join(scanPaths(records), ",") != ".OldMods,Group/.disabled,Group/Live" {
		t.Fatalf("records = %v, want the two dot reports plus the live unit", scanPaths(records))
	}
	dot, ok := got[".OldMods"]
	if !ok {
		t.Fatalf(".OldMods not reported; paths = %v", scanPaths(records))
	}
	if dot.Outcome != OutcomeIgnored || dot.Reason != ReasonIgnoredFolder {
		t.Fatalf("dot record = %+v, want ignored/ignored-folder", dot)
	}
	if dot.Note != "ignored folder because its name starts with a dot." {
		t.Fatalf("note = %q, want SMAPI's dot-folder wording", dot.Note)
	}
	if dot.Unit != nil {
		t.Fatal("a dot folder's manifest must stay invisible")
	}
	if _, ok := got["Group/.disabled"]; !ok {
		t.Fatalf("nested dot folder not reported; paths = %v", scanPaths(records))
	}
	if _, ok := got["Group/Live"]; !ok {
		t.Fatalf("sibling unit lost; paths = %v", scanPaths(records))
	}
}

func TestScanMetadataPrunedSilently(t *testing.T) {
	records := Scan(fstest.MapFS{
		"__MACOSX/Mod/manifest.json": {Data: []byte(codeMod("Mac.Mod"))},
		"mcs/Mod/manifest.json":      {Data: []byte(codeMod("Mcs.Mod"))},
		"Good/manifest.json":         {Data: []byte(codeMod("Good.Mod"))},
		"Good/desktop.ini":           {Data: []byte("ini")},
		"Good/Thumbs.db":             {Data: []byte("db")},
		"Good/.DS_Store":             {Data: []byte("ds")},
		"Good/._resource":            {Data: []byte("resource")},
	}, Options{})

	if len(records) != 1 || records[0].Path != "Good" {
		t.Fatalf("records = %v, want only Good", scanPaths(records))
	}
}

func TestScanIgnoredExtensionsDecideEmptyVsMissing(t *testing.T) {
	records := Scan(fstest.MapFS{
		"OnlyDocs/readme.md":   {Data: []byte("readme")},
		"OnlyDocs/shot.png":    {Data: []byte("png")},
		"OnlyDocs/notes.txt":   {Data: []byte("notes")},
		"OnlyDocs/archive.zip": {Data: []byte("zip")},
		"Really/x.dll":         {Data: []byte("dll")},
	}, Options{})

	got := byPath(records)
	if r := got["OnlyDocs"]; r.Outcome != OutcomeInvalid || r.Reason != ReasonEmptyFolder {
		t.Fatalf("OnlyDocs = %+v, want invalid/empty-folder", r)
	} else if r.Note != "it's an empty folder." {
		t.Fatalf("note = %q, want SMAPI's empty wording", r.Note)
	}
	if r := got["Really"]; r.Outcome != OutcomeInvalid || r.Reason != ReasonManifestMissing {
		t.Fatalf("Really = %+v, want invalid/manifest-missing", r)
	}
}

func TestScanIrrelevantFilesDoNotPoisonOrganizers(t *testing.T) {
	records := Scan(fstest.MapFS{
		"Group/readme.md":         {Data: []byte("readme")},
		"Group/.DS_Store":         {Data: []byte("ds")},
		"Group/Mod/manifest.json": {Data: []byte(codeMod("Group.Mod"))},
	}, Options{})

	if len(records) != 1 || records[0].Path != "Group/Mod" {
		t.Fatalf("records = %v, want the nested unit only", scanPaths(records))
	}
	if records[0].Outcome != OutcomeSmapi {
		t.Fatalf("unit = %+v, want smapi", records[0])
	}
}

func TestScanConsolidatesChildrenPerSmapisRules(t *testing.T) {
	allEmpty := Scan(fstest.MapFS{
		"Group/A": &fstest.MapFile{Mode: fs.ModeDir},
		"Group/B": &fstest.MapFile{Mode: fs.ModeDir},
	}, Options{})
	if len(allEmpty) != 1 {
		t.Fatalf("all-empty records = %v, want one merged parent record", scanPaths(allEmpty))
	}
	if r := allEmpty[0]; r.Path != "Group" || r.Outcome != OutcomeInvalid || r.Reason != ReasonEmptyFolder {
		t.Fatalf("all-empty merge = %+v, want Group invalid/empty-folder", r)
	}

	xnbMixed := Scan(fstest.MapFS{
		"Group/A/thing.xnb": {Data: []byte("xnb")},
		"Group/B":           &fstest.MapFile{Mode: fs.ModeDir},
	}, Options{})
	if len(xnbMixed) != 1 {
		t.Fatalf("xnb records = %v, want one merged parent record", scanPaths(xnbMixed))
	}
	if r := xnbMixed[0]; r.Path != "Group" || r.Outcome != OutcomeXnb || r.Reason != ReasonXnbMod {
		t.Fatalf("xnb merge = %+v, want Group xnb/xnb-mod", r)
	}
	if !strings.Contains(xnbMixed[0].Note, "smapi.io/xnb") {
		t.Fatalf("note = %q, want the XNB explanation", xnbMixed[0].Note)
	}

	live := Scan(fstest.MapFS{
		"Group/A/manifest.json": {Data: []byte(codeMod("A.Mod"))},
		"Group/B/manifest.json": {Data: []byte(codeMod("B.Mod"))},
	}, Options{})
	if len(live) != 2 {
		t.Fatalf("live records = %v, want both children kept", scanPaths(live))
	}

	single := Scan(fstest.MapFS{
		"Group/Only": &fstest.MapFile{Mode: fs.ModeDir},
	}, Options{})
	if len(single) != 1 || single[0].Path != "Group/Only" {
		t.Fatalf("single-child records = %v, want the child, not the parent", scanPaths(single))
	}
}

func TestScanXnbVersusManifestPrecedence(t *testing.T) {
	records := Scan(fstest.MapFS{
		"HasManifest/manifest.json": {Data: []byte(codeMod("Pack.Mod"))},
		"HasManifest/thing.xnb":     {Data: []byte("xnb")},
		"Legacy/thing.xnb":          {Data: []byte("xnb")},
		"Legacy/polygon.json":       {Data: []byte("{}")},
		"NotXnb/thing.xnb":          {Data: []byte("xnb")},
		"NotXnb/Mod.dll":            {Data: []byte("dll")},
	}, Options{})

	got := byPath(records)
	if r := got["HasManifest"]; r.Outcome != OutcomeSmapi {
		t.Fatalf("manifest + xnb = %+v, want a normal unit", r)
	}
	if r := got["Legacy"]; r.Outcome != OutcomeXnb || r.Reason != ReasonXnbMod {
		t.Fatalf("legacy xnb drop = %+v, want xnb/xnb-mod", r)
	}
	if r := got["NotXnb"]; r.Outcome != OutcomeInvalid || r.Reason != ReasonManifestMissing {
		t.Fatalf("xnb beside a dll = %+v, want invalid/manifest-missing", r)
	}
}

func TestScanInstallerBundleReportsGuidance(t *testing.T) {
	records := Scan(fstest.MapFS{
		"SMAPI 4.5.2/install on Windows.bat": {Data: []byte("bat")},
		"SMAPI 4.5.2/internal/install.dat":   {Data: []byte("dat")},
		"SMAPI 4.5.2/StardewModdingAPI.exe":  {Data: []byte("exe")},
	}, Options{})

	if len(records) != 1 {
		t.Fatalf("records = %v, want one installer record", scanPaths(records))
	}
	r := records[0]
	if r.Outcome != OutcomeInvalid || r.Reason != ReasonInstaller {
		t.Fatalf("installer record = %+v, want invalid/smapi-installer", r)
	}
	if r.Note != "the SMAPI installer isn't a mod (you can delete this folder after running the installer file)." {
		t.Fatalf("note = %q, want SMAPI's installer guidance", r.Note)
	}
}

func TestScanVortexEmptyFolder(t *testing.T) {
	records := Scan(fstest.MapFS{
		"Vortexed/__folder_managed_by_vortex":           {Data: []byte("marker")},
		"VortexedWithConfig/__folder_managed_by_vortex": {Data: []byte("marker")},
		"VortexedWithConfig/config.json":                {Data: []byte("{}")},
	}, Options{})

	got := byPath(records)
	for _, p := range []string{"Vortexed", "VortexedWithConfig"} {
		r, ok := got[p]
		if !ok {
			t.Fatalf("%s not reported; paths = %v", p, scanPaths(records))
		}
		if r.Outcome != OutcomeInvalid || r.Reason != ReasonEmptyVortexFolder {
			t.Fatalf("%s = %+v, want invalid/empty-vortex-folder", p, r)
		}
	}
}

func TestScanManifestVerdicts(t *testing.T) {
	records := Scan(fstest.MapFS{
		"Partial/manifest.json": {Data: []byte(`{"Name":"P","Version":"1.0.0","UniqueID":"A.P","EntryDll":"P.dll","UpdateKeys":"oops"}`)},
		"Broken/manifest.json":  {Data: []byte(`{"Name":"B","Version":"1.0.0"}`)},
		"NotJSON/manifest.json": {Data: []byte(`not json`)},
	}, Options{})

	got := byPath(records)
	partial := got["Partial"]
	if partial.Outcome != OutcomeSmapi || partial.Unit == nil || partial.Unit.Verdict != VerdictPartial {
		t.Fatalf("partial = %+v, want a kept unit with a partial verdict", partial)
	}
	if len(partial.Unit.Errors) == 0 {
		t.Fatal("partial unit carries no warnings")
	}
	for _, p := range []string{"Broken", "NotJSON"} {
		r, ok := got[p]
		if !ok {
			t.Fatalf("%s not reported; paths = %v", p, scanPaths(records))
		}
		if r.Outcome != OutcomeInvalid || r.Reason != ReasonManifestInvalid {
			t.Fatalf("%s = %+v, want invalid/manifest-invalid", p, r)
		}
		if r.Unit == nil || !strings.Contains(r.Note, "parsing its manifest failed") {
			t.Fatalf("%s = %+v, want the parse failure recorded with its detail", p, r)
		}
	}
}

func TestScanManifestCaseSensitivityOption(t *testing.T) {
	files := fstest.MapFS{
		"Odd/manifest.json":   {Data: []byte(codeMod("Odd.Mod"))},
		"Shout/MANIFEST.JSON": {Data: []byte(codeMod("Shout.Mod"))},
	}
	strict := byPath(Scan(files, Options{}))
	if r := strict["Shout"]; r.Outcome != OutcomeInvalid || r.Reason != ReasonManifestMissing {
		t.Fatalf("case-sensitive scan = %+v, want manifest-missing", r)
	}

	lenient := byPath(Scan(files, Options{CaseInsensitivePaths: true}))
	if r := lenient["Shout"]; r.Outcome != OutcomeSmapi {
		t.Fatalf("case-insensitive scan = %+v, want the unit found", r)
	}
}

func TestScanEmptyAndMissingRootIsHonestEmpty(t *testing.T) {
	records := Scan(fstest.MapFS{}, Options{})
	if records == nil || len(records) != 0 {
		t.Fatalf("empty root = %v, want a non-nil empty result", records)
	}
	missing := Scan(missingRootFS{}, Options{})
	if len(missing) != 0 {
		t.Fatalf("missing root = %v, want an empty result and no error", scanPaths(missing))
	}
}

func TestScanUnreadableFolderIsReportedNotFatal(t *testing.T) {
	files := unreadableFS{MapFS: fstest.MapFS{
		"Locked/manifest.json": {Data: []byte(codeMod("Locked.Mod"))},
		"Good/manifest.json":   {Data: []byte(codeMod("Good.Mod"))},
	}}
	records := Scan(files, Options{})
	got := byPath(records)
	if r := got["Locked"]; r.Outcome != OutcomeInvalid || r.Reason != ReasonUnreadable {
		t.Fatalf("locked = %+v, want invalid/unreadable", r)
	}
	if r, ok := got["Good"]; !ok || r.Outcome != OutcomeSmapi {
		t.Fatalf("one bad folder spoiled the scan: %v", scanPaths(records))
	}
}

func TestScanLinkLoopReportedIgnored(t *testing.T) {
	files := loopFS{
		MapFS: fstest.MapFS{
			"Group/Live/manifest.json": {Data: []byte(codeMod("Live.Mod"))},
		},
		links: map[string]string{"Group/loop": "Group"},
	}
	records := Scan(files, Options{})
	if len(records) != 2 {
		t.Fatalf("records = %v, want the unit plus one loop report", scanPaths(records))
	}
	got := byPath(records)
	if r := got["Group/Live"]; r.Outcome != OutcomeSmapi {
		t.Fatalf("linked tree lost its unit: %+v", r)
	}
	loop, ok := got["Group/loop"]
	if !ok {
		t.Fatalf("loop not reported; paths = %v", scanPaths(records))
	}
	if loop.Outcome != OutcomeIgnored || loop.Reason != ReasonLinkLoop {
		t.Fatalf("loop record = %+v, want ignored/link-loop", loop)
	}
}

func TestScanLinkLoopInsideLeafFileWalkTerminates(t *testing.T) {
	files := loopFS{
		MapFS: fstest.MapFS{
			"Leaf/notes.json":     {Data: []byte("{}")},
			"Leaf/sub/other.json": {Data: []byte("{}")},
		},
		links: map[string]string{"Leaf/sub/loop": "Leaf"},
	}
	records := Scan(files, Options{})
	if len(records) != 1 {
		t.Fatalf("records = %v, want one record", scanPaths(records))
	}
	if r := records[0]; r.Path != "Leaf" || r.Outcome != OutcomeInvalid || r.Reason != ReasonManifestMissing {
		t.Fatalf("leaf = %+v, want invalid/manifest-missing", r)
	}
}

func TestScanManifestFailureNoteNamesTheFailure(t *testing.T) {
	// A warning (malformed UpdateKeys) accompanies a failure (no UniqueID);
	// the note must name the failure, not whichever error came first.
	records := Scan(fstest.MapFS{
		"Mixed/manifest.json": {Data: []byte(`{"Name":"N","Version":"1.0.0","EntryDll":"A.dll","UpdateKeys":"oops"}`)},
	}, Options{})
	if len(records) != 1 {
		t.Fatalf("records = %v, want one", scanPaths(records))
	}
	r := records[0]
	if r.Outcome != OutcomeInvalid || r.Reason != ReasonManifestInvalid {
		t.Fatalf("record = %+v, want invalid/manifest-invalid", r)
	}
	if !strings.Contains(r.Note, "missing required fields") {
		t.Fatalf("note = %q, want the failure named", r.Note)
	}
	if strings.Contains(r.Note, "UpdateKeys") {
		t.Fatalf("note = %q, want the failure, not the accompanying warning", r.Note)
	}
}

// TestScanRealDirectoryWithLinkedFolder anchors the in-memory suite on a real
// filesystem once: directory links exist only there, and a plain fs.FS reports
// them as link entries, which must still be walked like real folders.
func TestScanRealDirectoryWithLinkedFolder(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "Real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatalf("mkdir real: %v", err)
	}
	if err := os.WriteFile(filepath.Join(real, "manifest.json"), []byte(codeMod("Real.Mod")), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	group := filepath.Join(root, "Group")
	if err := os.MkdirAll(group, 0o755); err != nil {
		t.Fatalf("mkdir group: %v", err)
	}
	if err := makeDirLink(real, filepath.Join(root, "Linked")); err != nil {
		t.Skipf("directory links unavailable on this system: %v", err)
	}
	if err := makeDirLink(real, filepath.Join(group, "LinkInGroup")); err != nil {
		t.Skipf("directory links unavailable on this system: %v", err)
	}

	records := Scan(os.DirFS(root), Options{})
	got := byPath(records)
	if r, ok := got["Real"]; !ok || r.Outcome != OutcomeSmapi {
		t.Fatalf("real folder lost: %v", scanPaths(records))
	}
	if r, ok := got["Linked"]; !ok || r.Outcome != OutcomeSmapi {
		t.Fatalf("linked mod folder must scan like a real one: %v", scanPaths(records))
	}
	if r, ok := got["Group/LinkInGroup"]; !ok || r.Outcome != OutcomeSmapi {
		t.Fatalf("a linked folder inside an organizer must scan like a real one: %v", scanPaths(records))
	}
}

// makeDirLink creates a directory link. Windows falls back to a junction
// because plain symlinks need a privilege a test run may not hold.
func makeDirLink(target, link string) error {
	symlinkErr := os.Symlink(target, link)
	if symlinkErr == nil {
		return nil
	}
	if runtime.GOOS != "windows" {
		return symlinkErr
	}
	if err := exec.Command("cmd", "/c", "mklink", "/J", link, target).Run(); err != nil {
		return fmt.Errorf("symlink: %v; junction: %w", symlinkErr, err)
	}
	return nil
}

// missingRootFS models a mods root that isn't there (deleted or never created).
type missingRootFS struct{}

func (missingRootFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }

// unreadableFS models a tree with a permission-denied folder.
type unreadableFS struct{ fstest.MapFS }

func (u unreadableFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "Locked" {
		return nil, fs.ErrPermission
	}
	return fs.ReadDir(u.MapFS, name)
}

// loopFS models directory links: links maps an alias path onto its target, and
// Canonical reports the target, which a plain fs.FS cannot express.
type loopFS struct {
	fstest.MapFS
	links map[string]string
}

func (l loopFS) resolve(name string) string {
	for alias, target := range l.links {
		if name == alias {
			return target
		}
		if strings.HasPrefix(name, alias+"/") {
			return path.Join(target, strings.TrimPrefix(name, alias+"/"))
		}
	}
	return name
}

func (l loopFS) Open(name string) (fs.File, error) { return l.MapFS.Open(l.resolve(name)) }

// ReadDir lists the resolved folder plus one synthetic entry per link alias
// that sits inside it, so traversal sees the link the way a real filesystem
// would. Entries stay name-sorted, matching fs.ReadDir otherwise.
func (l loopFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(l.MapFS, l.resolve(name))
	if err != nil {
		return nil, err
	}
	for alias := range l.links {
		if path.Dir(alias) == name {
			entries = append(entries, linkEntry{path.Base(alias)})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

func (l loopFS) ReadFile(name string) ([]byte, error)  { return fs.ReadFile(l.MapFS, l.resolve(name)) }
func (l loopFS) Canonical(name string) (string, error) { return l.resolve(name), nil }

// Stat resolves aliases the way a real filesystem follows a link, which is how
// the scanner learns that a link entry is really a folder.
func (l loopFS) Stat(name string) (fs.FileInfo, error) { return fs.Stat(l.MapFS, l.resolve(name)) }

// linkEntry is the directory entry a link presents on a real filesystem: a
// link, not a folder, even when its target is one.
type linkEntry struct{ name string }

func (e linkEntry) Name() string    { return e.name }
func (linkEntry) IsDir() bool       { return false }
func (linkEntry) Type() fs.FileMode { return fs.ModeSymlink }

// Info is unused: the scanner resolves a link entry through fs.Stat on the
// filesystem, exactly as it must for a bridge that can't describe the target.
func (e linkEntry) Info() (fs.FileInfo, error) {
	return nil, fs.ErrInvalid
}
