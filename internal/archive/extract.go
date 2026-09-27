// This file holds the extraction seam itself: Extract, its two phases, and the
// progress reporting that makes a slow run visible. One archive is read twice
// — once sequentially for its hash, once entry by entry out of the central
// directory — and written once, into the stage, through an os.Root handle that
// keeps the stage's boundary the operating system's business rather than the
// policy pass's alone.
//
// The order of operations is the security property, so it is worth stating
// plainly. The stat bounds the archive before archive/zip can allocate for its
// central directory. The policy pass runs before the stage exists, so a refused
// archive is refused with nothing written anywhere. The stage is proven empty
// before the first entry lands in it, so a caller can never lose files to an
// extractor. And the source is re-checked after extraction, so the bytes that
// were hashed are the bytes the caller will still find on disk.
package archive

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Zendevve/astradew/internal/apperror"
)

const (
	// bufSize is the one I/O buffer a run allocates, shared by the hashing
	// pass and the extraction pass. The phases do not overlap, so one buffer
	// is enough, and 64 KiB keeps the syscall count low without making the
	// progress callback wait long between chunks.
	bufSize = 64 << 10
	// progressStep is how much movement buys one progress event: a large
	// enough step that a 20,000-entry archive produces dozens of events
	// rather than one per entry, and small enough that a progress bar moves
	// smoothly on a slow disk.
	progressStep = 1 << 20
)

// Extract inspects srcPath without changing anything outside destRoot: it
// hashes the source over a read-only handle, refuses the whole archive on the
// first policy or limit violation, writes the entries into destRoot through an
// os.Root handle, and reports the digests, counts, and per-file hashes of what
// landed there.
//
// It creates destRoot (0o755) and refuses a destRoot that already holds
// anything: the stage belongs to this call alone. On failure it removes what it
// wrote, so a refused or interrupted run leaves no partial tree behind; on
// success the caller owns the stage and is responsible for removing it once the
// inspection is over (see StageDir and SweepStages). The first thing Extract
// does is check ctx, and a cancelled context is returned as its own error
// rather than as one of this package's refusals; cancellation is observed again
// per hash chunk, per entry, and per copy chunk, so a cancelled run — even one
// stuck on a single multi-gigabyte entry — stops promptly and still cleans up
// after itself.
//
// limits fills any unset field from DefaultLimits; onProgress may be nil. The
// returned Result's StagePath is destRoot exactly as given.
func Extract(ctx context.Context, srcPath, destRoot string, limits Limits, onProgress func(Progress)) (result Result, err error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	limits = limits.withDefaults()

	// The stat bounds everything that follows: it is the only thing standing
	// between a hostile file and the central-directory parse, and it captures
	// the size and mtime the run is defined against.
	before, err := os.Stat(srcPath)
	if err != nil {
		return Result{}, archiveRefusal(apperror.CodeArchiveUnreadable, srcPath, err.Error())
	}
	if !before.Mode().IsRegular() {
		return Result{}, archiveRefusal(apperror.CodeArchiveUnreadable, srcPath, "it is not a regular file")
	}
	if before.Size() > limits.MaxArchiveBytes {
		return Result{}, archiveRefusal(apperror.CodeArchiveLimitExceeded, srcPath,
			fmt.Sprintf("it is %d bytes, over the %d-byte archive limit", before.Size(), limits.MaxArchiveBytes))
	}

	source, err := os.Open(srcPath)
	if err != nil {
		return Result{}, archiveRefusal(apperror.CodeArchiveUnreadable, srcPath, err.Error())
	}
	defer source.Close()

	buf := make([]byte, bufSize)
	archiveSHA, err := hashSource(ctx, source, before.Size(), buf, onProgress)
	if err != nil {
		return Result{}, err
	}

	// The zip reader reads bodies through origin, which counts the bytes the
	// archive actually yields; the ratio budget is charged from that count
	// rather than from any declaration the archive makes about itself. A
	// *os.File is a ReaderAt, so NewReader reads the same file the hash did:
	// hashing advanced the read offset, and ReadAt ignores it.
	origin := &countingReaderAt{source: source}
	reader, err := zip.NewReader(origin, before.Size())
	if err != nil {
		return Result{}, archiveRefusal(apperror.CodeArchiveCorrupt, srcPath, err.Error())
	}
	if refusal := checkArchive(reader, srcPath, limits); refusal != nil {
		return Result{}, refusal
	}

	// Only now, with the archive proven acceptable, does anything get created.
	stageExisted, err := prepareStage(destRoot)
	if err != nil {
		return Result{}, err
	}
	// From here the stage's contents are ours, so a failure anywhere below
	// takes them back out. The handle is closed before this runs: defers are
	// last-in-first-out, and Windows will not remove an open file's directory.
	defer func() {
		if err != nil {
			discardStage(destRoot, stageExisted)
		}
	}()
	root, err := os.OpenRoot(destRoot)
	if err != nil {
		return Result{}, stageRefusal(destRoot, err.Error())
	}
	defer root.Close()

	extraction := &extractor{ctx: ctx, root: root, limits: limits, buf: buf, origin: origin,
		report: &progressReport{on: onProgress, phase: PhaseExtracting, total: declaredBytes(reader.File),
			label: "extracting the archive"},
		dirs: map[string]bool{".": true}}
	if err := extraction.run(reader); err != nil {
		return Result{}, err
	}
	if err := confirmUnchanged(srcPath, before); err != nil {
		return Result{}, err
	}
	sort.Slice(extraction.files, func(i, j int) bool { return extraction.files[i].Path < extraction.files[j].Path })

	return Result{
		StagePath:     destRoot,
		SizeBytes:     before.Size(),
		ArchiveSHA256: archiveSHA,
		PackageSHA256: packageDigest(extraction.files),
		Entries:       len(reader.File),
		ExpandedBytes: extraction.expanded,
		Files:         extraction.files,
	}, nil
}

