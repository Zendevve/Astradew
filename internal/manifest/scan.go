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
//     logs prose, throws, or reuses ManifestMissing respectively;
//   - the XNB note appends Astradew's never-deploy guidance to SMAPI's own
//     sentence, because the product diagnoses legacy XNB content and never
//     installs it;
//   - a per-scan work bound (defaultScanLimit listings read, defaultEntryLimit
//     entries examined, maxScanDepth components deep) that ends the scan once a
//     bound is spent, reported as one ignored/scan-limit record at the folder it
//     ended on. SMAPI's recursion has no bound at all, and a link loop a plain
//     fs.FS cannot see through would never end; a bound that counted only
//     listings would let one wide folder of links multiply the records it
//     produces by the number of passes.
//
// Parity is deliberate elsewhere: directory links are followed like real
// folders (SMAPI gets that from DirectoryInfo; a plain fs.FS needs the Stat in
// isDir), and every other note string is SMAPI's own wording.
package manifest

import (
	"errors"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/Zendevve/astradew/internal/detect"
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
	// ReasonScanLimit means the scan's folder budget ran out before this folder
	// was read, so its contents are unknown rather than empty.
	ReasonScanLimit Reason = "scan-limit"
)

// SMAPI's wording for the two outcomes that can be reported either directly or
// by consolidation, kept in one place so parity text can't drift apart. The
// XNB note carries the product's never-deploy guidance after SMAPI's sentence:
// a legacy XNB folder is diagnosed, never installed.
const (
	noteEmptyFolder = "it's an empty folder."
	noteXnbMod      = "it's not a SMAPI mod (see https://smapi.io/xnb for info). Astradew does not install XNB mods automatically."
)

// The scan's three work bounds. Together they bound what one scan can cost: no
// record or visited key can exceed the entries examined, no listing can be read
// without an entry having paid for it, and no path can grow past maxScanDepth.
// A tree a plain fs.FS cannot see through — no LinkResolver means the visited
// set has no canonical identity to match, so a link loop never repeats a key —
// therefore ends at a bound with one ignored/scan-limit record instead of an
// endless walk.
//
// One floor stays and is deliberate: fs.ReadDir hands back a whole listing at
// once, so the widest single directory a tree holds is memory no charge can
// refuse. The numbers are sized so no real Mods tree approaches them — a
// two-thousand-mod tree reads a couple of thousand listings and examines tens
// of thousands of entries — while a hostile one stops in bounded time.
const (
	defaultScanLimit  = 10_000
	defaultEntryLimit = 100_000
	maxScanDepth      = 64
)

// noteScanLimit is the record note for a folder a bound left unread or
// unexpanded. No number appears in the sentence, so retuning the constants
// cannot stale it.
const noteScanLimit = "not scanned: this scan reached its limit (a link loop can do this)."

