// This file holds the package's two fuzz targets. FuzzExtract drives arbitrary
// bytes through the real seam — the only thing that can prove no archive can
// panic the extractor or make it write somewhere it should not — and
// FuzzEntryName drives the name policy directly, because the policy is the part
// that must hold for names no archive writer would ever produce. The seeds are
// the fixture corpus, so every seed also runs as an ordinary test.
package archive

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zendevve/astradew/internal/apperror"
)

func FuzzExtract(f *testing.F) {
	for _, fixture := range []*Fixture{
		SingleMod(),
		Wrapper(),
		MultiMod(),
		ContentPack(),
		LegacyXnb(),
		NestedArchive(),
		NewFixture().File("../escape.txt", "x"),
		NewFixture().File("Mod/manifest.json", "{}").Symlink("Mod/link", "/etc/passwd"),
		NewFixture().Encrypted("Mod/secret.txt", "x"),
		NewFixture().Dir("Mod/empty").File("Mod/Mod.dll", "MZ"),
	} {
		f.Add(fixture.Bytes())
	}
	f.Add([]byte{})
	f.Add([]byte("this is not a zip file at all"))
	f.Add(SingleMod().Bytes()[:64])

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip("the input is larger than the fuzz budget allows")
		}
		srcPath := filepath.Join(t.TempDir(), "archive.zip")
		if err := os.WriteFile(srcPath, data, 0o644); err != nil {
			t.Fatalf("writing the fuzzed archive: %v", err)
		}
		srcHash := fileSHA256(t, srcPath)

		// The limits are tightened rather than relaxed, twice over: a
		// fuzzer's inputs are arbitrary, so a generous expansion budget would
		// let one execution write gigabytes, and a generous entry budget would
		// let it spend seconds creating files. The point is that no input
		// escapes the stage, not that a big one extracts, and small limits
		// keep the fuzzer exploring shapes instead of bytes.
		limits := DefaultLimits()
		limits.MaxArchiveBytes = 2 << 20
		limits.MaxExpandedBytes = 8 << 20
		limits.MaxEntries = 256

		tempRoot := t.TempDir()
		stage := StageDir(tempRoot, "fuzzed")
		result, err := Extract(t.Context(), srcPath, stage, limits, nil)

		// Whatever happened, nothing may exist outside the stage: the temp
		// root gains the inspection area and nothing else, and that area gains
		// this run's stage and nothing else.
		assertOnlyNew(t, tempRoot, nil, "inspect")
		assertOnlyNew(t, filepath.Dir(stage), nil, filepath.Base(stage))
		if got := fileSHA256(t, srcPath); got != srcHash {
			t.Fatalf("the run changed its source archive: %q, want %q", got, srcHash)
		}
		if err != nil {
			// Whatever an arbitrary input produces, the caller must get one of
			// the package's typed refusals — or the context's own error, which
			// is the caller's cancellation rather than a refusal. A raw error
			// escaping here would be the "something went wrong" the PRD
			// forbids, and only the fuzzer reaches the shapes that could
			// produce one.
			var refusal *apperror.AppError
			if !errors.As(err, &refusal) || refusal.Code == "" {
				if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("Extract() error = %v, want a typed refusal with a code or a context error", err)
				}
			}
			return
		}
		// A successful run must report exactly the tree it wrote: one digest
		// per file, sizes and hashes that match the bytes on disk, and a
		// package digest that is a function of those files alone.
		tree := treeOf(t, result.StagePath)
		if len(tree) != len(result.Files) {
			t.Fatalf("Files = %d entries, want the %d files in the stage", len(result.Files), len(tree))
		}
		var expanded int64
		for _, file := range result.Files {
			content, ok := tree[file.Path]
			if !ok {
				t.Fatalf("Files names %q, which is not in the stage (%v)", file.Path, tree)
			}
			if int64(len(content)) != file.Size || fileSHA256(t, filepath.Join(result.StagePath, filepath.FromSlash(file.Path))) != file.SHA256 {
				t.Fatalf("Files[%q] = %+v, want the size and hash of the bytes on disk", file.Path, file)
			}
			expanded += file.Size
		}
		if result.ExpandedBytes != expanded {
			t.Fatalf("ExpandedBytes = %d, want the %d bytes its files add up to", result.ExpandedBytes, expanded)
		}
		if result.Entries < len(result.Files) {
			t.Fatalf("Entries = %d, want at least the %d files it extracted", result.Entries, len(result.Files))
		}
	})
}