// hashSource reads the whole file once, hashing it and reporting progress. The
// byte count must come out exactly equal to the size the stats agreed on: a
// file that shrank, grew, or was replaced under us would otherwise be hashed as
// a prefix of itself and reported under a digest the caller cannot reproduce.
func hashSource(ctx context.Context, source *os.File, size int64, buf []byte, onProgress func(Progress)) (string, error) {
	report := &progressReport{on: onProgress, phase: PhaseHashing, total: size, label: "hashing the archive"}
	report.emit(0, "hashing the archive")

	digest := sha256.New()
	var read int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := source.Read(buf)
		if n > 0 {
			digest.Write(buf[:n])
			read += int64(n)
			report.advance(read)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", archiveRefusal(apperror.CodeArchiveUnreadable, source.Name(), err.Error())
		}
	}
	if read != size {
		return "", archiveRefusal(apperror.CodeArchiveUnreadable, source.Name(), reasonChanged)
	}
	report.emit(read, "hashed the archive")
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// prepareStage creates the stage directory and proves it is empty, reporting
// whether it already existed. A directory the caller created (and left empty)
// is kept if the run fails; one this call created is removed.
func prepareStage(destRoot string) (bool, error) {
	existed := false
	if info, err := os.Stat(destRoot); err == nil {
		if !info.IsDir() {
			return false, stageRefusal(destRoot, "it exists and is not a directory")
		}
		existed = true
	}
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return false, stageRefusal(destRoot, err.Error())
	}
	entries, err := os.ReadDir(destRoot)
	if err != nil {
		if !existed {
			// The directory is ours and unreadable, so nothing else can be
			// in it: take it back rather than leave a stage that will never
			// be swept.
			os.RemoveAll(destRoot)
		}
		return false, stageRefusal(destRoot, err.Error())
	}
	if len(entries) > 0 {
		return existed, stageRefusal(destRoot, fmt.Sprintf("it already holds %d entries", len(entries)))
	}
	return existed, nil
}

// discardStage removes what a failed run wrote, best effort: a cleanup that
// itself fails must not replace the refusal the caller needs to see.
func discardStage(destRoot string, existed bool) {
	if !existed {
		os.RemoveAll(destRoot)
		return
	}
	// The caller handed us an empty directory it owns: take back what we
	// wrote into it and leave the directory itself alone.
	entries, err := os.ReadDir(destRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		os.RemoveAll(filepath.Join(destRoot, entry.Name()))
	}
}

// confirmUnchanged re-checks the source after the stage is written. A file that
// changed under us means the archive that was hashed is not the archive the
// caller will find on disk, so the run is refused and the stage discarded
// rather than reported as an inspection of the current file.
func confirmUnchanged(srcPath string, before os.FileInfo) error {
	after, err := os.Stat(srcPath)
	if err != nil {
		return archiveRefusal(apperror.CodeArchiveUnreadable, srcPath, err.Error())
	}
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return archiveRefusal(apperror.CodeArchiveUnreadable, srcPath, reasonChanged)
	}
	return nil
}

// countingReaderAt counts the bytes read out of the source archive through it.
// The zip reader is handed this wrapper, so read counts what the archive
// actually yields — bodies, central directory, local headers and all — and the
// extraction loop can charge the compression-ratio budget against bytes that
// exist rather than against a header's own claim about them.
type countingReaderAt struct {
	source *os.File
	read   int64
}

