// This file holds the read-only directory scanner: in-memory, rooted at one
// Mods directory, over an fs.FS seam that has no write methods. It replicates
// SMAPI's ModScanner traversal and ignore tables (develop branch) so a scan
// agrees with the folders SMAPI itself would load, with these documented
// deviations:
//
//   - a visited-set guard instead of SMAPI's unguarded recursion into linked
//     folders, so a cycle is one ignored record rather than an endless walk;
//   - deterministic order, because fs.ReadDir sorts and SMAPI uses raw
//     enumeration order (its merged-record text depends on that order);
//   - one reason and note per merged record, instead of copying the first
//     child's text onto a differently-classified parent;
//   - a manifest whose verdict is invalid is reported as manifest-invalid,
//     where SMAPI's scanner would still label its type and fail the mod later
//     during load-time validation;
//   - failure notes carry the parser's first failure message, not SMAPI's
//     exception dump;
//   - loose root files, an unreadable folder or manifest, and the SMAPI
//     installer are reported as records with their own reasons, where SMAPI
//     logs prose, throws, or reuses ManifestMissing respectively.
//
// Parity is deliberate elsewhere: directory links are followed like real
// folders (SMAPI gets that from DirectoryInfo; a plain fs.FS needs the Stat in
// isDir), and every note string is SMAPI's own wording.
package manifest

