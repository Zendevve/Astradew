// This file holds the root pass: what the staged package's own top level
// contributes beyond the scanner's records, and the bounded walk both root-pass
// searches share. The scanner's root loop only descends into children, so the
// stage root itself — the one folder an archive's layout is defined at — is the
// part of the tree only this package can classify.
package inspect

import (
	"errors"
	"io/fs"
	"path"
	"strings"

	"github.com/Zendevve/astradew/internal/detect"
	"github.com/Zendevve/astradew/internal/manifest"
)

const (
	// conventionalManifest is the file name SMAPI reads a Mod Unit's manifest
	// from: a unit's own folder root, or — here — the stage root itself.
	conventionalManifest = "manifest.json"
	// rootContentDir and smapiPayloadDir are the two top-level directories the
	// root pass classifies. Both are compared case-insensitively, the way the
	// package may spell them after a round trip through an archive, and both
	// findings name the conventional spelling.
	rootContentDir  = "Content"
	smapiPayloadDir = "smapi-internal"
)

// legacyXnbNotice is PRD §16's copy verbatim: spelling and the line break
// included, because the display contract quotes it.
const legacyXnbNotice = "This package replaces Stardew Valley content files directly.\nAstradew does not install XNB mods automatically."

// rootContentMessage adds the destination the copy alone does not name: a
// Content tree is not a broken mod, it is content that belongs to the game
// folder itself.
const rootContentMessage = legacyXnbNotice + "\nIt belongs in the game folder, not in Mods."

// smapiPayloadNote explains the extracted installer payload: it is the bundle
// that installs SMAPI itself, so there is no Mod Unit here to install.
const smapiPayloadNote = "this folder is the SMAPI installer payload (install.dat): it installs SMAPI itself into the game folder and is not a Mod Unit."

// hiddenManifestNote carries the re-pack guidance a poisoned organizer needs:
// the nested manifest is real, and the fix is to re-pack the folder so it sits
// where SMAPI looks for it.
const hiddenManifestNote = "found a nested manifest.json: SMAPI reads a mod's manifest.json only from the mod's own folder, so this unit would never load. Re-pack the folder so each mod's files and manifest.json sit together in their own folder."

// The two root-pass walks are bounded by the same pair of numbers: at most this
// many directory entries are examined, and folders deeper than this many levels
// below the folder the walk started at are refused. Both searches answer one
// question — is a manifest, or packed content, down there — and neither may
// cost more than a bounded, small amount of work on a hostile tree.
const (
	walkEntryLimit = 1000
	walkDepthLimit = 8
)

// rootPass is what the staged package's own top level contributes beyond the
// scanner's records.
type rootPass struct {
	// unit is the stage root's own manifest, parsed, when one exists and was
	// read. folderName is the manifest's Name: a root manifest ships no folder
	// name, so the installed folder takes the manifest's own (the Vortex
	// convention).
	unit       *manifest.Unit
	folderName string
	// unreadable is the root manifest's read failure, when the file exists but
	// could not be read.
	unreadable *Finding
	// contentDir is the top-level Content directory's name as the package
	// spells it, "" when there is none; contentXnb reports whether that tree
	// actually holds packed content.
	contentDir string
	contentXnb bool
	// payload reports a top-level smapi-internal directory.
	payload bool
}

// inspectRoot reads the stage root's own listing once and classifies what the
// scanner's records cannot: the root manifest, the root Content tree, and the
// SMAPI installer payload.
func inspectRoot(fsys fs.FS, opts Options) rootPass {
	var pass rootPass
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		// A root the filesystem refuses to list is nothing to inspect: the
		// scanner's own root read has already reported the package as empty.
		return pass
	}
	for _, entry := range entries {
		if !isDir(fsys, ".", entry) {
			continue
		}
		switch {
		case pass.contentDir == "" && strings.EqualFold(entry.Name(), rootContentDir):
			pass.contentDir = entry.Name()
			pass.contentXnb = holdsStrictXnb(fsys, entry.Name())
		case strings.EqualFold(entry.Name(), smapiPayloadDir):
			pass.payload = true
		}
	}

	name, found, err := rootManifestName(fsys, entries, opts.CaseInsensitivePaths)
	switch {
	case err != nil:
		pass.unreadable = rootManifestFinding(name, err)
	case !found:
	default:
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			pass.unreadable = rootManifestFinding(name, err)
			break
		}
		// The same three steps the scanner's parseUnit takes, so a root unit
		// carries exactly what a scanned one does: parsed manifest, verdict,
		// field errors, system status by Unique ID.
		m, verdict, errs := manifest.Parse(data)
		pass.unit = &manifest.Unit{
			Manifest: m, Verdict: verdict, Errors: errs,
			SystemMod: detect.IsBundledUniqueID(m.UniqueID),
		}
		pass.folderName = m.Name
	}
	return pass
}

