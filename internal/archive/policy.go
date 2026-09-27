// This file holds the whole-archive policy pass: everything that makes an
// archive refusable before a single byte of it reaches the stage, plus the
// canonical digest of what did reach it. Policy runs over zip.Reader.File —
// the raw central-directory records — and never over the fs.FS view, which
// silently rewrites names; it is pure inspection, so a refusal here leaves the
// archive untouched and nothing written.
//
// Two decisions shape the whole file. First, every violation refuses the
// archive as a whole and names the offending entry: nothing is renamed,
// repaired, dropped, or extracted "safely" around, because a package whose
// names the host cannot represent is a package the user cannot install and
// should be told about before anything is unpacked. Second, names are judged
// as the host filesystem will see them — Windows' rules, on every platform —
// so behaviour is identical on Windows, macOS, and Linux, and an inspection
// that passes on the developer's machine cannot quietly fail on a user's.
package archive

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Zendevve/astradew/internal/apperror"
)

// ratioGrace is the floor under the per-entry ratio cap. Apache POI's
// inflate-ratio guard (the only first-party ratio default in the field, 1%
// inflate ratio) grants slack before it trips; this package grants a fixed
// megabyte instead, because the false positive to avoid is not a large file but
// a small highly compressible one: a few hundred kilobytes of JSON or XML
// compresses 1000:1 and is a perfectly ordinary mod.
const ratioGrace = 1 << 20

// reservedDeviceNames are the Win32 names that never name a file: they open
// devices instead. The superscript digits are the variants Windows documents
// alongside COM1-9 and LPT1-9; CONIN$ and CONOUT$ open console handles.
var reservedDeviceNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"COM\u00b9": true, "COM\u00b2": true, "COM\u00b3": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
	"LPT\u00b9": true, "LPT\u00b2": true, "LPT\u00b3": true,
	"CONIN$": true, "CONOUT$": true,
}

// illegalNameBytes are the ASCII characters a Win32 name may not contain.
// Backslash and slash are absent because they are caught earlier, as
// separators, and NUL because it is caught before them.
const illegalNameBytes = `<>:"|?*`

// checkArchive runs the whole policy pass, in a fixed order, and returns the
// first refusal. Per-entry rules come first, so the entry that is actually
// wrong is named rather than whatever limit the archive as a whole exceeds;
// the archive-wide checks (entry count, then name collisions, which need the
// whole directory before they can be judged) come after.
func checkArchive(reader *zip.Reader, srcPath string, limits Limits) *apperror.AppError {
	for _, entry := range reader.File {
		if refusal := checkEntry(entry, limits); refusal != nil {
			return refusal
		}
	}
	if len(reader.File) > limits.MaxEntries {
		return archiveRefusal(apperror.CodeArchiveLimitExceeded, srcPath,
			fmt.Sprintf("%d entries exceeds the %d-entry limit", len(reader.File), limits.MaxEntries))
	}
	return checkNames(reader.File)
}

// checkEntry applies the per-entry rules in a fixed order: link, encryption,
// compression method, name, then the one malformed shape that is about an
// entry's content rather than its name.
func checkEntry(entry *zip.File, limits Limits) *apperror.AppError {
	// A symlink entry is refused wherever it points. The mode bit only reaches
	// us when the archive was written by a unix-like tool (creator 3 or 19):
	// Windows tooling writes a symlink as an ordinary file, which is exactly
	// what it becomes here as well.
	if entry.Mode()&fs.ModeSymlink != 0 {
		return entryRefusal(apperror.CodeArchiveLinkEntry, entry.Name,
			"symbolic links are never extracted, wherever they point")
	}
	// archive/zip never consults the encryption bit: it feeds ciphertext to
	// the decompressor, so an encrypted entry would surface as a bogus CRC or
	// decompression failure. Naming it for what it is matters more than the
	// error it would otherwise produce.
	if entry.Flags&0x1 != 0 {
		return entryRefusal(apperror.CodeArchiveEncrypted, entry.Name,
			"the entry is password protected, so its contents cannot be inspected")
	}
	if entry.Method != zip.Store && entry.Method != zip.Deflate {
		return entryRefusal(apperror.CodeArchiveCorrupt, entry.Name,
			fmt.Sprintf("compression method %d is not store or deflate", entry.Method))
	}
	if code, reason := namePolicy(entry.Name, limits); code != "" {
		return entryRefusal(code, entry.Name, reason)
	}
	// A directory carries no data in any well-formed archive (zip.Writer
	// itself refuses to produce one), and archive/zip's reader fails on the
	// first read; refusing it here means the failure names the entry instead
	// of arriving as an unexplained decompression error.
	if strings.HasSuffix(entry.Name, "/") && entry.UncompressedSize64 != 0 {
		return entryRefusal(apperror.CodeArchiveCorrupt, entry.Name,
			fmt.Sprintf("a directory entry declares %d bytes of data", entry.UncompressedSize64))
	}
	return nil
}