import (
	"errors"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// Outcome is the scanner's classification of one folder, mirroring SMAPI's
// ModType.
type Outcome string

const (
	// OutcomeSmapi is a Mod Unit SMAPI loads directly.
	OutcomeSmapi Outcome = "smapi"
	// OutcomeContentPack is a Mod Unit consumed by its host mod.
	OutcomeContentPack Outcome = "content-pack"
	// OutcomeInvalid is a folder that isn't a loadable Mod Unit; Reason says why.
	OutcomeInvalid Outcome = "invalid"
	// OutcomeXnb is a legacy XNB folder SMAPI can't load.
	OutcomeXnb Outcome = "xnb"
	// OutcomeIgnored is a folder SMAPI skips by convention.
	OutcomeIgnored Outcome = "ignored"
)

// Reason is why a folder got its Outcome: SMAPI's ModParseError values plus
// the scanner's own cases.
type Reason string

const (
	// ReasonManifestMissing means the folder holds files but no manifest.json.
	ReasonManifestMissing Reason = "manifest-missing"
	// ReasonManifestInvalid means a manifest.json exists but didn't parse.
	ReasonManifestInvalid Reason = "manifest-invalid"
	// ReasonEmptyFolder means the folder has no relevant files at all.
	ReasonEmptyFolder Reason = "empty-folder"
	// ReasonEmptyVortexFolder means the folder only holds a Vortex marker.
	ReasonEmptyVortexFolder Reason = "empty-vortex-folder"
	// ReasonXnbMod means the folder is a legacy XNB mod.
	ReasonXnbMod Reason = "xnb-mod"
	// ReasonIgnoredFolder means the folder name starts with a dot.
	ReasonIgnoredFolder Reason = "ignored-folder"
	// ReasonInstaller means the folder is the SMAPI installer bundle. SMAPI
	// reports that as ManifestMissing with its own guidance text; it gets a
	// distinct reason here so callers never parse prose.
	ReasonInstaller Reason = "smapi-installer"
	// ReasonLinkLoop means the folder resolves to one already scanned: the
	// scanner's deliberate cycle guard.
	ReasonLinkLoop Reason = "link-loop"
	// ReasonLooseRootFiles means mod-looking files sit directly in the root.
	ReasonLooseRootFiles Reason = "loose-root-files"
	// ReasonUnreadable means the filesystem refused to read the folder.
	ReasonUnreadable Reason = "unreadable"
)

// SMAPI's wording for the two outcomes that can be reported either directly or
// by consolidation; kept in one place so parity text can't drift apart.
const (
	noteEmptyFolder = "it's an empty folder."
	noteXnbMod      = "it's not a SMAPI mod (see https://smapi.io/xnb for info)."
)

// Unit is one parsed Mod Unit: how its Manifest parsed, with per-field detail.
type Unit struct {
	Manifest Manifest
	Verdict  Verdict
	Errors   []FieldError
}

// Record is one folder the scan reports. A flat list of these is the whole
// result: no grouping, no ordering guarantees beyond scan order.
type Record struct {
	// Path is the folder's path relative to the scanned root, slash-separated;
	// "" is the root itself (used only for the loose-root-files record).
	Path string
	// Outcome is what the folder is.
	Outcome Outcome
	// Reason is why, for every outcome except a parsed unit.
	Reason Reason
	// Note is the human-facing explanation, mirroring SMAPI's wording.
	Note string
	// Files lists the offending file names, set only on a loose-root-files
	// record, sorted case-insensitively.
	Files []string
	// Unit is the parsed Manifest detail, nil when no manifest parsed. An
	// invalid manifest still carries its detail: nothing the author wrote is
	// dropped just because the verdict failed.
	Unit *Unit
}

// Options configures one scan.
type Options struct {
	// CaseInsensitivePaths mirrors SMAPI's UseCaseInsensitivePaths: when set,
	// a mis-cased manifest.json still identifies a Mod Unit. SMAPI defaults it
	// on for Linux and Android and off elsewhere; the zero value is off.
	CaseInsensitivePaths bool
}

// LinkResolver is implemented by filesystem bridges that can name a folder's
// canonical identity, link targets included. A plain fs.FS cannot express link
// targets (fs.ReadLinkFS exposes a link's target text, not the identity it
// resolves to), so a bridge that wants loop detection provides this; the
// scanner then keys its visited set on the canonical path and reports a loop
// as an ignored record instead of walking it.
type LinkResolver interface {
	Canonical(name string) (string, error)
}

// Scan reads a Mods directory without touching it. The filesystem must be
// rooted at the Mods folder itself (the game's Mods folder or an alternate
// mods-path root); a missing or unreadable root is an honest empty scan, never
// an error, and no single bad folder fails the whole scan.
func Scan(fsys fs.FS, opts Options) []Record {
	s := &scanner{fsys: fsys, opts: opts, visited: map[string]bool{}}
	s.resolver, _ = fsys.(LinkResolver)

	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return []Record{}
	}
	s.visited[s.canonical(".")] = true

	out := make([]Record, 0, len(entries))
	if loose := s.looseRootRecord(entries); loose != nil {
		out = append(out, *loose)
	}
	for _, entry := range entries {
		// The root always descends: it is never a mod folder of its own.
		if !s.isDir(entry, ".") || !relevantName(entry.Name()) {
			continue
		}
		out = append(out, s.scanFolder(entry.Name())...)
	}
	return out
}

type scanner struct {
	fsys     fs.FS
	opts     Options
	visited  map[string]bool
	resolver LinkResolver
}

// canonical names a folder's identity: what the bridge resolves, else the
// cleaned path. Cleaned paths alone cannot see through a link, so a bridge
// without Canonical only catches folders the filesystem maps back onto a
// path already scanned.
func (s *scanner) canonical(name string) string {
	if s.resolver != nil {
		if resolved, err := s.resolver.Canonical(name); err == nil {
			return path.Clean(resolved)
		}
	}
	return path.Clean(name)
}

// enter claims a folder for this scan, reporting false when it was seen before.
func (s *scanner) enter(name string) bool {
	canonical := s.canonical(name)
	if s.visited[canonical] {
		return false
	}
	s.visited[canonical] = true
	return true
}

// isDir reports whether an entry is a folder for traversal purposes: a real
// folder, or a link whose target is a folder. Real filesystems report a
// directory link as a link entry — ModeSymlink for a symlink, and on Windows
// ModeIrregular for a junction, both with IsDir false — while SMAPI follows
// such links transparently. Resolving them through Stat keeps linked mod
// folders visible instead of dropping them as if they were files.
func (s *scanner) isDir(entry fs.DirEntry, parent string) bool {
	if entry.IsDir() {
		return true
	}
	if entry.Type().IsRegular() {
		return false
	}
	info, err := fs.Stat(s.fsys, path.Join(parent, entry.Name()))
	return err == nil && info.IsDir()
}