// rootManifestName resolves the stage root's own manifest.json, mirroring the
// scanner's lookup: the conventional name first, then — under the
// case-insensitive option — the first matching spelling in the listing's sorted
// order. A folder named manifest.json is not a manifest, exactly as it is not
// one anywhere else. found is false when the root holds no manifest at all; a
// non-nil error is a stat failure on the conventional name, reported at that
// path.
func rootManifestName(fsys fs.FS, entries []fs.DirEntry, caseInsensitive bool) (string, bool, error) {
	info, err := fs.Stat(fsys, conventionalManifest)
	switch {
	case err == nil && !info.IsDir():
		return conventionalManifest, true, nil
	case err == nil:
		// A folder named manifest.json is not a manifest; the case-insensitive
		// fallback below still may find a differently-cased file.
	case !errors.Is(err, fs.ErrNotExist):
		return conventionalManifest, false, err
	}
	if !caseInsensitive {
		return "", false, nil
	}
	for _, entry := range entries {
		if isDir(fsys, ".", entry) {
			continue
		}
		if strings.EqualFold(entry.Name(), conventionalManifest) {
			return entry.Name(), true, nil
		}
	}
	return "", false, nil
}

// rootManifestFinding reports a root manifest that exists but cannot be read.
// The path is the file that failed — the conventional spelling unless the
// case-insensitive fallback matched a differently-cased name — and the message
// is the wording the scanner uses for the same failure in a folder.
func rootManifestFinding(name string, err error) *Finding {
	return &Finding{
		Kind:    FindingUnreadable,
		Path:    name,
		Message: "its manifest couldn't be read: " + err.Error(),
	}
}

// rootContentTreeFinding is the one finding that replaces every record of a
// root Content tree: the tree is game-folder content, and the message says both
// what it is and where it belongs.
func rootContentTreeFinding() Finding {
	return Finding{Kind: FindingRootContentTree, Path: rootContentDir, Message: rootContentMessage}
}

// holdsStrictXnb reports whether a bounded walk below dir finds at least one
// strict XNB file: the packed-content extensions the scanner's own XNB test
// keys on. Potential content (.json/.yaml beside them) never counts, exactly as
// it never makes a folder XNB-shaped in the scan.
func holdsStrictXnb(fsys fs.FS, dir string) bool {
	found := false
	walkBounded(fsys, dir, func(rel string) bool {
		found = strictXnbExtension(path.Ext(rel))
		return !found
	})
	return found
}

// strictXnbExtension reports the packed-content extensions, mirroring the
// scanner's strict set.
func strictXnbExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".xgs", ".xnb", ".xsb", ".xwb":
		return true
	}
	return false
}

// isDir reports whether an entry is a folder for traversal purposes: a real
// folder, or a link whose target is a folder. It mirrors the scanner's own
// check, so the root pass sees the same top level the scan saw.
func isDir(fsys fs.FS, parent string, entry fs.DirEntry) bool {
	if entry.IsDir() {
		return true
	}
	if entry.Type().IsRegular() {
		return false
	}
	info, err := fs.Stat(fsys, path.Join(parent, entry.Name()))
	return err == nil && info.IsDir()
}

// walkBounded walks the files below dir in sorted, depth-first order, examining
// at most walkEntryLimit directory entries and descending at most walkDepthLimit
// levels below dir. Dot-prefixed names and the manager metadata the scanner's
// name filter drops are pruned — __MACOSX noise must never look like a buried
// manifest or buried content — and a folder the filesystem refuses to read
// contributes nothing. Both callers ask a yes/no question about a subtree, and
// noise or an unreadable subtree must not turn that question into a false yes
// or an error. visit receives each file's path relative to the stage root and
// reports whether the walk continues, so a yes/no caller stops at the first yes.
func walkBounded(fsys fs.FS, dir string, visit func(rel string) bool) {
	examined := 0
	var walk func(dir string, depth int) bool
	walk = func(dir string, depth int) bool {
		entries, err := fs.ReadDir(fsys, dir)
		if err != nil {
			return true
		}
		for _, entry := range entries {
			if examined >= walkEntryLimit {
				return false
			}
			examined++
			name := entry.Name()
			if isDir(fsys, dir, entry) {
				if depth < walkDepthLimit && !prunedName(name) && !walk(path.Join(dir, name), depth+1) {
					return false
				}
				continue
			}
			if prunedName(name) {
				continue
			}
			if !visit(path.Join(dir, name)) {
				return false
			}
		}
		return true
	}
	walk(dir, 0)
}

// prunedName reports whether the scanner's name filter drops a name: the
// manager metadata names it matches outright, and every dot-prefixed name
// (SMAPI's file filter drops dot-files, its traversal drops dot-folders). The
// table mirrors internal/manifest's unexported filter, which stays the
// scanner's authority on what is metadata and what is a mod.
func prunedName(name string) bool {
	if strings.HasPrefix(name, ".") { // .DS_Store and ._* resource forks included
		return true
	}
	switch {
	case strings.EqualFold(name, "__folder_managed_by_vortex"), // Vortex marker
		strings.EqualFold(name, "__MACOSX"), // macOS metadata
		strings.EqualFold(name, "mcs"),      // Mono compiler output
		strings.EqualFold(name, "desktop.ini"),
		strings.EqualFold(name, "Thumbs.db"):
		return true
	}
	return false
}
