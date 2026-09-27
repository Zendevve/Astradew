// This file holds the fixture library's own tests: that the corpus shapes are
// the shapes they claim, that Bytes() is fresh every call, and that the builder
// really can synthesize the names a writer accepts and an extractor must
// refuse. The last one matters most: without it, the refusal tests could be
// testing names the fixture never actually wrote.
package archive

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Zendevve/astradew/internal/apperror"
)

func TestCorpusConstructorsProduceTheNamedShapes(t *testing.T) {
	cases := []struct {
		name    string
		fixture *Fixture
		want    []string
	}{
		{
			name:    "single mod",
			fixture: SingleMod(),
			want: []string{
				"FishZones/manifest.json",
				"FishZones/FishZones.dll",
				"FishZones/config.json",
				"FishZones/i18n/default.json",
			},
		},
		{
			name:    "wrapper",
			fixture: Wrapper(),
			want: []string{
				"FishZones-12345-0-3-2/readme.txt",
				"FishZones-12345-0-3-2/FishZones/manifest.json",
				"FishZones-12345-0-3-2/FishZones/FishZones.dll",
			},
		},
		{
			name:    "multi-mod",
			fixture: MultiMod(),
			want: []string{
				"Stardew Valley Expanded/StardewValleyExpanded/manifest.json",
				"Stardew Valley Expanded/StardewValleyExpanded/StardewValleyExpanded.dll",
				"Stardew Valley Expanded/[CP] Stardew Valley Expanded/manifest.json",
				"Stardew Valley Expanded/[CP] Stardew Valley Expanded/content.json",
				"Stardew Valley Expanded/[FTM] Stardew Valley Expanded/manifest.json",
				"Stardew Valley Expanded/[FTM] Stardew Valley Expanded/content.json",
			},
		},
		{
			name:    "content pack",
			fixture: ContentPack(),
			want: []string{
				"[CP] FishZones/manifest.json",
				"[CP] FishZones/content.json",
				"[CP] FishZones/assets/fish.png",
			},
		},
		{
			name:    "legacy XNB",
			fixture: LegacyXnb(),
			want:    []string{"Content/Animals/content.xnb", "Content/Maps/content.xnb"},
		},
		{
			name:    "nested archive",
			fixture: NestedArchive(),
			want:    []string{"SMAPI-4.5.2-installer.zip"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := tc.fixture.Bytes()
			if got := names(t, data); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("entries = %v, want %v", got, tc.want)
			}
			// Every corpus shape must be an archive the extractor accepts:
			// these are the fixtures the rest of the Phase 3 corpus reuses.
			result := extractInto(t, writeArchive(t, data), t.TempDir(), DefaultLimits(), nil)
			if result.Entries != len(tc.want) {
				t.Fatalf("Entries = %d, want %d", result.Entries, len(tc.want))
			}
		})
	}
}