// scanFolder applies SMAPI's per-folder rules in order: dot prefix (recorded
// and pruned), metadata name (pruned silently), the descend test, then the
// leaf classification.
func (s *scanner) scanFolder(folder string) []Record {
	name := path.Base(folder)
	if strings.HasPrefix(name, ".") {
		return []Record{{
			Path: folder, Outcome: OutcomeIgnored, Reason: ReasonIgnoredFolder,
			Note: "ignored folder because its name starts with a dot.",
		}}
	}
	if !relevantName(name) {
		return nil
	}
	if !s.enter(folder) {
		return []Record{{
			Path: folder, Outcome: OutcomeIgnored, Reason: ReasonLinkLoop,
			Note: "ignored because it links back to a folder already scanned.",
		}}
	}
	entries, err := fs.ReadDir(s.fsys, folder)
	if err != nil {
		return []Record{{
			Path: folder, Outcome: OutcomeInvalid, Reason: ReasonUnreadable,
			Note: "the folder couldn't be read: " + err.Error(),
		}}
	}

	subfolders, files := 0, 0
	for _, entry := range entries {
		if s.isDir(entry, folder) {
			// A dot folder still counts here: SMAPI's name filter doesn't
			// test the dot prefix, the traversal does.
			if relevantName(entry.Name()) {
				subfolders++
			}
			continue
		}
		if relevantFile(entry.Name()) {
			files++
		}
	}

	// All-subfolder organizers recurse; anything else is a mod candidate.
	if subfolders > 0 && files == 0 {
		var children []Record
		for _, entry := range entries {
			if !s.isDir(entry, folder) || !relevantName(entry.Name()) {
				continue
			}
			children = append(children, s.scanFolder(path.Join(folder, entry.Name()))...)
		}
		return consolidate(folder, children)
	}
	return []Record{s.readFolder(folder, entries)}
}

// readFolder classifies one mod candidate: manifest first, then SMAPI's
// manifest-less taxonomy in its exact precedence.
func (s *scanner) readFolder(folder string, entries []fs.DirEntry) Record {
	data, err := s.readManifest(folder, entries)
	switch {
	case err == nil:
		return classify(folder, data)
	case !errors.Is(err, fs.ErrNotExist):
		return Record{
			Path: folder, Outcome: OutcomeInvalid, Reason: ReasonUnreadable,
			Note: "its manifest couldn't be read: " + err.Error(),
		}
	}

	files := s.collectFiles(folder)
	if vortexEmpty(files) {
		return Record{
			Path: folder, Outcome: OutcomeInvalid, Reason: ReasonEmptyVortexFolder,
			Note: "it's an empty Vortex folder (is the mod disabled in Vortex?).",
		}
	}
	relevant := make([]string, 0, len(files))
	for _, name := range files {
		if relevantFile(name) {
			relevant = append(relevant, name)
		}
	}
	switch {
	case len(relevant) == 0:
		return Record{
			Path: folder, Outcome: OutcomeInvalid, Reason: ReasonEmptyFolder,
			Note: noteEmptyFolder,
		}
	case xnbMod(relevant):
		return Record{
			Path: folder, Outcome: OutcomeXnb, Reason: ReasonXnbMod,
			Note: noteXnbMod,
		}
	case hasInstallerScript(relevant):
		return Record{
			Path: folder, Outcome: OutcomeInvalid, Reason: ReasonInstaller,
			Note: "the SMAPI installer isn't a mod (you can delete this folder after running the installer file).",
		}
	default:
		return Record{
			Path: folder, Outcome: OutcomeInvalid, Reason: ReasonManifestMissing,
			Note: "it contains files, but none of them are manifest.json.",
		}
	}
}