func FuzzEntryName(f *testing.F) {
	for _, name := range []string{
		"Mod/manifest.json",
		"Mod/empty/",
		"",
		"/absolute.txt",
		"../escape.txt",
		"Mod/../../escape.txt",
		`Mod\sub\x.txt`,
		`\\server\share\x.txt`,
		`\\?\C:\x.txt`,
		"C:/drive.txt",
		"C:relative.txt",
		"Mod//x.txt",
		"Mod/./x.txt",
		"Mod/../x.txt",
		"NUL",
		"CON.txt",
		"CONIN$",
		"Mod/name.",
		"Mod/name ",
		"Mod/na<me.txt",
		"Mod/bad\x00name.txt",
		strings.Repeat("a", 300),
		strings.Repeat("d/", 33) + "x.txt",
		"Mod/\u03a3.txt",
	} {
		f.Add(name)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		limits := DefaultLimits()
		code, reason := namePolicy(raw, limits)
		switch code {
		case "", apperror.CodeArchivePathTraversal, apperror.CodeArchiveNameInvalid, apperror.CodeArchiveLimitExceeded:
		default:
			t.Fatalf("namePolicy(%q) = %s, want one of the name-policy codes", raw, code)
		}
		if code == "" && reason != "" {
			t.Fatalf("namePolicy(%q) accepted the name with the reason %q", raw, reason)
		}
		if code != "" {
			return
		}

		// An accepted name must be one the host can represent and the
		// extractor can create: a valid relative fs path, inside every
		// budget, and creatable through os.Root on this machine. This is the
		// property the whole policy exists for, so it is asserted rather than
		// assumed.
		name := strings.TrimSuffix(raw, "/")
		if !fs.ValidPath(name) {
			t.Fatalf("namePolicy accepted %q, which is not a valid relative fs path", raw)
		}
		if len(name) > limits.MaxPathBytes {
			t.Fatalf("namePolicy accepted %q with %d bytes, over the %d-byte limit", raw, len(name), limits.MaxPathBytes)
		}
		components := strings.Split(name, "/")
		if len(components) > limits.MaxPathDepth {
			t.Fatalf("namePolicy accepted %q with %d components, over the %d-component limit", raw, len(components), limits.MaxPathDepth)
		}
		for _, component := range components {
			switch {
			case component == "" || component == "." || component == "..":
				t.Fatalf("namePolicy accepted %q with the component %q", raw, component)
			case len(component) > 255:
				t.Fatalf("namePolicy accepted %q with a %d-byte component", raw, len(component))
			case strings.HasSuffix(component, ".") || strings.HasSuffix(component, " "):
				t.Fatalf("namePolicy accepted %q with the component %q", raw, component)
			case isReservedDeviceName(component):
				t.Fatalf("namePolicy accepted %q, whose component %q is a device name", raw, component)
			}
		}
		if strings.Contains(name, `\`) || strings.IndexByte(name, 0) >= 0 {
			t.Fatalf("namePolicy accepted %q, which a host would read as another path", raw)
		}

		root, err := os.OpenRoot(t.TempDir())
		if err != nil {
			t.Fatalf("opening a temp root: %v", err)
		}
		defer root.Close()
		if parent := path.Dir(name); parent != "." {
			if err := root.MkdirAll(parent, 0o755); err != nil {
				t.Fatalf("namePolicy accepted %q, whose parent the filesystem cannot create: %v", raw, err)
			}
		}
		file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			t.Fatalf("namePolicy accepted %q, which the filesystem cannot create: %v", raw, err)
		}
		file.Close()
	})
}
