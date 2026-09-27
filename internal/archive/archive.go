// Package archive owns everything that touches ZIP bytes and the staging
// filesystem: pointing it at a downloaded .zip hashes the file in place over a
// read-only handle, refuses a hostile or malformed archive before anything is
// written, unpacks the survivors entry by entry into a staged directory through
// an os.Root handle, and reports what came out — the archive's size and
// SHA-256, the package digest of the extracted tree, a per-file digest for
// every file, entry and expanded-byte counts, and progress for the hashing and
// extracting phases. Nothing outside the stage is ever written, and the source
// archive is never modified, moved, or deleted.
//
// The seam is Extract, one call. It cleans up its own partial stage on failure;
// the caller owns the finished stage and removes it when the inspection is over
// (StageDir and SweepStages define and tidy that layout). Nothing here knows
// about Wails, SQLite, tasks, or the frontend: classifying what was extracted
// is internal/inspect's job, not this package's.
//
// The package deliberately deviates from archive/zip, and from what a
// general-purpose unzip does, in these ways:
//
//   - Policy is our own pass over zip.Reader.File, using the RAW header name
//     bytes. The fs.FS view is never consulted: toValidName silently rewrites
//     backslashes, strips leading slashes and `..` segments, and drops names
//     made only of separators, so a policy checked over it would neither see
//     nor report the hostile spelling. File.OpenRaw is never used either: it
//     skips decompression, the size check, and the CRC check.
//
//   - A violation refuses the whole archive on the first entry that breaks a
//     rule, naming that entry. archive/zip itself extracts anything it can
//     parse, and comparable tools sanitize names instead; nothing here is ever
//     silently renamed or repaired.
//
//   - Symlink entries are refused wholesale, wherever they point. PRD section
//     13 asks only that symlinks escaping the extraction root be refused; a
//     wholesale refusal is stricter, and is what makes "no link is ever
//     created" a property of the code rather than of the target check.
//
//   - Encrypted entries are detected from the general-purpose bit 0, which
//     archive/zip never consults: it feeds the ciphertext to the decompressor
//     and surfaces whatever that yields.
//
//   - Failure needs both a CRC match — when the entry declares a CRC at all —
//     and a final size that matches the declared uncompressed size.
//     archive/zip only verifies the CRC when a reader reaches EOF, and skips it
//     entirely when the header declares zero, so without the size comparison a
//     truncated copy could pass as success.
//
//   - Entry permission bits and mtimes are not preserved: files land 0o644 and
//     directories 0o755, because the mode of an entry in a downloaded archive
//     describes the author's machine, not anything this product promises. The
//     one mode bit that is honoured is the symlink bit, as a refusal.
//
//   - Names are not decoded, but they must be valid UTF-8. CP437 (or any other
//     legacy encoding) entry names are refused rather than transcoded: Windows
//     would silently replace their bytes with U+FFFD when it created the file,
//     macOS would refuse to create it at all, and the fs.FS seam the
//     classification layer reads the stage through cannot spell such a name.
//     Refusing is what keeps the archive's names and the stage's names the same
//     on every host, and what keeps "nothing is ever silently renamed" true.
package archive

import (
	"fmt"

	"github.com/Zendevve/astradew/internal/apperror"
)

// Limits bounds what inspecting one archive may cost: how big the source may
// be, how much it may expand to, how many entries it may carry, how far any one
// entry may compress, and how long and deep any entry name may be. Every field
// is also a settings key, so a user can raise a limit for an archive they trust.
//
// A zero or negative field means "use DefaultLimits for this field", which
// makes a partly-filled Limits the natural way to tune one limit; callers with
// no opinion pass DefaultLimits() (or the zero Limits).
type Limits struct {
	// MaxArchiveBytes caps the source archive before it is parsed. It is the
	// only bound on central-directory heap: archive/zip parses the whole
	// directory inside zip.NewReader and offers no API to limit how much
	// header data it reads, so the file's size is what keeps a malicious
	// directory from costing unbounded memory.
	MaxArchiveBytes int64
	// MaxExpandedBytes caps the decompressed bytes actually written to the
	// stage, cumulatively over the whole archive.
	MaxExpandedBytes int64
	// MaxEntries caps len(zip.Reader.File).
	MaxEntries int
	// MaxRatio caps one entry's declared uncompressed:compressed ratio after a
	// fixed grace (see ratioGrace). The declared size is an upper bound on what
	// an entry can yield, so the ratio is checked before anything is read.
	MaxRatio int
	// MaxPathBytes caps an entry name's raw bytes, as written in the archive.
	// The default keeps names clear of Windows' long-path threshold, where
	// path semantics change rather than merely a limit being hit.
	MaxPathBytes int
	// MaxPathDepth caps an entry name's component count.
	MaxPathDepth int
}

