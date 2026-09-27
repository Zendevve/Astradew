// This file holds the scaffolding the package's test files share: writing a
// fixture to disk, listing a directory, walking a stage, recording progress,
// and the header patching that produces the few archive shapes archive/zip's
// writer refuses to write. It is support, not a test: every helper here is used
// by more than one of the other files.
package archive

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Zendevve/astradew/internal/apperror"
)

// writeFixture writes a fixture's bytes to a fresh file and returns its path.
func writeFixture(t *testing.T, fixture *Fixture) string {
	t.Helper()
	return writeArchive(t, fixture.Bytes())
}

// writeArchive writes raw archive bytes to a fresh file and returns its path.
// Patched archives take this route, which is why it is separate from
// writeFixture.
func writeArchive(t *testing.T, data []byte) string {
	t.Helper()
	filePath := filepath.Join(t.TempDir(), "archive.zip")
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		t.Fatalf("writing the archive: %v", err)
	}
	return filePath
}

// extractInto extracts srcPath into a fresh stage under tempRoot and fails the
// test if the run does not succeed.
func extractInto(t *testing.T, srcPath, tempRoot string, limits Limits, onProgress func(Progress)) Result {
	t.Helper()
	result, err := Extract(t.Context(), srcPath, StageDir(tempRoot, "inspection"), limits, onProgress)
	if err != nil {
		t.Fatalf("Extract(%q) = %v, want success", srcPath, err)
	}
	return result
}

// listing returns the sorted names directly under dir. A missing directory
// lists nothing, which is what the "before" side of a stage assertion needs.
func listing(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("listing %q: %v", dir, err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	sort.Strings(out)
	return out
}

// assertOnlyNew asserts that dir gained nothing except the allowed names: the
// proof that a run wrote where it said it would and nowhere else.
func assertOnlyNew(t *testing.T, dir string, before []string, allowed ...string) {
	t.Helper()
	known := make(map[string]bool, len(before)+len(allowed))
	for _, name := range before {
		known[name] = true
	}
	for _, name := range allowed {
		known[name] = true
	}
	for _, name := range listing(t, dir) {
		if !known[name] {
			t.Fatalf("listing %q = %v, want nothing beyond %v", dir, listing(t, dir), known)
		}
	}
}

// treeOf returns every file under dir as slash-separated relative path to
// content.
func treeOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(dir, filePath)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = string(content)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %q: %v", dir, err)
	}
	return files
}

// dirsOf returns every directory under dir, relative and slash-separated,
// sorted; the root itself is not included.
func dirsOf(t *testing.T, dir string) []string {
	t.Helper()
	var dirs []string
	err := filepath.WalkDir(dir, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() || filePath == dir {
			return nil
		}
		relative, err := filepath.Rel(dir, filePath)
		if err != nil {
			return err
		}
		dirs = append(dirs, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		t.Fatalf("walking %q: %v", dir, err)
	}
	sort.Strings(dirs)
	return dirs
}

// names returns an archive's raw central-directory names, in order: the
// fixture's own statement of what it holds, independent of any extraction.
func names(t *testing.T, data []byte) []string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("reading the archive: %v", err)
	}
	out := make([]string, 0, len(reader.File))
	for _, entry := range reader.File {
		out = append(out, entry.Name)
	}
	return out
}