// namePolicy judges one raw entry name, returning the refusal code and reason
// or an empty code when the name is acceptable.
//
// The checks run in this order, and the order is observable: one trailing slash
// (the directory marker) is stripped first, because everything after it judges
// the path a host would create; then the checks that a whole path can fail
// without being split (NUL, a backslash separator, a rooted path, a drive
// form); then the two length budgets; then the components.
func namePolicy(raw string, limits Limits) (apperror.Code, string) {
	name := strings.TrimSuffix(raw, "/")
	if name == "" {
		return apperror.CodeArchivePathTraversal, "the name is empty"
	}
	if strings.IndexByte(name, 0) >= 0 {
		return apperror.CodeArchiveNameInvalid, "it contains a NUL byte"
	}
	// A backslash is a separator on Windows and an ordinary character
	// everywhere else, so a name using one means different paths on different
	// hosts. Refusing every backslash keeps the archive's meaning single.
	if strings.IndexByte(name, '\\') >= 0 {
		return apperror.CodeArchivePathTraversal, "it contains a backslash separator"
	}
	if strings.HasPrefix(name, "/") {
		return apperror.CodeArchivePathTraversal, "it is an absolute path"
	}
	if isDrivePath(name) {
		return apperror.CodeArchivePathTraversal, "it is a drive-relative or drive-absolute path"
	}
	if len(name) > limits.MaxPathBytes {
		return apperror.CodeArchiveLimitExceeded,
			fmt.Sprintf("its %d bytes exceed the %d-byte path limit", len(name), limits.MaxPathBytes)
	}
	components := strings.Split(name, "/")
	if len(components) > limits.MaxPathDepth {
		return apperror.CodeArchiveLimitExceeded,
			fmt.Sprintf("its %d components exceed the %d-component depth limit", len(components), limits.MaxPathDepth)
	}
	// An empty component, "." and ".." are all the same failure in different
	// spellings: the name does not describe a path below the extraction root.
	// `a//b` and `a/./b` are not merely unusual — the first is invalid on
	// every platform, the second means two different things depending on who
	// normalizes it.
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			return apperror.CodeArchivePathTraversal, fmt.Sprintf("its component %q is empty, dot, or dot-dot", component)
		}
	}
	for _, component := range components {
		if code, reason := componentPolicy(component); code != "" {
			return code, reason
		}
	}
	return "", ""
}

