// This file holds the fixture library: an in-memory ZIP builder, so every
// archive shape — and every archive the policy must refuse — is stated in a few
// chained lines inside the test that needs it instead of being committed as a
// binary. A committed archive rots, cannot be read in review, and says nothing
// about why its bytes matter; a builder states the shape under test, and
// Bytes() hands out a fresh archive every call, which is what lets one fixture
// be extracted twice.
//
// The builder writes what archive/zip accepts, including names no extractor may
// accept — a NUL byte, a backslash, an absolute path, a drive letter — because
// those are exactly the shapes the refusals have to be tested against, and a
// writer that refused them would make the policy untestable. The few shapes the
// writer will not produce at all (an unknown compression method, a directory
// declaring data, a central directory whose sizes lie) are made by patching the
// bytes Bytes() returns, which is why it is a plain []byte copy.
package archive

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
)

// zipEntry is one accumulated entry. A nested fixture is resolved when the
// archive is written rather than when it is named, so the inner archive can
// still be extended after it has been attached.
type zipEntry struct {
	name   string
	body   []byte
	nested *Fixture
	method uint16
	mode   fs.FileMode
	flags  uint16
}

// Fixture accumulates one in-memory ZIP archive. Every method returns the
// fixture so calls chain, and every method appends to the same archive, which
// makes a whole package — a mod beside its content pack, a wrapper above both,
// a readme nobody wants — a few lines at the top of a test. Entries keep the
// order they were added in, which is the order extract writes them back.
type Fixture struct {
	entries []zipEntry
}

// NewFixture returns an empty archive.
func NewFixture() *Fixture {
	return &Fixture{}
}

// File writes one deflated file entry, the shape a zip tool produces for text
// and code.
func (f *Fixture) File(name, body string) *Fixture {
	return f.add(name, []byte(body), zip.Deflate, 0o644, 0)
}

// FileBytes is File for bodies that are not text: an asset, a nested archive's
// bytes, anything a string literal would only obscure.
func (f *Fixture) FileBytes(name string, body []byte) *Fixture {
	return f.add(name, body, zip.Deflate, 0o644, 0)
}

// Stored writes one uncompressed entry, the shape a zip tool produces for
// payloads that are already compressed — and the one that makes a single
// patched byte a CRC failure rather than a decompression error.
func (f *Fixture) Stored(name, body string) *Fixture {
	return f.add(name, []byte(body), zip.Store, 0o644, 0)
}

// Dir writes an explicit directory entry; the trailing slash ZIP requires is
// added when the caller left it off. A directory no file mentions has no other
// way to exist, which is why real archives carry these and why the digest must
// ignore them.
func (f *Fixture) Dir(name string) *Fixture {
	if !strings.HasSuffix(name, "/") {
		name += "/"
	}
	return f.add(name, nil, zip.Store, fs.ModeDir|0o755, 0)
}

// Symlink writes a unix symlink entry: the target string is the entry's
// content, and the unix mode marks it as a link. Windows tooling cannot write
// this shape — it stores links as ordinary files — so it is deliberately built
// the way a unix zip tool would.
func (f *Fixture) Symlink(name, target string) *Fixture {
	return f.add(name, []byte(target), zip.Deflate, fs.ModeSymlink|0o777, 0)
}

// Encrypted writes an entry with the general-purpose "encrypted" bit set, the
// way a password-protected zip does. The body is written in the clear: the
// policy refuses the entry from its header, long before anything would try to
// decrypt it, so the bytes never matter.
func (f *Fixture) Encrypted(name, body string) *Fixture {
	return f.add(name, []byte(body), zip.Deflate, 0o644, 0x1)
}

// Nested writes inner.Bytes() as one file entry, never descending into it: the
// double-zipped shape SMAPI's own release publishes on purpose, and the shape
// of the installer's internal/install.dat payload.
func (f *Fixture) Nested(name string, inner *Fixture) *Fixture {
	f.entries = append(f.entries, zipEntry{name: name, nested: inner, method: zip.Deflate, mode: 0o644})
	return f
}

// Mod adds a valid code mod: a manifest naming the folder's own name plus .dll
// as its entry assembly, beside the assembly file itself. That is the shape
// every sampled release has in common.
func (f *Fixture) Mod(dir, uid string) *Fixture {
	dll := path.Base(dir) + ".dll"
	return f.File(path.Join(dir, "manifest.json"), codeModManifest(uid, dll)).
		File(path.Join(dir, dll), "MZ")
}

// Pack adds a valid Content Pack for hostID, beside the content.json the pack's
// framework reads and the folder convention's bracketed prefix in the name.
func (f *Fixture) Pack(dir, uid, hostID string) *Fixture {
	return f.File(path.Join(dir, "manifest.json"), contentPackManifest(uid, hostID)).
		File(path.Join(dir, "content.json"), "{}")
}

// Xnb adds a legacy XNB content file: packed game content with no manifest,
// which SMAPI cannot load and Astradew diagnoses but never deploys.
func (f *Fixture) Xnb(dir string) *Fixture {
	return f.File(path.Join(dir, "content.xnb"), "XNB")
}