// The default limits. The numbers follow the survey in
// docs/phase3-research/zip-semantics.md section 8: ClamAV's per-file /
// cumulative / count model for the byte and entry budgets (the closest
// structural fit for the safety list), Apache POI's inflate-ratio guard for the
// ratio — the only first-party ratio default found in the field — and the
// Windows name rules for the path budgets.
const (
	// defaultMaxArchiveBytes is 2 GiB: larger than any real mod release (the
	// biggest sampled one is 42 MB). It is the only bound on the heap
	// archive/zip may spend before the policy pass runs, so it is deliberately
	// generous rather than tuned; a user who handles no such archives can lower
	// it, and the price of the default is that a hostile file of that size can
	// cost several times its own size in central-directory records.
	defaultMaxArchiveBytes int64 = 2 << 30
	// defaultMaxExpandedBytes is 4 GiB: the point past which a Stardew mod
	// package is a bomb rather than a package, whatever it contains.
	defaultMaxExpandedBytes int64 = 4 << 30
	// defaultMaxEntries is 20,000: real mod archives hold tens of files; a
	// package folder shipped as a merged megapack holds thousands.
	defaultMaxEntries = 20_000
	// defaultMaxRatio is 100:1 after the grace. High enough that any real
	// text, JSON, or XML payload passes, low enough that the classic
	// zeros-are-free bomb trips immediately.
	defaultMaxRatio = 100
	// defaultMaxPathBytes is 240: the 260-byte MAX_PATH budget less room for
	// the stage root and an 8.3 name, so extraction never crosses into
	// Windows' \\?\ semantics, where `.`/`..` stop being expanded and
	// trailing dots and spaces stop being trimmed.
	defaultMaxPathBytes = 240
	// defaultMaxPathDepth is 32: deeper than any real package nests.
	defaultMaxPathDepth = 32
)

// DefaultLimits returns the limits Extract applies to any field the caller left
// unset, and the values the settings registry defaults to.
func DefaultLimits() Limits {
	return Limits{
		MaxArchiveBytes:  defaultMaxArchiveBytes,
		MaxExpandedBytes: defaultMaxExpandedBytes,
		MaxEntries:       defaultMaxEntries,
		MaxRatio:         defaultMaxRatio,
		MaxPathBytes:     defaultMaxPathBytes,
		MaxPathDepth:     defaultMaxPathDepth,
	}
}

// withDefaults fills every field a caller left unset. A zero limit would
// otherwise mean "refuse everything", which is never what a caller means.
func (l Limits) withDefaults() Limits {
	defaults := DefaultLimits()
	if l.MaxArchiveBytes <= 0 {
		l.MaxArchiveBytes = defaults.MaxArchiveBytes
	}
	if l.MaxExpandedBytes <= 0 {
		l.MaxExpandedBytes = defaults.MaxExpandedBytes
	}
	if l.MaxEntries <= 0 {
		l.MaxEntries = defaults.MaxEntries
	}
	if l.MaxRatio <= 0 {
		l.MaxRatio = defaults.MaxRatio
	}
	if l.MaxPathBytes <= 0 {
		l.MaxPathBytes = defaults.MaxPathBytes
	}
	if l.MaxPathDepth <= 0 {
		l.MaxPathDepth = defaults.MaxPathDepth
	}
	return l
}

// Phase names one stage of an inspection for a progress display.
type Phase string

const (
	// PhaseHashing is the sequential read of the source archive, in place,
	// over the read-only handle.
	PhaseHashing Phase = "hashing"
	// PhaseExtracting is the entry-by-entry write into the stage.
	PhaseExtracting Phase = "extracting"
	// PhaseScanning belongs to the classification layer that consumes a
	// finished stage; Extract never emits it. It is named here so every phase
	// a caller can display is defined in one place.
	PhaseScanning Phase = "scanning"
)

// Progress is one progress event of a run.
type Progress struct {
	// Phase is the stage the numbers describe.
	Phase Phase
	// Current is how far that phase has got: bytes hashed, or bytes written
	// to the stage so far.
	Current int64
	// Total is what Current counts toward. For PhaseHashing it is the
	// source's stat size. For PhaseExtracting it is the sum of the entries'
	// declared uncompressed sizes, which a hostile archive can overstate —
	// it is a progress denominator, never a budget.
	Total int64
	// Message is a short human sentence for the status line.
	Message string
}

// FileDigest identifies one extracted file by content.
type FileDigest struct {
	// Path is the file's path relative to the stage root, slash-separated.
	Path string
	// Size is the file's size in bytes, as written.
	Size int64
	// SHA256 is the lowercase hex SHA-256 of the file's bytes.
	SHA256 string
}