// readManifest reads the folder's own manifest.json, top level only. The
// conventional name wins when several spellings exist; the case-insensitive
// fallback follows the option, and picks the first match in sorted order
// (SMAPI's own pick is enumeration-order dependent, so it is unspecified).
func (s *scanner) readManifest(folder string, entries []fs.DirEntry) ([]byte, error) {
	const conventional = "manifest.json"
	data, err := fs.ReadFile(s.fsys, path.Join(folder, conventional))
	if err == nil || !errors.Is(err, fs.ErrNotExist) || !s.opts.CaseInsensitivePaths {
		return data, err
	}
	for _, entry := range entries {
		if !s.isDir(entry, folder) && strings.EqualFold(entry.Name(), conventional) {
			return fs.ReadFile(s.fsys, path.Join(folder, entry.Name()))
		}
	}
	return nil, fs.ErrNotExist
}

// classify turns manifest bytes into a unit record. A manifest that fails
// validation still reports its parsed detail alongside the failure.
func classify(folder string, data []byte) Record {
	m, verdict, errs := Parse(data)
	unit := &Unit{Manifest: m, Verdict: verdict, Errors: errs}
	if verdict == VerdictInvalid {
		// Parse appends failures after warnings and never returns an invalid
		// verdict without one, so the last error names the failure rather than
		// a warning that merely accompanied it. The guard is defensive: a
		// future parser change must not panic a scan over hostile input.
		note := "its manifest is invalid."
		if n := len(errs); n > 0 {
			note = "parsing its manifest failed: " + errs[n-1].Message
		}
		return Record{
			Path: folder, Outcome: OutcomeInvalid, Reason: ReasonManifestInvalid,
			Note: note, Unit: unit,
		}
	}
	outcome := OutcomeSmapi
	if m.ContentPackFor != nil {
		outcome = OutcomeContentPack
	}
	return Record{Path: folder, Outcome: outcome, Unit: unit}
}

// consolidate merges a non-root organizer's child records exactly as SMAPI
// does: only more than one child merges, all-empty becomes one empty record on
// the parent, and all-XNB-or-empty becomes one XNB record on the parent.
func consolidate(folder string, children []Record) []Record {
	if len(children) <= 1 {
		return children
	}
	allEmpty, allEmptyOrXnb := true, true
	for _, child := range children {
		if child.Reason != ReasonEmptyFolder {
			allEmpty = false
		}
		if child.Outcome != OutcomeXnb && child.Reason != ReasonEmptyFolder {
			allEmptyOrXnb = false
		}
	}
	switch {
	case allEmpty:
		return []Record{{
			Path: folder, Outcome: OutcomeInvalid, Reason: ReasonEmptyFolder,
			Note: noteEmptyFolder,
		}}
	case allEmptyOrXnb:
		return []Record{{
			Path: folder, Outcome: OutcomeXnb, Reason: ReasonXnbMod,
			Note: noteXnbMod,
		}}
	default:
		return children
	}
}

// collectFiles lists every file below folder, pruning only directories whose
// name matches the metadata rules (dot-directories are walked, as SMAPI does).
// Descents are guarded against cycles; a folder that can't be read simply
// contributes nothing.
func (s *scanner) collectFiles(folder string) []string {
	var out []string
	s.walkFiles(folder, true, &out)
	return out
}

func (s *scanner) walkFiles(folder string, isStart bool, out *[]string) {
	if !isStart && !s.enter(folder) {
		return
	}
	entries, err := fs.ReadDir(s.fsys, folder)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if s.isDir(entry, folder) {
			if relevantName(entry.Name()) {
				s.walkFiles(path.Join(folder, entry.Name()), false, out)
			}
			continue
		}
		*out = append(*out, entry.Name())
	}
}