// Unit is one parsed Mod Unit: how its Manifest parsed, with per-field detail.
type Unit struct {
	Manifest Manifest
	Verdict  Verdict
	Errors   []FieldError
	// SystemMod reports whether the Manifest's Unique ID names one of SMAPI's
	// bundled Mod Units (Console Commands, Save Backup), compared
	// case-insensitively against the Phase 1 allow-list. Folder names never
	// confer system status: a renamed bundle still matches, a forged folder
	// name stays an ordinary third-party unit.
	SystemMod bool
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
	// Note is the human-facing explanation: SMAPI's wording where the outcome
	// mirrors SMAPI's, plus Astradew's guidance for the cases SMAPI only logs,
	// throws on, or leaves to the product.
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
	// Cache, when set, serves parsed manifests whose file stat is unchanged,
	// so rescanning an untouched Mods root re-reads nothing. The zero value
	// scans without caching, reading and parsing every manifest every time.
	// Construct one with NewCache: a zero Cache must not be used. The cache
	// lives exactly as long as the caller keeps it and never persists.
	Cache *Cache
	// scanLimit and entryLimit override the scan's work bounds (<= 0 means the
	// default). They are unexported on purpose: they exist so tests can exercise
	// the bounds on a handful of folders and entries, not as product knobs.
	scanLimit  int
	entryLimit int
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
//
// One scan reads at most defaultScanLimit folder listings, examines at most
// defaultEntryLimit directory entries, and refuses folders deeper than
// maxScanDepth; the first bound it spends ends the scan, which reports one
// ignored scan-limit record naming the folder it ended on. No tree — a link loop
// included — can therefore run it without bound. A bridge that wants accurate
// loop reporting implements LinkResolver; without one these bounds are the only
// protection, because literal paths cannot see through a link.
func Scan(fsys fs.FS, opts Options) []Record {
	s := &scanner{fsys: fsys, opts: opts, visited: map[string]bool{}, budget: opts.scanLimit, entries: opts.entryLimit}
	if s.budget <= 0 {
		s.budget = defaultScanLimit
	}
	if s.entries <= 0 {
		s.entries = defaultEntryLimit
	}
	s.resolver, _ = fsys.(LinkResolver)

	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return []Record{}
	}
	s.visited[s.canonical(".")] = true

	out := make([]Record, 0, min(len(entries), s.entries+1))
	if loose := s.looseRootRecord(entries); loose != nil {
		out = append(out, *loose)
	}
	for _, entry := range entries {
		if s.done {
			break
		}
		if !s.examine() {
			return append(out, scanLimitRecord(""))
		}
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
	budget   int  // folder listings this scan may still read; see spend
	entries  int  // directory entries this scan may still examine; see examine
	done     bool // a bound ended the scan: enclosing loops break instead of reporting more
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

// spend draws one folder listing from the scan's budget, reporting false when
// it is spent. The Mods root listing is never charged: the budget bounds what a
// scan expands below the root, and every scan needs that one listing to start
// from. The first false ends the scan (see done), so the caller that saw it
// reports where the scan stopped and nothing after it is examined.
func (s *scanner) spend() bool {
	if s.budget <= 0 {
		s.done = true
		return false
	}
	s.budget--
	return true
}

// examine draws one directory entry from the scan's entry budget, reporting
// false when it is spent. Charging per entry, not just per listing, is what
// keeps one wide folder of links from multiplying the scan's records and
// visited keys by the number of passes over it: the entry is the unit of work
// each listing hands out.
func (s *scanner) examine() bool {
	if s.entries <= 0 {
		s.done = true
		return false
	}
	s.entries--
	return true
}

// tooDeep reports whether folder sits maxScanDepth or more path components below
// the root. No SMAPI-visible tree comes close, and the cap keeps the path
// strings the visited set and every record retain linear in the bounds instead
// of quadratic. A folder it refuses ends the scan like a spent bound does, so
// the report still carries exactly one record saying where scanning stopped.
func tooDeep(folder string) bool {
	return strings.Count(folder, "/") >= maxScanDepth
}

// halt ends the scan without charging a bound: the depth cap refuses a folder
// outright, and the refusal has to end the scan the way a spent bound does, or
// every deep branch would add its own record claiming to be the stop.
func (s *scanner) halt() {
	s.done = true
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
	if tooDeep(folder) {
		s.halt()
		return []Record{scanLimitRecord(folder)}
	}
	if !s.spend() {
		return []Record{scanLimitRecord(folder)}
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
		if !s.examine() {
			return []Record{scanLimitRecord(folder)}
		}
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
			if s.done {
				break
			}
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
	unit, err := s.readUnit(folder, entries)
	switch {
	case err == nil:
		return recordForUnit(folder, unit)
	case !errors.Is(err, fs.ErrNotExist):
		return Record{
			Path: folder, Outcome: OutcomeInvalid, Reason: ReasonUnreadable,
			Note: "its manifest couldn't be read: " + err.Error(),
		}
	}

	files, complete := s.collectFiles(folder)
	if !complete {
		return scanLimitRecord(folder)
	}
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

// manifestPath names the folder's own manifest.json, top level only. The
// conventional name wins when several spellings exist; the case-insensitive
// fallback follows the option, and picks the first match in sorted order
// (SMAPI's own pick is enumeration-order dependent, so it is unspecified).
// Resolving the name through Stat rather than a read is what lets a cache hit
// skip the read: the scanner has to know which file it is about to consult
// before it can ask whether that file's stat was already parsed.
func (s *scanner) manifestPath(folder string, entries []fs.DirEntry) (string, error) {
	const conventional = "manifest.json"
	name := path.Join(folder, conventional)
	_, err := fs.Stat(s.fsys, name)
	switch {
	case err == nil:
		return name, nil
	case !errors.Is(err, fs.ErrNotExist):
		return "", err
	}
	if !s.opts.CaseInsensitivePaths {
		return "", fs.ErrNotExist
	}
	for _, entry := range entries {
		if !s.isDir(entry, folder) && strings.EqualFold(entry.Name(), conventional) {
			return path.Join(folder, entry.Name()), nil
		}
	}
	return "", fs.ErrNotExist
}

// readUnit resolves the folder's manifest and parses it, serving the cached
// parse when a Cache is configured and the manifest file's stat is unchanged.
// The error is the manifest's own, exactly as it was before caching existed: a
// folder with no manifest reports fs.ErrNotExist, and one whose manifest can't
// be read reports why.
func (s *scanner) readUnit(folder string, entries []fs.DirEntry) (*Unit, error) {
	name, err := s.manifestPath(folder, entries)
	if err != nil {
		return nil, err
	}
	if s.opts.Cache != nil {
		return s.opts.Cache.unit(s.fsys, name)
	}
	data, err := fs.ReadFile(s.fsys, name)
	if err != nil {
		return nil, err
	}
	return parseUnit(data), nil
}

// parseUnit parses one manifest and marks system status. Marking happens here,
// where the manifest is parsed, so a unit served from the cache carries the
// same marking as one parsed fresh.
func parseUnit(data []byte) *Unit {
	m, verdict, errs := Parse(data)
	return &Unit{Manifest: m, Verdict: verdict, Errors: errs, SystemMod: detect.IsBundledUniqueID(m.UniqueID)}
}

// recordForUnit turns one parsed Unit into its folder's record. A manifest that
// fails validation still reports its parsed detail alongside the failure.
func recordForUnit(folder string, unit *Unit) Record {
	if unit.Verdict == VerdictInvalid {
		// Parse appends failures after warnings and never returns an invalid
		// verdict without one, so the last error names the failure rather than
		// a warning that merely accompanied it. The guard is defensive: a
		// future parser change must not panic a scan over hostile input.
		note := "its manifest is invalid."
		if n := len(unit.Errors); n > 0 {
			note = "parsing its manifest failed: " + unit.Errors[n-1].Message
		}
		return Record{
			Path: folder, Outcome: OutcomeInvalid, Reason: ReasonManifestInvalid,
			Note: note, Unit: unit,
		}
	}
	outcome := OutcomeSmapi
	if unit.Manifest.ContentPackFor != nil {
		outcome = OutcomeContentPack
	}
	return Record{Path: folder, Outcome: outcome, Unit: unit}
}

// scanLimitRecord reports a folder a work bound left unread or unexpanded. Such
// a folder is unknown, not empty: nothing about it may be inferred from a
// listing the scan never finished, and its record is the one signal that the
// scan stopped here.
func scanLimitRecord(folder string) Record {
	return Record{Path: folder, Outcome: OutcomeIgnored, Reason: ReasonScanLimit, Note: noteScanLimit}
}

// consolidate merges a non-root organizer's child records exactly as SMAPI
// does: only more than one child merges, all-empty becomes one empty record on
// the parent, and all-XNB-or-empty becomes one XNB record on the parent. A
// child the budget left unexpanded (ignored/scan-limit) fails both merge tests,
// so the children of a partly scanned organizer always pass through unmerged.
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
// contributes nothing. complete is false when the scan's folder budget ran out
// mid-walk: the list is then a fragment, which callers must report rather than
// classify from.
func (s *scanner) collectFiles(folder string) (files []string, complete bool) {
	var out []string
	complete = s.walkFiles(folder, true, &out)
	return out, complete
}

// walkFiles lists the files below folder, reporting false when a bound stopped
// it short: a budget ran out, or a subfolder sits deeper than maxScanDepth. A
// cycle prune is not a truncation: the guard cuts a loop already seen at this
// path, and what has been listed stays usable for classification.
func (s *scanner) walkFiles(folder string, isStart bool, out *[]string) bool {
	if !isStart && !s.enter(folder) {
		return true
	}
	if !s.spend() {
		return false
	}
	entries, err := fs.ReadDir(s.fsys, folder)
	if err != nil {
		return true
	}
	complete := true
	for _, entry := range entries {
		if s.done {
			return false
		}
		if !s.examine() {
			return false
		}
		if s.isDir(entry, folder) {
			if relevantName(entry.Name()) {
				child := path.Join(folder, entry.Name())
				if tooDeep(child) {
					s.halt()
					return false
				}
				if !s.walkFiles(child, false, out) {
					complete = false
				}
			}
			continue
		}
		*out = append(*out, entry.Name())
	}
	return complete
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