// ReadAt implements io.ReaderAt, counting the bytes the source hands back. A
// failed read still counts what arrived, which is what makes a short entry
// visible to the ratio budget.
func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.source.ReadAt(p, off)
	c.read += int64(n)
	return n, err
}

// extractor is the state one extraction pass carries. It exists so each step —
// what to do with an entry, how to create its directory, how to copy it — can
// be read on its own without threading six arguments through every call.
type extractor struct {
	ctx    context.Context
	root   *os.Root
	limits Limits
	buf    []byte
	report *progressReport
	// origin counts the bytes the archive has yielded, which the copy loop
	// charges the ratio budget against.
	origin *countingReaderAt
	// dirs records the directories already created in the stage, so a
	// central directory that names 20,000 files in one folder asks the
	// filesystem to make that folder once rather than 20,000 times.
	dirs map[string]bool
	// files and expanded accumulate what the run produced: one digest per
	// file written, and the total bytes written.
	files    []FileDigest
	expanded int64
}

// run writes every entry into the stage in central-directory order, which is
// the order the archive itself declares and therefore the order a user's other
// tools would report.
func (x *extractor) run(reader *zip.Reader) error {
	x.report.emit(0, fmt.Sprintf("extracting %d entries", len(reader.File)))
	for _, entry := range reader.File {
		if err := x.ctx.Err(); err != nil {
			return err
		}
		if err := x.entry(entry); err != nil {
			return err
		}
	}
	x.report.emit(x.expanded, "extracted the archive")
	return nil
}

// entry writes one entry: a directory becomes its tree, a file becomes a file.
// The expanded-budget check runs here, before anything is created, because the
// declared uncompressed size is an upper bound on what reading the entry can
// yield — archive/zip refuses to hand out more than an entry declares — so an
// archive that declares more than the budget allows is refused without writing
// a byte of it. The ratio budget cannot be charged here: it is a fact about
// bytes, not declarations, so the copy loop charges it as the bytes arrive.
func (x *extractor) entry(entry *zip.File) error {
	name := strings.TrimSuffix(entry.Name, "/")
	if strings.HasSuffix(entry.Name, "/") {
		return x.mkdir(name)
	}
	declared, ok := declaredSize(entry)
	if !ok {
		return entryRefusal(apperror.CodeArchiveLimitExceeded, entry.Name,
			fmt.Sprintf("it declares %d bytes, over the %d-byte expanded limit", entry.UncompressedSize64, x.limits.MaxExpandedBytes))
	}
	if declared > x.limits.MaxExpandedBytes-x.expanded {
		return entryRefusal(apperror.CodeArchiveLimitExceeded, entry.Name,
			fmt.Sprintf("it would expand the archive past the %d-byte expanded limit", x.limits.MaxExpandedBytes))
	}
	if parent := path.Dir(name); parent != "." {
		if err := x.mkdir(parent); err != nil {
			return err
		}
	}
	file, err := x.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_EXCL, 0o644)
	if err != nil {
		return stageRefusal(name, err.Error())
	}
	digest, written, err := x.copy(file, entry, x.origin.read)
	if closeErr := file.Close(); err == nil && closeErr != nil {
		err = stageRefusal(name, closeErr.Error())
	}
	if err != nil {
		return err
	}
	x.expanded += written
	x.files = append(x.files, FileDigest{Path: name, Size: written, SHA256: digest})
	x.report.advance(x.expanded)
	return nil
}

// mkdir creates one directory inside the stage, once. os.Root creates no
// parents of its own, so every file's parent is made here first.
func (x *extractor) mkdir(name string) error {
	if x.dirs[name] {
		return nil
	}
	if err := x.root.MkdirAll(name, 0o755); err != nil {
		return stageRefusal(name, err.Error())
	}
	x.dirs[name] = true
	return nil
}