// Result is what one successful extraction produced. It is the whole product of
// the extraction seam: what the archive is, what came out of it, and where it
// landed.
type Result struct {
	// StagePath is the directory the entries were written to, exactly as the
	// caller passed it. The caller owns it and removes it when the inspection
	// ends.
	StagePath string
	// SizeBytes is the source archive's size, taken from the stat that bounded
	// the read and re-checked afterwards.
	SizeBytes int64
	// ArchiveSHA256 is the lowercase hex SHA-256 of the whole source file.
	ArchiveSHA256 string
	// PackageSHA256 is the canonical-listing digest of the extracted tree: the
	// identity ADR 0009's content-addressed store keys on. Identical tree
	// bytes produce the same digest on every host.
	PackageSHA256 string
	// Entries is len(zip.Reader.File) for the archive that was extracted:
	// files and directories alike, as the central directory declares them. A
	// Result only ever exists for an archive that passed the whole policy
	// pass, so this is also the number of entries the stage holds (plus its
	// implicit ancestor directories).
	Entries int
	// ExpandedBytes is the total size of the files written to the stage. It
	// equals the sum of Files' sizes.
	ExpandedBytes int64
	// Files holds one digest per extracted file, sorted ascending by Path.
	// Empty directories have no digest and contribute nothing.
	Files []FileDigest
}

// The refusals Extract is defined to return. Each code is fixed by
// internal/apperror; these messages are stable short sentences a UI can show,
// and Details always adds the offending entry — or, for a failure that belongs
// to the file as a whole, the archive path. Consumers branch on the code and
// read Details for the subject; they must never branch on either sentence.
const (
	msgUnreadable       = "the archive could not be read"
	msgPathTraversal    = "the archive contains an entry that could escape the staging directory"
	msgLinkEntry        = "the archive contains a symbolic link entry"
	msgEncrypted        = "the archive contains an encrypted entry"
	msgNameInvalid      = "the archive contains a name the filesystem cannot represent"
	msgLimitExceeded    = "the archive exceeds a safety limit"
	msgCorrupt          = "the archive is corrupt"
	msgInspectionFailed = "the staging directory could not be prepared"

	// reasonChanged is the reason shared by both halves of the same problem:
	// the archive the caller pointed at is not the archive that was read.
	reasonChanged = "the file changed while it was being inspected"
)

// refusalMessage is the stable sentence for a refusal code. The codes here are
// exactly the ones this package returns; any other code falls back to its own
// text so a future addition cannot silently lose its message.
func refusalMessage(code apperror.Code) string {
	switch code {
	case apperror.CodeArchiveUnreadable:
		return msgUnreadable
	case apperror.CodeArchivePathTraversal:
		return msgPathTraversal
	case apperror.CodeArchiveLinkEntry:
		return msgLinkEntry
	case apperror.CodeArchiveEncrypted:
		return msgEncrypted
	case apperror.CodeArchiveNameInvalid:
		return msgNameInvalid
	case apperror.CodeArchiveLimitExceeded:
		return msgLimitExceeded
	case apperror.CodeArchiveCorrupt:
		return msgCorrupt
	case apperror.CodeInspectionFailed:
		return msgInspectionFailed
	default:
		return string(code)
	}
}

// entryRefusal builds a refusal whose subject is one archive entry. The entry
// is quoted with %q so a hostile name — a NUL byte, a control character, a
// newline — lands in Details escaped rather than as invisible text.
func entryRefusal(code apperror.Code, entry, reason string) *apperror.AppError {
	return apperror.NewRecoverable(code, refusalMessage(code), fmt.Sprintf("entry %q: %s", entry, reason))
}

// archiveRefusal builds a refusal whose subject is the source archive as a
// whole: its size, its readability, or its central directory. There is no
// entry to name, so the path takes that place.
func archiveRefusal(code apperror.Code, srcPath, reason string) *apperror.AppError {
	return apperror.NewRecoverable(code, refusalMessage(code), fmt.Sprintf("archive %q: %s", srcPath, reason))
}

// stageRefusal builds a refusal whose subject is the staging filesystem: a
// stage that cannot be created, proven empty, or written to. The OS error is
// carried verbatim, because "access is denied" and "the disk is full" are the
// two cases the user can act on and they must not be flattened into one
// sentence.
func stageRefusal(subject, reason string) *apperror.AppError {
	return apperror.NewRecoverable(apperror.CodeInspectionFailed, msgInspectionFailed,
		fmt.Sprintf("stage %q: %s", subject, reason))
}