// fileSHA256 returns a file's lowercase hex SHA-256, or "" when the file cannot
// be read (the unreadable-source cases have nothing to hash).
func fileSHA256(t *testing.T, filePath string) string {
	t.Helper()
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// progressLog records every progress event, in order.
type progressLog struct {
	events []Progress
}

// record is the callback Extract is given.
func (l *progressLog) record(event Progress) {
	l.events = append(l.events, event)
}

// phase returns the events of one phase, in order.
func (l *progressLog) phase(phase Phase) []Progress {
	var out []Progress
	for _, event := range l.events {
		if event.Phase == phase {
			out = append(out, event)
		}
	}
	return out
}

// refusalOf asserts that err is the typed refusal with wantCode whose Details
// name wantSubject, and returns it. The subject is accepted either verbatim or
// in the quoted form the details use for a name that may hold control
// characters, so a Windows path with backslashes and a hostile entry name are
// both assertable.
func refusalOf(t *testing.T, err error, wantCode apperror.Code, wantSubject string) *apperror.AppError {
	t.Helper()
	var refusal *apperror.AppError
	if !errors.As(err, &refusal) {
		t.Fatalf("err = %v, want an *apperror.AppError with code %s", err, wantCode)
	}
	if refusal.Code != wantCode {
		t.Fatalf("code = %s (%v), want %s", refusal.Code, refusal, wantCode)
	}
	if !refusal.Recoverable {
		t.Fatalf("refusal %v is not recoverable, want every archive refusal recoverable", refusal)
	}
	if !strings.Contains(refusal.Details, wantSubject) && !strings.Contains(refusal.Details, strconv.Quote(wantSubject)) {
		t.Fatalf("details = %q, want it to name %q", refusal.Details, wantSubject)
	}
	return refusal
}

// The signatures that mark a local file header and a central directory record.
const (
	localHeaderSignature   = 0x04034b50
	centralHeaderSignature = 0x02014b50
)

// entryHeaders returns the offsets of one entry's local and central headers.
// The name is read out of each candidate record, so a signature byte pair
// inside an entry's data cannot be mistaken for a header.
func entryHeaders(t *testing.T, data []byte, name string) (local, central int) {
	t.Helper()
	local, central = -1, -1
	for i := 0; i+4 <= len(data); i++ {
		switch binary.LittleEndian.Uint32(data[i:]) {
		case localHeaderSignature:
			if local >= 0 || i+30 > len(data) {
				continue
			}
			nameLen := int(binary.LittleEndian.Uint16(data[i+26:]))
			if i+30+nameLen <= len(data) && string(data[i+30:i+30+nameLen]) == name {
				local = i
			}
		case centralHeaderSignature:
			if central >= 0 || i+46 > len(data) {
				continue
			}
			nameLen := int(binary.LittleEndian.Uint16(data[i+28:]))
			if i+46+nameLen <= len(data) && string(data[i+46:i+46+nameLen]) == name {
				central = i
			}
		}
	}
	if local < 0 || central < 0 {
		t.Fatalf("entry %q not found in the archive (local %d, central %d)", name, local, central)
	}
	return local, central
}

// patchEntryMethod rewrites one entry's compression method in both headers.
// archive/zip's writer refuses to produce this shape — it looks the compressor
// up as the entry is created — so a hostile method can only be made by hand.
func patchEntryMethod(t *testing.T, data []byte, name string, method uint16) []byte {
	t.Helper()
	local, central := entryHeaders(t, data, name)
	out := bytes.Clone(data)
	binary.LittleEndian.PutUint16(out[local+8:], method)
	binary.LittleEndian.PutUint16(out[central+10:], method)
	return out
}

// patchEntryDeclaredSize rewrites one entry's declared uncompressed size in
// both headers, which is how a directory that claims data — and an entry whose
// data cannot satisfy its own declaration — is synthesized.
func patchEntryDeclaredSize(t *testing.T, data []byte, name string, size uint32) []byte {
	t.Helper()
	local, central := entryHeaders(t, data, name)
	out := bytes.Clone(data)
	binary.LittleEndian.PutUint32(out[local+22:], size)
	binary.LittleEndian.PutUint32(out[central+24:], size)
	return out
}

// patchEntryCompressedSize rewrites one entry's declared compressed size in
// both headers. Nothing checks that declaration against the bytes the archive
// actually holds, so a hostile archive can claim an entry occupies far more of
// the file than it does — the shape a ratio budget must not be computed from.
func patchEntryCompressedSize(t *testing.T, data []byte, name string, size uint32) []byte {
	t.Helper()
	local, central := entryHeaders(t, data, name)
	out := bytes.Clone(data)
	binary.LittleEndian.PutUint32(out[local+18:], size)
	binary.LittleEndian.PutUint32(out[central+20:], size)
	return out
}

// flipStoredByte flips one byte of a stored entry's data and leaves every
// header alone, so the CRC the central directory declares no longer matches the
// bytes the reader gets.
func flipStoredByte(t *testing.T, data []byte, name string) []byte {
	t.Helper()
	local, _ := entryHeaders(t, data, name)
	nameLen := int(binary.LittleEndian.Uint16(data[local+26:]))
	extraLen := int(binary.LittleEndian.Uint16(data[local+28:]))
	at := local + 30 + nameLen + extraLen
	if at >= len(data) {
		t.Fatalf("entry %q has no data to flip", name)
	}
	out := bytes.Clone(data)
	out[at] ^= 0xff
	return out
}