// Bytes returns the archive, freshly written on every call so the same fixture
// can be extracted twice and the same test can patch one copy's bytes without
// disturbing the next. It panics when archive/zip refuses to write an entry —
// an over-long name, or a method with no registered compressor: a fixture is
// test code describing an archive that is supposed to exist, so a failure to
// build it is a bug in the test, not a condition to return.
func (f *Fixture) Bytes() []byte {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range f.entries {
		body := entry.body
		if entry.nested != nil {
			body = entry.nested.Bytes()
		}
		header := &zip.FileHeader{Name: entry.name, Method: entry.method, Flags: entry.flags}
		header.SetMode(entry.mode)
		target, err := writer.CreateHeader(header)
		if err != nil {
			panic("archive fixture: writing entry " + strconv.Quote(entry.name) + ": " + err.Error())
		}
		if _, err := target.Write(body); err != nil {
			panic("archive fixture: writing entry " + strconv.Quote(entry.name) + ": " + err.Error())
		}
	}
	if err := writer.Close(); err != nil {
		panic("archive fixture: closing the archive: " + err.Error())
	}
	return buffer.Bytes()
}

// WriteTo writes Bytes() to filePath, creating or truncating it.
func (f *Fixture) WriteTo(filePath string) error {
	return os.WriteFile(filePath, f.Bytes(), 0o644)
}

// add appends one entry.
func (f *Fixture) add(name string, body []byte, method uint16, mode fs.FileMode, flags uint16) *Fixture {
	f.entries = append(f.entries, zipEntry{name: name, body: body, method: method, mode: mode, flags: flags})
	return f
}

// codeModManifest is the manifest a code mod carries: identity, version, and
// the assembly it loads.
func codeModManifest(uid, dll string) string {
	return `{"Name":"` + uid + `","Author":"Astradew fixture","Version":"1.0.0","UniqueID":"` + uid + `","EntryDll":"` + dll + `"}`
}

// contentPackManifest is the manifest a Content Pack carries: the same identity
// fields, with ContentPackFor in place of an entry assembly.
func contentPackManifest(uid, hostID string) string {
	return `{"Name":"` + uid + `","Author":"Astradew fixture","Version":"1.0.0","UniqueID":"` + uid + `","ContentPackFor":{"UniqueID":"` + hostID + `"}}`
}

// SingleMod returns the shape of a typical GitHub-hosted release: one wrapper
// folder named after the mod, holding its manifest and assembly plus the i18n
// folder and pre-shipped config.json BetterRanching-class mods ship.
func SingleMod() *Fixture {
	return NewFixture().
		Mod("FishZones", "Mushymato.FishZones").
		File("FishZones/config.json", `{"Enabled":true}`).
		File("FishZones/i18n/default.json", `{"fish":"Fish"}`)
}

// Wrapper returns the container-folder shape the player guide warns about: a
// download whose outer folder carries the mod's id and version, holding a
// readme beside the real mod folder. It is the layout Nexus-style file names
// produce and the most common reason a player's mods end up nested.
func Wrapper() *Fixture {
	return NewFixture().
		File("FishZones-12345-0-3-2/readme.txt", "unzip me into Mods").
		Mod("FishZones-12345-0-3-2/FishZones", "Mushymato.FishZones")
}

// MultiMod returns the layout the SMAPI build package writes when a project
// bundles content packs: one wrapper folder holding the main mod and each pack
// as siblings, which is Stardew Valley Expanded's published shape.
func MultiMod() *Fixture {
	return NewFixture().
		Mod("Stardew Valley Expanded/StardewValleyExpanded", "FlashShifter.StardewValleyExpanded").
		Pack("Stardew Valley Expanded/[CP] Stardew Valley Expanded", "FlashShifter.SVE.CP", "Pathoschild.ContentPatcher").
		Pack("Stardew Valley Expanded/[FTM] Stardew Valley Expanded", "FlashShifter.SVE.FTM", "Esca.FarmTypeManager")
}

// ContentPack returns a Content Patcher pack: manifest and content.json with an
// assets folder beside them, named with the community's bracketed prefix.
func ContentPack() *Fixture {
	return NewFixture().
		Pack("[CP] FishZones", "Mushymato.FishZones.CP", "Pathoschild.ContentPatcher").
		File("[CP] FishZones/assets/fish.png", "PNG")
}

// LegacyXnb returns an XNB replacement pack: a Content tree of .xnb files and
// no manifest, the shape that overwrites game content and that Astradew
// diagnoses as a legacy mod it will never install.
func LegacyXnb() *Fixture {
	return NewFixture().
		Xnb("Content/Animals").
		Xnb("Content/Maps")
}

// NestedArchive returns the double-zipped shape SMAPI publishes on purpose: an
// archive whose only entry is another archive, here the installer bundle with
// its platform scripts and its own nested install.dat payload.
func NestedArchive() *Fixture {
	return NewFixture().Nested("SMAPI-4.5.2-installer.zip", smapiInstaller())
}

// smapiInstaller is the installer bundle shape: the platform scripts and
// readme inside a version-suffixed wrapper folder, with each platform's payload
// a nested zip under internal/.
func smapiInstaller() *Fixture {
	inner := NewFixture().
		File("Mods/ConsoleCommands/manifest.json", codeModManifest("SMAPI.ConsoleCommands", "ConsoleCommands.dll")).
		File("Mods/ConsoleCommands/ConsoleCommands.dll", "MZ").
		File("StardewModdingAPI.exe", "MZ")
	return NewFixture().
		File("SMAPI 4.5.2 installer/README.txt", "read me first").
		File("SMAPI 4.5.2 installer/install on Windows.bat", "@echo off").
		Nested("SMAPI 4.5.2 installer/internal/windows/install.dat", inner)
}
