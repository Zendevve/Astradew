// This file holds the fixture library: an in-memory Mods tree builder, so every
// scanner state composes in a few chained lines instead of a checked-in fixture
// tree. Real trees rot — a symlink here, a binary asset there, a platform
// difference everywhere — while a builder states the state under test in the
// test itself. Later phases (the load pipeline, sample data for the UI) reuse it
// too, so it lives in the package proper and depends on nothing outside the
// standard library.
package manifest

import (
	"io/fs"
	"path"
	"strings"
	"testing/fstest"
)

// Fixture accumulates one in-memory Mods tree. Every method returns the fixture
// so calls chain, and every method writes into the same tree, which makes a
// whole scanner state — a valid unit beside a partial one, an XNB drop, an
// ignored dot folder, a renamed System Mod — a few lines at the top of a test.
// FS hands the tree to Scan, and may be called again for a second scan.
type Fixture struct {
	files fstest.MapFS
}

// NewFixture returns an empty tree.
func NewFixture() *Fixture {
	return &Fixture{files: fstest.MapFS{}}
}

// File writes one raw file: the escape hatch for anything the typed helpers
// don't cover, such as a config.json, an asset, or a stray text file.
func (f *Fixture) File(name, body string) *Fixture {
	f.files[name] = &fstest.MapFile{Data: []byte(body)}
	return f
}

// Dir creates an explicitly empty folder. A folder with nothing in it has no
// other way to be named: its parent's listing would never mention it.
func (f *Fixture) Dir(name string) *Fixture {
	f.files[name] = &fstest.MapFile{Mode: fs.ModeDir}
	return f
}

// Mod adds a valid code mod: a version 1.0.0 manifest naming Mod.dll as its
// entry assembly, beside the assembly file itself.
func (f *Fixture) Mod(dir, uid string) *Fixture {
	return f.ModVersion(dir, uid, "1.0.0")
}

// ModVersion is Mod with a chosen version. Two units that must differ without
// either one becoming invalid differ here.
func (f *Fixture) ModVersion(dir, uid, version string) *Fixture {
	return f.manifest(dir, `{"Name":"`+uid+`","Author":"A","Version":"`+version+`","UniqueID":"`+uid+`","EntryDll":"Mod.dll"}`).
		File(path.Join(dir, "Mod.dll"), "MZ")
}

// Pack adds a valid Content Pack for hostID, beside the content.json SMAPI
// expects a pack to carry.
func (f *Fixture) Pack(dir, uid, hostID string) *Fixture {
	return f.manifest(dir, `{"Name":"`+uid+`","Author":"A","Version":"1.0.0","UniqueID":"`+uid+`","ContentPackFor":{"UniqueID":"`+hostID+`"}}`).
		File(path.Join(dir, "content.json"), "{}")
}

// SystemMod adds a valid unit whose Unique ID names one of SMAPI's bundled Mod
// Units, in whatever folder: identity, never a folder name, is what makes a
// System Mod, so the given folder stays free to be a peculiar name.
func (f *Fixture) SystemMod(dir string) *Fixture {
	return f.Mod(dir, "SMAPI.ConsoleCommands")
}

// Partial adds a unit with a valid identity and one malformed optional field (a
// MinimumApiVersion that isn't a version): the parser warns and drops the field
// rather than failing, so the verdict is partial.
func (f *Fixture) Partial(dir, uid string) *Fixture {
	return f.manifest(dir, `{"Name":"`+uid+`","Author":"A","Version":"1.0.0","UniqueID":"`+uid+`","EntryDll":"Mod.dll","MinimumApiVersion":4}`).
		File(path.Join(dir, "Mod.dll"), "MZ")
}

// BrokenManifest writes manifest.json verbatim, however mangled: the state an
// interrupted download, a text editor, or a bad encoding leaves behind.
func (f *Fixture) BrokenManifest(dir, body string) *Fixture {
	return f.manifest(dir, body)
}

// MissingManifest adds a folder that holds files but no manifest.json at all,
// the state of a hand-extracted or half-deleted mod. With no file names it
// holds a single Mod.dll, the file SMAPI's own scanner keys on.
func (f *Fixture) MissingManifest(dir string, files ...string) *Fixture {
	if len(files) == 0 {
		files = []string{"Mod.dll"}
	}
	for _, name := range files {
		f.File(path.Join(dir, name), "x")
	}
	return f
}

// XnbMod adds a legacy XNB drop: packed content and no manifest, which SMAPI
// can't load and Astradew diagnoses but never deploys.
func (f *Fixture) XnbMod(dir string) *Fixture {
	return f.File(path.Join(dir, "content.xnb"), "xnb")
}

// Ignored adds a valid unit inside a dot-prefixed folder, the state SMAPI skips
// by convention. The dot is added when dir doesn't already carry one, because
// the dot is the whole point of the helper.
func (f *Fixture) Ignored(dir, uid string) *Fixture {
	if !strings.HasPrefix(path.Base(dir), ".") {
		dir = path.Join(path.Dir(dir), "."+path.Base(dir))
	}
	return f.Mod(dir, uid)
}

// FS returns the accumulated tree, as a fresh copy: a test can edit or drop
// entries in what it gets back without disturbing the fixture — including
// mutating a file's bytes in place, which an edit-between-scans test does — and
// can call FS again to scan the same fixture a second time.
func (f *Fixture) FS() fstest.MapFS {
	out := make(fstest.MapFS, len(f.files))
	for name, file := range f.files {
		clone := *file
		clone.Data = append([]byte(nil), file.Data...)
		out[name] = &clone
	}
	return out
}

// manifest writes one folder's manifest.json verbatim.
func (f *Fixture) manifest(dir, body string) *Fixture {
	return f.File(path.Join(dir, "manifest.json"), body)
}