// TestNestedArchiveHoldsAnArchive checks the shape the double-zipped corpus
// exists for: the outer entry's bytes are themselves a readable archive, and
// extraction never descends into them.
func TestNestedArchiveHoldsAnArchive(t *testing.T) {
	result := extractInto(t, writeFixture(t, NestedArchive()), t.TempDir(), DefaultLimits(), nil)
	inner, err := os.ReadFile(filepath.Join(result.StagePath, "SMAPI-4.5.2-installer.zip"))
	if err != nil {
		t.Fatalf("reading the nested entry: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(inner), int64(len(inner)))
	if err != nil {
		t.Fatalf("reading the nested archive: %v", err)
	}
	if len(reader.File) == 0 {
		t.Fatalf("the nested archive holds no entries, want the installer bundle")
	}
	if got := treeOf(t, result.StagePath); len(got) != 1 {
		t.Fatalf("stage = %v, want the nested archive to stay one file", got)
	}
}

// TestFixtureBytesAreFreshEveryCall is what lets one fixture be extracted twice
// and lets a test patch a copy without disturbing the next caller.
func TestFixtureBytesAreFreshEveryCall(t *testing.T) {
	fixture := SingleMod()
	first := fixture.Bytes()
	second := fixture.Bytes()
	if !bytes.Equal(first, second) {
		t.Fatalf("Bytes() produced different archives for one fixture")
	}
	first[0] ^= 0xff
	if bytes.Equal(first, fixture.Bytes()) {
		t.Fatalf("patching the bytes Bytes() returned changed the fixture")
	}
}

// TestFixtureWritesTheShapesTheWriterRefusesToMake sure the builder can state
// every entry kind the policy cares about, using the reader's view of the
// archive rather than the builder's own bookkeeping.
func TestFixtureWritesTheShapesTheWriterRefusesToMake(t *testing.T) {
	fixture := NewFixture().
		File("Mod/deflated.txt", "deflated").
		Stored("Mod/stored.txt", "stored").
		FileBytes("Mod/bytes.bin", []byte{0, 1, 2}).
		Dir("Mod/empty").
		Symlink("Mod/link", "../../target").
		Encrypted("Mod/secret.txt", "ciphertext").
		Nested("Mod/inner.zip", NewFixture().File("Mod/inner.txt", "inner"))

	reader, err := zip.NewReader(bytes.NewReader(fixture.Bytes()), int64(len(fixture.Bytes())))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	entries := map[string]*zip.File{}
	for _, entry := range reader.File {
		entries[entry.Name] = entry
	}

	if got := entries["Mod/deflated.txt"].Method; got != zip.Deflate {
		t.Fatalf("deflated.txt method = %d, want deflate", got)
	}
	if got := entries["Mod/stored.txt"].Method; got != zip.Store {
		t.Fatalf("stored.txt method = %d, want store", got)
	}
	if got := entries["Mod/empty/"]; got == nil || !got.Mode().IsDir() {
		t.Fatalf("empty/ = %+v, want a directory entry", got)
	}
	if got := entries["Mod/link"]; got == nil || got.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("link = %+v, want the unix symlink mode bit", got)
	}
	if got := entries["Mod/secret.txt"]; got == nil || got.Flags&0x1 == 0 {
		t.Fatalf("secret.txt = %+v, want the encrypted flag bit", got)
	}
	if got := entries["Mod/inner.zip"]; got == nil || got.UncompressedSize64 == 0 {
		t.Fatalf("inner.zip = %+v, want a nested archive as one file entry", got)
	}
}

// TestFixtureSynthesizesNamesTheWriterAcceptsButAnExtractorMustRefuse is the
// fixture library's one hard requirement: the hostile names the refusal tests
// are built from have to survive the writer verbatim, or those tests would be
// asserting against names that never reached the archive.
func TestFixtureSynthesizesNamesTheWriterAcceptsButAnExtractorMustRefuse(t *testing.T) {
	hostile := []string{
		"../escape.txt",
		"/absolute.txt",
		"C:/drive.txt",
		"C:relative.txt",
		`\\server\share\x.txt`,
		`\\?\C:\device.txt`,
		`back\slash.txt`,
		"nul\x00byte.txt",
		"empty//component.txt",
		"dot/./component.txt",
		"case/Readme.txt",
		"case/README.TXT",
		"duplicate.txt",
		"duplicate.txt",
		"NUL",
		"CON.txt",
		"trailing.",
		"trailing ",
	}
	fixture := NewFixture()
	for _, name := range hostile {
		fixture.File(name, "x")
	}
	data := fixture.Bytes()
	if got := names(t, data); !reflect.DeepEqual(got, hostile) {
		t.Fatalf("entries = %v, want every hostile name written verbatim", got)
	}
	// And the extractor refuses the archive rather than writing any of them.
	tempRoot := t.TempDir()
	stage := StageDir(tempRoot, "refused")
	_, err := Extract(t.Context(), writeArchive(t, data), stage, DefaultLimits(), nil)
	refusalOf(t, err, apperror.CodeArchivePathTraversal, "../escape.txt")
	if got := listing(t, tempRoot); len(got) != 0 {
		t.Fatalf("temp root = %v, want the refusal to write nothing", got)
	}
}

// TestFixtureWriteToMatchesBytes pins the file path and the in-memory path to
// the same archive.
func TestFixtureWriteToMatchesBytes(t *testing.T) {
	fixture := MultiMod()
	filePath := filepath.Join(t.TempDir(), "archive.zip")
	if err := fixture.WriteTo(filePath); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	written, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading what WriteTo wrote: %v", err)
	}
	if !bytes.Equal(written, fixture.Bytes()) {
		t.Fatalf("WriteTo wrote %d bytes, want the %d Bytes() produces", len(written), len(fixture.Bytes()))
	}
}

// TestFixtureChains proves the builder is chainable in the style the manifest
// fixture library established: one expression, one archive.
func TestFixtureChains(t *testing.T) {
	fixture := NewFixture().
		Dir("Group").
		Mod("Group/Alpha", "Alpha.Mod").
		Pack("Group/Pack", "Alpha.Pack", "Pathoschild.ContentPatcher").
		Xnb("Group/Legacy").
		File("Group/notes.txt", strings.Repeat("x", 8))
	if got := len(names(t, fixture.Bytes())); got != 7 {
		t.Fatalf("entries = %d, want the 7 the chain declares", got)
	}
}