// componentPolicy judges one path component against the Win32 naming rules that
// decide whether a host can represent it at all. The byte-length cap is
// portable: Linux' NAME_MAX is 255 bytes and NTFS' is 255 UTF-16 units, so a
// component of at most 255 bytes fits both.
func componentPolicy(component string) (apperror.Code, string) {
	if len(component) > 255 {
		return apperror.CodeArchiveNameInvalid,
			fmt.Sprintf("its %d-byte component exceeds the 255-byte component limit", len(component))
	}
	for _, r := range component {
		if r < utf8.RuneSelf && strings.ContainsRune(illegalNameBytes, r) {
			return apperror.CodeArchiveNameInvalid, fmt.Sprintf("it contains the illegal character %q", r)
		}
		if unicode.IsControl(r) {
			return apperror.CodeArchiveNameInvalid, fmt.Sprintf("it contains the control character %q", r)
		}
	}
	// A name that is not valid UTF-8 is a name no host can hold faithfully:
	// Windows replaces its bytes with U+FFFD when it creates the file, macOS
	// refuses to create it at all, and the fs.FS seam the classification layer
	// reads the stage through cannot spell it. Refusing it is what keeps "the
	// bytes in the archive are the names on disk" true, and keeps an archive
	// accepted on one platform from being a different archive on another.
	if !utf8.ValidString(component) {
		return apperror.CodeArchiveNameInvalid, "it is not valid UTF-8, so no host can hold it faithfully"
	}
	if strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
		return apperror.CodeArchiveNameInvalid,
			"it ends in a dot or a space, which Windows strips, so it would silently rename the file"
	}
	if isReservedDeviceName(component) {
		return apperror.CodeArchiveNameInvalid,
			fmt.Sprintf("component %q is a reserved Windows device name", component)
	}
	return "", ""
}

// isDrivePath reports whether a name is rooted in a drive: `C:x` (drive
// relative) or `C:/x` (drive absolute). Both mean something different on
// Windows and nothing at all elsewhere, which is exactly why neither is
// extracted.
func isDrivePath(name string) bool {
	if len(name) < 2 || name[1] != ':' {
		return false
	}
	return ('a' <= name[0] && name[0] <= 'z') || ('A' <= name[0] && name[0] <= 'Z')
}

// isReservedDeviceName reports whether one component names a Win32 device. The
// rule follows the documented one: the check is case-insensitive on the base
// name, with the extension stripped — `NUL.txt` and `NUL.tar.gz` are both NUL
// as far as Windows is concerned — and trailing spaces are trimmed first, since
// the object manager trims them too.
func isReservedDeviceName(component string) bool {
	base := component
	if i := strings.IndexAny(base, ".:"); i >= 0 {
		base = base[:i]
	}
	base = strings.TrimRight(base, " ")
	return reservedDeviceNames[strings.ToUpper(base)]
}

// checkNames refuses an archive whose entries cannot all coexist in one
// directory tree, whatever the host. Four collisions are possible, checked in
// this order because the later checks subsume the earlier ones when both would
// fire:
//
//   - exact duplicates, compared with one trailing slash stripped, so `a` and
//     `a/` collide as well as `a` and `a`;
//   - names equal under Unicode simple case folding, which collide on NTFS and
//     on a default APFS volume;
//   - directory names equal under the same folding: `Mod/A/x` and `Mod/a/y`
//     are one directory on a case-insensitive host and two on a case-sensitive
//     one, so the same archive would extract into different trees, and the
//     paths the result reports would not all exist on disk;
//   - a file whose path is also a directory of another entry, which no
//     single-directory-tree representation can hold: Windows cannot have both
//     `a` as a file and `a/b` as a path. This one is folding-aware too, because
//     `Mod/thing` as a file and `Mod/THING/x` as a directory is the same
//     collision in another spelling.
//
// All of them are refused unconditionally on every host, so behaviour is
// identical on Windows, macOS, and Linux: an archive accepted on Linux must not
// be the archive that silently loses a file — or reports a path that does not
// exist — on the user's machine.
//
// One collision is deliberately not detected: names that differ only by Unicode
// normalization, which a default APFS volume treats as one name. The standard
// library has no normalizer, so a case like that is left to the host.
func checkNames(files []*zip.File) *apperror.AppError {
	exact := make(map[string]string, len(files))
	folded := make(map[string]string, len(files))
	for _, entry := range files {
		name := strings.TrimSuffix(entry.Name, "/")
		if first, ok := exact[name]; ok {
			return entryRefusal(apperror.CodeArchiveNameInvalid, entry.Name,
				fmt.Sprintf("it duplicates entry %q", first))
		}
		exact[name] = entry.Name
		key := foldKey(name)
		if first, ok := folded[key]; ok {
			return entryRefusal(apperror.CodeArchiveNameInvalid, entry.Name,
				fmt.Sprintf("its name collides with entry %q under Unicode case folding, which no host can represent consistently", first))
		}
		folded[key] = entry.Name
	}

	// Directories, explicit or implied: every ancestor of every entry, plus
	// every directory entry itself. Keyed by folded name so two spellings of
	// one directory are caught, and recording the entry that first needed it,
	// which is what a refusal names alongside the second spelling.
	type directory struct{ path, entry string }
	directories := make(map[string]directory, len(files))
	record := func(path, entry string) *apperror.AppError {
		key := foldKey(path)
		if first, ok := directories[key]; ok {
			if first.path == path {
				return nil
			}
			return entryRefusal(apperror.CodeArchiveNameInvalid, entry,
				fmt.Sprintf("its directory %q differs only in case from %q, which entry %q needs, and no host can hold both", path, first.path, first.entry))
		}
		directories[key] = directory{path: path, entry: entry}
		return nil
	}
	for _, entry := range files {
		if strings.HasSuffix(entry.Name, "/") {
			if refusal := record(strings.TrimSuffix(entry.Name, "/"), entry.Name); refusal != nil {
				return refusal
			}
		}
	}
	for _, entry := range files {
		name := strings.TrimSuffix(entry.Name, "/")
		for i := range len(name) {
			if name[i] != '/' {
				continue
			}
			if refusal := record(name[:i], entry.Name); refusal != nil {
				return refusal
			}
		}
	}
	for _, entry := range files {
		if strings.HasSuffix(entry.Name, "/") {
			continue
		}
		if first, ok := directories[foldKey(strings.TrimSuffix(entry.Name, "/"))]; ok {
			return entryRefusal(apperror.CodeArchiveNameInvalid, entry.Name,
				fmt.Sprintf("it is a file, and entry %q needs %q to be a directory", first.entry, first.path))
		}
	}
	return nil
}