// copy streams one entry into its file, hashing the bytes as they pass. The
// copy is deliberately a loop rather than io.CopyBuffer: a failure while
// reading the entry is corrupt archive data, while a failure while writing the
// stage is a filesystem problem, and the two must not arrive as the same
// refusal. Reading to EOF is what lets archive/zip check the CRC, and the byte
// count is compared against the declared size on top of it, so a truncated copy
// can never pass as a complete one.
//
// The ratio budget is charged here, from the bytes actually read out of the
// archive since start, because a ratio computed from the header's declared
// compressed size is a ratio the archive chooses: overstating that declaration
// would widen the allowance for exactly the archive a ratio limit exists to
// refuse. consumed counts what the decompressor pulled from the file, which
// includes at most one read buffer of look-ahead, so the allowance can only be
// loose by the ratio times that buffer — bounded, and negligible beside the
// grace.
func (x *extractor) copy(file *os.File, entry *zip.File, start int64) (string, int64, error) {
	source, err := entry.Open()
	if err != nil {
		return "", 0, entryRefusal(apperror.CodeArchiveCorrupt, entry.Name, err.Error())
	}
	defer source.Close()

	digest := sha256.New()
	var written int64
	for {
		// A single entry can be gigabytes, so cancellation is observed inside
		// the copy as well as between entries: a cancelled inspection has to
		// stop while it is still copying, not after the entry it is copying.
		if err := x.ctx.Err(); err != nil {
			return "", written, err
		}
		n, readErr := source.Read(x.buf)
		if n > 0 {
			written += int64(n)
			consumed := x.origin.read - start
			if allowance := ratioAllowance(x.limits, consumed); written > allowance {
				return "", written, entryRefusal(apperror.CodeArchiveLimitExceeded, entry.Name,
					fmt.Sprintf("it expanded past the %d:1 ratio limit: %d bytes read out of the archive produced %d bytes, and the limit allows %d",
						x.limits.MaxRatio, consumed, written, allowance))
			}
			digest.Write(x.buf[:n])
			if _, err := file.Write(x.buf[:n]); err != nil {
				return "", written, stageRefusal(entry.Name, err.Error())
			}
			x.report.advance(x.expanded + written)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", written, entryRefusal(apperror.CodeArchiveCorrupt, entry.Name, readErr.Error())
		}
	}
	if written != int64(entry.UncompressedSize64) {
		return "", written, entryRefusal(apperror.CodeArchiveCorrupt, entry.Name,
			fmt.Sprintf("it declares %d bytes but only %d could be read", entry.UncompressedSize64, written))
	}
	return hex.EncodeToString(digest.Sum(nil)), written, nil
}

// ratioAllowance is how many bytes one entry may produce, given how many bytes
// the archive has actually yielded for it: MaxRatio times those bytes, floored
// at the grace so small highly compressible files cannot false-positive. It
// mirrors Apache POI's inflate-ratio guard — the only first-party ratio default
// in the field — which is also where the grace comes from; the ratio limit is
// deliberately a limit on how much the *archive* has to hold, not on how much
// the entry declares, so a header cannot widen its own allowance.
func ratioAllowance(limits Limits, consumed int64) int64 {
	ratio := int64(limits.MaxRatio)
	if ratio < 1 {
		ratio = 1
	}
	if consumed > 0 && ratio > math.MaxInt64/consumed {
		return math.MaxInt64
	}
	if allowance := ratio * consumed; allowance > ratioGrace {
		return allowance
	}
	return ratioGrace
}

// declaredSize returns one entry's declared uncompressed size as an int64. A
// declaration beyond int64 can only come from a zip64 lie — no host could hold
// the entry anyway — and is refused rather than converted, because the
// conversion would go negative and quietly turn every later comparison around.
func declaredSize(entry *zip.File) (int64, bool) {
	if entry.UncompressedSize64 > math.MaxInt64 {
		return 0, false
	}
	return int64(entry.UncompressedSize64), true
}

// declaredBytes is the extracting phase's progress total: the sum of every
// entry's declared uncompressed size, saturating at MaxInt64 so the arithmetic
// cannot wrap. It is knowingly attacker-controlled — a hostile archive can
// overstate it, and the progress bar will then sit short of the end while the
// run finishes — so it is used as a denominator only, never as a budget. The
// budgets charge what was actually written.
func declaredBytes(files []*zip.File) int64 {
	var total int64
	for _, entry := range files {
		if entry.UncompressedSize64 > math.MaxInt64 || total > math.MaxInt64-int64(entry.UncompressedSize64) {
			return math.MaxInt64
		}
		total += int64(entry.UncompressedSize64)
	}
	return total
}

// progressReport throttles one phase's events: one at the start, then at most
// one per progressStep of movement, and always one at the end. A nil sink makes
// every method a no-op, which is the headless case.
type progressReport struct {
	on    func(Progress)
	phase Phase
	total int64
	label string
	// sent is the Current of the last event, so throttling counts movement
	// rather than time and a fast disk cannot flood a slow consumer.
	sent int64
}

// emit reports one event unconditionally.
func (p *progressReport) emit(current int64, message string) {
	if p.on == nil {
		return
	}
	p.sent = current
	p.on(Progress{Phase: p.phase, Current: current, Total: p.total, Message: message})
}

// advance reports an event once progressStep has passed since the last one.
func (p *progressReport) advance(current int64) {
	if p.on == nil || current-p.sent < progressStep {
		return
	}
	p.emit(current, p.label)
}