// looseRootRecord reports mod-looking files sitting directly in the Mods root,
// which SMAPI never treats as mods. SMAPI logs every loose file and raises its
// error only for manifest.json or *.dll; that actionable case is the one
// reported here.
func (s *scanner) looseRootRecord(entries []fs.DirEntry) *Record {
	var names []string
	modLooking := false
	for _, entry := range entries {
		if s.isDir(entry, ".") {
			continue
		}
		names = append(names, entry.Name())
		if strings.EqualFold(entry.Name(), "manifest.json") || strings.EqualFold(path.Ext(entry.Name()), ".dll") {
			modLooking = true
		}
	}
	if !modLooking {
		return nil
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	return &Record{
		Path: "", Outcome: OutcomeIgnored, Reason: ReasonLooseRootFiles, Files: names,
		Note: "Detected mod files directly inside the mods folder. These will be ignored. Each mod must have its own subfolder instead.",
	}
}

// relevantName is SMAPI's name filter for files and folders: metadata names are
// pruned. The dot-prefix rules differ between files and folders and live in
// their own checks.
func relevantName(name string) bool {
	switch {
	case strings.EqualFold(name, "__folder_managed_by_vortex"), // Vortex marker
		strings.EqualFold(name, "__MACOSX"), // macOS metadata
		strings.EqualFold(name, "mcs"),      // Mono compiler output
		strings.EqualFold(name, "desktop.ini"),
		strings.EqualFold(name, "Thumbs.db"):
		return false
	case strings.HasPrefix(name, "._"), strings.EqualFold(name, ".DS_Store"):
		return false
	}
	return true
}

// relevantFile is SMAPI's file filter: ignored extensions and dot-files don't
// count when deciding whether a folder is empty, XNB-shaped, or a mod.
func relevantFile(name string) bool {
	if strings.HasPrefix(name, ".") || ignoredExtensions[strings.ToLower(path.Ext(name))] {
		return false
	}
	return relevantName(name)
}

// ignoredExtensions is SMAPI's IgnoreFileExtensions table. ".tar.gz" stays for
// parity: it can never match an extension, which is also true upstream.
var ignoredExtensions = map[string]bool{
	".doc": true, ".docx": true, ".md": true, ".rtf": true, ".txt": true,
	".bmp": true, ".gif": true, ".ico": true, ".jpeg": true, ".jpg": true,
	".png": true, ".psd": true, ".tif": true, ".xcf": true,
	".rar": true, ".zip": true, ".7z": true, ".tar": true, ".tar.gz": true,
	".backup": true, ".bak": true, ".old": true,
	".url": true, ".lnk": true,
}

// strictXnbExtensions are the packed-content files that make a folder
// XNB-shaped; potentialXnbExtensions may sit beside them.
var (
	strictXnbExtensions    = map[string]bool{".xgs": true, ".xnb": true, ".xsb": true, ".xwb": true}
	potentialXnbExtensions = map[string]bool{".json": true, ".yaml": true}
)

// xnbMod reports whether relevant files look like a legacy XNB mod: at least
// one packed-content file, and nothing else but .json/.yaml.
func xnbMod(files []string) bool {
	hasXnb := false
	for _, name := range files {
		switch extension := strings.ToLower(path.Ext(name)); {
		case strictXnbExtensions[extension]:
			hasXnb = true
		case !potentialXnbExtensions[extension]:
			return false
		}
	}
	return hasXnb
}

// vortexEmpty reports whether the folder's files are only a Vortex marker and
// optionally config.json — the state Vortex leaves behind for a disabled mod.
// Both name comparisons are case-sensitive, as SMAPI's are.
func vortexEmpty(files []string) bool {
	const marker = "__folder_managed_by_vortex"
	hasMarker := false
	for _, name := range files {
		if name == marker {
			hasMarker = true
			continue
		}
		if relevantFile(name) && name != "config.json" {
			return false
		}
	}
	return hasMarker
}

// hasInstallerScript reports the SMAPI installer bundle, a folder users
// extract here by mistake. Exact names, case-sensitive, as SMAPI matches them.
func hasInstallerScript(files []string) bool {
	for _, name := range files {
		switch name {
		case "install on Linux.sh", "install on macOS.command", "install on Windows.bat":
			return true
		}
	}
	return false
}