// packageDigest folds the sorted per-file digests into the package digest ADR
// 0009's content-addressed store will key on: for each file, in ascending
// relative-path byte order, the path bytes, a NUL, the lowercase hex per-file
// SHA-256, a NUL, the decimal byte size, and a newline. Empty directories,
// mtimes, and modes contribute nothing, so two hosts that unpacked the same
// archive produce the same digest, and a re-imported identical archive can
// dedupe. The NUL separators are what make the listing unambiguous: no path or
// hex digest contains one, so no sequence of files can spell another's listing.
func packageDigest(files []FileDigest) string {
	digest := sha256.New()
	var line []byte
	for _, file := range files {
		line = append(line[:0], file.Path...)
		line = append(line, 0)
		line = append(line, file.SHA256...)
		line = append(line, 0)
		line = strconv.AppendInt(line, file.Size, 10)
		line = append(line, '\n')
		digest.Write(line)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// foldKey is a name's key under Unicode simple case folding, which is what
// decides whether two names collide on a case-insensitive host. It is not
// strings.ToLower: simple folding maps the Greek final sigma to sigma and the
// Kelvin sign to k, so `Σ.txt` and `ς.txt` collide here while ToLower keeps
// them apart — the difference between refusing an archive Windows would
// mangle and accepting one it would silently lose a file from.
//
// The key is the smallest rune of each folding cycle, which is a canonical
// representative; ASCII letters fold to uppercase for the same reason, since
// 'A' sorts before 'a' in the cycle. Bytes that are not valid UTF-8 pass
// through unchanged rather than being folded to U+FFFD: the name policy has
// already refused such names, so this is only here to keep the fold honest if
// it is ever reached with one.
func foldKey(name string) string {
	key := make([]byte, 0, len(name))
	for i := 0; i < len(name); {
		r, size := utf8.DecodeRuneInString(name[i:])
		if r == utf8.RuneError && size == 1 {
			key = append(key, name[i])
			i++
			continue
		}
		i += size
		if r < utf8.RuneSelf {
			if 'a' <= r && r <= 'z' {
				r -= 'a' - 'A'
			}
		} else {
			for fold := unicode.SimpleFold(r); fold != r; fold = unicode.SimpleFold(fold) {
				if fold < r {
					r = fold
				}
			}
		}
		key = utf8.AppendRune(key, r)
	}
	return string(key)
}
