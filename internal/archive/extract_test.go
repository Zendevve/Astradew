// This file holds the extraction seam's own tests: the exact tree a run lands
// in the stage, the hashes it reports for that tree, the proof that it wrote
// nowhere else, the progress it reports while it works, and the two ways a run
// that is still in flight can be told the world moved under it.
package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Zendevve/astradew/internal/apperror"
)

// TestDefaultLimits pins the six numbers the settings registry defaults to and
// the field-by-field rule for a Limits a caller filled in only partly: a zero
// or negative field is not a limit, it is "no opinion".
func TestDefaultLimits(t *testing.T) {
	want := Limits{
		MaxArchiveBytes:  2 << 30,
		MaxExpandedBytes: 4 << 30,
		MaxEntries:       20_000,
		MaxRatio:         100,
		MaxPathBytes:     240,
		MaxPathDepth:     32,
	}
	if got := DefaultLimits(); got != want {
		t.Fatalf("DefaultLimits() = %+v, want %+v", got, want)
	}
	if got := (Limits{}).withDefaults(); got != want {
		t.Fatalf("zero Limits = %+v, want the defaults %+v", got, want)
	}
	partial := Limits{MaxEntries: 5, MaxRatio: -1}
	if got := partial.withDefaults(); got.MaxEntries != 5 || got.MaxRatio != want.MaxRatio || got.MaxPathBytes != want.MaxPathBytes {
		t.Fatalf("partial Limits = %+v, want the set field kept and the rest defaulted", got)
	}
}

func TestExtractWritesTheExactTree(t *testing.T) {
	cases := []struct {
		name    string
		fixture *Fixture
		want    map[string]string
		dirs    []string
	}{
		{
			name:    "single mod",
			fixture: SingleMod(),
			want: map[string]string{
				"FishZones/manifest.json":     `{"Name":"Mushymato.FishZones","Author":"Astradew fixture","Version":"1.0.0","UniqueID":"Mushymato.FishZones","EntryDll":"FishZones.dll"}`,
				"FishZones/FishZones.dll":     "MZ",
				"FishZones/config.json":       `{"Enabled":true}`,
				"FishZones/i18n/default.json": `{"fish":"Fish"}`,
			},
			dirs: []string{"FishZones", "FishZones/i18n"},
		},
		{
			name:    "version-suffixed wrapper",
			fixture: Wrapper(),
			want: map[string]string{
				"FishZones-12345-0-3-2/readme.txt":              `unzip me into Mods`,
				"FishZones-12345-0-3-2/FishZones/manifest.json": `{"Name":"Mushymato.FishZones","Author":"Astradew fixture","Version":"1.0.0","UniqueID":"Mushymato.FishZones","EntryDll":"FishZones.dll"}`,
				"FishZones-12345-0-3-2/FishZones/FishZones.dll": "MZ",
			},
			dirs: []string{"FishZones-12345-0-3-2", "FishZones-12345-0-3-2/FishZones"},
		},
		{
			name:    "multi-mod wrapper",
			fixture: MultiMod(),
			want: map[string]string{
				"Stardew Valley Expanded/StardewValleyExpanded/manifest.json":             `{"Name":"FlashShifter.StardewValleyExpanded","Author":"Astradew fixture","Version":"1.0.0","UniqueID":"FlashShifter.StardewValleyExpanded","EntryDll":"StardewValleyExpanded.dll"}`,
				"Stardew Valley Expanded/StardewValleyExpanded/StardewValleyExpanded.dll": "MZ",
				"Stardew Valley Expanded/[CP] Stardew Valley Expanded/manifest.json":      `{"Name":"FlashShifter.SVE.CP","Author":"Astradew fixture","Version":"1.0.0","UniqueID":"FlashShifter.SVE.CP","ContentPackFor":{"UniqueID":"Pathoschild.ContentPatcher"}}`,
				"Stardew Valley Expanded/[CP] Stardew Valley Expanded/content.json":       "{}",
				"Stardew Valley Expanded/[FTM] Stardew Valley Expanded/manifest.json":     `{"Name":"FlashShifter.SVE.FTM","Author":"Astradew fixture","Version":"1.0.0","UniqueID":"FlashShifter.SVE.FTM","ContentPackFor":{"UniqueID":"Esca.FarmTypeManager"}}`,
				"Stardew Valley Expanded/[FTM] Stardew Valley Expanded/content.json":      "{}",
			},
			dirs: []string{
				"Stardew Valley Expanded",
				"Stardew Valley Expanded/StardewValleyExpanded",
				"Stardew Valley Expanded/[CP] Stardew Valley Expanded",
				"Stardew Valley Expanded/[FTM] Stardew Valley Expanded",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srcPath := writeFixture(t, tc.fixture)
			tempRoot := t.TempDir()
			result := extractInto(t, srcPath, tempRoot, DefaultLimits(), nil)

			if got := treeOf(t, result.StagePath); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("tree = %v, want %v", got, tc.want)
			}
			if got := dirsOf(t, result.StagePath); !reflect.DeepEqual(got, tc.dirs) {
				t.Fatalf("directories = %v, want %v", got, tc.dirs)
			}
			if want := len(names(t, tc.fixture.Bytes())); result.Entries != want {
				t.Fatalf("Entries = %d, want the archive's %d entries", result.Entries, want)
			}
		})
	}
}

// TestExtractAllowsACompressibleEntryWithinTheGrace pins the other half of the
// ratio limit: the grace is there so a small, highly compressible file — the
// JSON and XML every mod ships — cannot false-positive. Half a megabyte of one
// repeated character compresses far past 100:1 and must still extract.
func TestExtractAllowsACompressibleEntryWithinTheGrace(t *testing.T) {
	fixture := NewFixture().File("Mod/locale.json", strings.Repeat("a", 512<<10))
	result := extractInto(t, writeFixture(t, fixture), t.TempDir(), DefaultLimits(), nil)

	if result.ExpandedBytes != 512<<10 {
		t.Fatalf("ExpandedBytes = %d, want the entry's %d bytes extracted", result.ExpandedBytes, 512<<10)
	}
	if len(result.Files) != 1 || result.Files[0].Size != 512<<10 {
		t.Fatalf("Files = %+v, want the one file the archive holds", result.Files)
	}
}

// TestExtractReportsIndependentlyComputedHashes pins the archive and per-file
// digests against hashes the test computes for itself, so a change in how the
// package hashes cannot quietly agree with itself.
func TestExtractReportsIndependentlyComputedHashes(t *testing.T) {
	srcPath := writeFixture(t, MultiMod())
	data, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("reading the source archive: %v", err)
	}
	archiveSum := sha256.Sum256(data)

	tempRoot := t.TempDir()
	result := extractInto(t, srcPath, tempRoot, DefaultLimits(), nil)

	if result.StagePath != StageDir(tempRoot, "inspection") {
		t.Fatalf("StagePath = %q, want the stage it was given", result.StagePath)
	}
	if result.SizeBytes != int64(len(data)) {
		t.Fatalf("SizeBytes = %d, want %d", result.SizeBytes, len(data))
	}
	if want := hex.EncodeToString(archiveSum[:]); result.ArchiveSHA256 != want {
		t.Fatalf("ArchiveSHA256 = %q, want %q", result.ArchiveSHA256, want)
	}
	if want := len(names(t, data)); result.Entries != want {
		t.Fatalf("Entries = %d, want %d", result.Entries, want)
	}

	want := map[string]FileDigest{}
	var expanded int64
	for path, content := range treeOf(t, result.StagePath) {
		sum := sha256.Sum256([]byte(content))
		want[path] = FileDigest{Path: path, Size: int64(len(content)), SHA256: hex.EncodeToString(sum[:])}
		expanded += int64(len(content))
	}
	if len(result.Files) != len(want) {
		t.Fatalf("Files = %d entries, want one per extracted file (%d)", len(result.Files), len(want))
	}
	if !sort.SliceIsSorted(result.Files, func(i, j int) bool { return result.Files[i].Path < result.Files[j].Path }) {
		t.Fatalf("Files = %+v, want them sorted ascending by Path", result.Files)
	}
	for _, file := range result.Files {
		if want[file.Path] != file {
			t.Fatalf("Files[%q] = %+v, want %+v", file.Path, file, want[file.Path])
		}
	}
	if result.ExpandedBytes != expanded {
		t.Fatalf("ExpandedBytes = %d, want %d", result.ExpandedBytes, expanded)
	}
}

// TestExtractWritesOnlyInsideTheStage is the isolation proof: the directories
// around the stage gain nothing but the stage, and the source archive comes out
// with the same size, mtime, and bytes it went in with.
func TestExtractWritesOnlyInsideTheStage(t *testing.T) {
	srcPath := writeFixture(t, MultiMod())
	before, err := os.Stat(srcPath)
	if err != nil {
		t.Fatalf("statting the source archive: %v", err)
	}
	beforeHash := fileSHA256(t, srcPath)

	tempRoot := t.TempDir()
	result := extractInto(t, srcPath, tempRoot, DefaultLimits(), nil)

	assertOnlyNew(t, tempRoot, nil, "inspect")
	assertOnlyNew(t, filepath.Dir(result.StagePath), nil, filepath.Base(result.StagePath))
	if got := len(listing(t, filepath.Dir(result.StagePath))); got != 1 {
		t.Fatalf("the inspection area holds %d entries, want only this run's stage", got)
	}

	after, err := os.Stat(srcPath)
	if err != nil {
		t.Fatalf("statting the source archive again: %v", err)
	}
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("source stat = %d bytes at %v, want the %d bytes at %v it started with",
			after.Size(), after.ModTime(), before.Size(), before.ModTime())
	}
	if got := fileSHA256(t, srcPath); got != beforeHash {
		t.Fatalf("source SHA-256 = %q, want the %q it started with", got, beforeHash)
	}
}

// TestExtractReportsProgress covers both phases: the shape of the events, the
// throttle that keeps a fast disk from flooding the caller, and the final event
// of each phase landing exactly on the count the result reports.
func TestExtractReportsProgress(t *testing.T) {
	// A body larger than one throttle step, and incompressible enough that the
	// archive itself is too: the hashing phase only produces a mid-run event
	// when there is more than a step of archive to read.
	body := make([]byte, 3<<20/2)
	random := rand.New(rand.NewPCG(1, 2))
	for i := range len(body) {
		body[i] = byte(random.Uint64() >> 56)
	}
	srcPath := writeFixture(t, NewFixture().FileBytes("Mod/payload.bin", body))

	tempRoot := t.TempDir()
	log := &progressLog{}
	result := extractInto(t, srcPath, tempRoot, DefaultLimits(), log.record)

	hashing := log.phase(PhaseHashing)
	if len(hashing) < 3 {
		t.Fatalf("hashing events = %+v, want a start, a mid-run event, and a final event", hashing)
	}
	if hashing[0].Current != 0 || hashing[0].Total != result.SizeBytes {
		t.Fatalf("first hashing event = %+v, want 0 of %d bytes", hashing[0], result.SizeBytes)
	}
	for i, event := range hashing {
		if event.Total != result.SizeBytes {
			t.Fatalf("hashing event %+v has Total = %d, want the archive's %d bytes", event, event.Total, result.SizeBytes)
		}
		if i > 0 && event.Current < hashing[i-1].Current {
			t.Fatalf("hashing progress went backwards: %+v", hashing)
		}
		if i > 0 && i < len(hashing)-1 && event.Current-hashing[i-1].Current < progressStep {
			t.Fatalf("hashing events %+v are closer together than the %d-byte throttle", hashing[i-1:i+1], progressStep)
		}
	}
	if last := hashing[len(hashing)-1]; last.Current != result.SizeBytes {
		t.Fatalf("last hashing event = %+v, want Current = %d", last, result.SizeBytes)
	}

	extracting := log.phase(PhaseExtracting)
	if len(extracting) < 2 {
		t.Fatalf("extracting events = %+v, want a start and a final event", extracting)
	}
	if extracting[0].Current != 0 || extracting[0].Total != result.ExpandedBytes {
		t.Fatalf("first extracting event = %+v, want 0 of %d bytes", extracting[0], result.ExpandedBytes)
	}
	if last := extracting[len(extracting)-1]; last.Current != result.ExpandedBytes {
		t.Fatalf("last extracting event = %+v, want Current = %d", last, result.ExpandedBytes)
	}
	for _, event := range log.events {
		if event.Message == "" {
			t.Fatalf("progress event %+v has no message", event)
		}
	}
}

// TestExtractStopsOnACancelledContext pins the contract that cancellation is
// the context's error and not one of this package's refusals: a cancelled
// inspection is not a broken archive.
func TestExtractStopsOnACancelledContext(t *testing.T) {
	srcPath := writeFixture(t, SingleMod())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Extract(ctx, srcPath, StageDir(t.TempDir(), "inspection"), DefaultLimits(), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	var refusal *apperror.AppError
	if errors.As(err, &refusal) {
		t.Fatalf("err = %v, want the context error itself, not a refusal", err)
	}
}

// TestExtractCancelledMidRun cancels a run that is already extracting, which is
// the case a user's Cancel button produces: the run must stop, report the
// context's error, and still take its stage back out.
func TestExtractCancelledMidRun(t *testing.T) {
	srcPath := writeFixture(t, manyEntries(3000))
	tempRoot := t.TempDir()
	stage := StageDir(tempRoot, "inspection")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	acted := afterStageAppears(t, stage, cancel)

	_, err := Extract(ctx, srcPath, stage, DefaultLimits(), nil)
	if !<-acted {
		t.Fatalf("the run finished before its stage ever appeared; the test could not interleave")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if _, statErr := os.Stat(stage); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the cancelled run's stage still exists (stat err = %v), want it removed", statErr)
	}
}

// TestExtractCancelledMidCopy cancels while a single entry is being copied,
// the case a run made of one large file produces. The between-entry check
// cannot fire here — there is only one entry, and its copy is still running —
// so the cancellation has to be observed inside the copy loop, from the
// progress the copy itself reports. The body is incompressible on purpose: a
// body of zeros this size would be refused by the ratio limit before the copy
// ever started.
func TestExtractCancelledMidCopy(t *testing.T) {
	body := make([]byte, 4<<20)
	random := rand.New(rand.NewPCG(3, 4))
	for i := range len(body) {
		body[i] = byte(random.Uint64() >> 56)
	}
	srcPath := writeFixture(t, NewFixture().FileBytes("Mod/big.bin", body))
	tempRoot := t.TempDir()
	stage := StageDir(tempRoot, "inspection")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	onProgress := func(event Progress) {
		// The first extracting event with movement comes from inside the copy
		// loop, one chunk past a megabyte of the entry already written.
		if event.Phase == PhaseExtracting && event.Current > 0 {
			cancel()
		}
	}

	_, err := Extract(ctx, srcPath, stage, DefaultLimits(), onProgress)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if _, statErr := os.Stat(stage); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the cancelled run's stage still exists (stat err = %v), want it removed", statErr)
	}
}

// TestExtractRefusesASourceThatChangedMidRun changes the source while the run
// is extracting it, which is what a re-download or an antivirus rewrite looks
// like: the run must refuse rather than report an inspection of a file that is
// no longer the file it hashed.
func TestExtractRefusesASourceThatChangedMidRun(t *testing.T) {
	srcPath := writeFixture(t, manyEntries(3000))
	tempRoot := t.TempDir()
	stage := StageDir(tempRoot, "inspection")

	acted := afterStageAppears(t, stage, func() {
		file, err := os.OpenFile(srcPath, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			return
		}
		defer file.Close()
		file.WriteString("x")
	})

	_, err := Extract(t.Context(), srcPath, stage, DefaultLimits(), nil)
	if !<-acted {
		t.Fatalf("the run finished before its stage ever appeared; the test could not interleave")
	}
	refusalOf(t, err, apperror.CodeArchiveUnreadable, srcPath)
	if _, statErr := os.Stat(stage); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the refused run's stage still exists (stat err = %v), want it removed", statErr)
	}
}

// TestExtractRefusesANonEmptyStage pins the one refusal that protects the
// caller's own files: a stage that already holds something is left exactly as
// it was found.
func TestExtractRefusesANonEmptyStage(t *testing.T) {
	srcPath := writeFixture(t, SingleMod())
	tempRoot := t.TempDir()
	stage := StageDir(tempRoot, "inspection")
	if err := os.MkdirAll(stage, 0o755); err != nil {
		t.Fatalf("preparing the stage: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stage, "keep.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatalf("writing into the stage: %v", err)
	}

	_, err := Extract(t.Context(), srcPath, stage, DefaultLimits(), nil)
	refusalOf(t, err, apperror.CodeInspectionFailed, stage)
	if got := treeOf(t, stage); !reflect.DeepEqual(got, map[string]string{"keep.txt": "mine"}) {
		t.Fatalf("stage = %v, want the caller's file untouched", got)
	}
}

// manyEntries builds an archive with enough entries that a run takes long
// enough for another goroutine to act while it is still in flight. 3,000 file
// creations is a second or more of work even on a fast disk, while the actor
// only has to notice the stage directory, which exists for the whole of that
// time.
func manyEntries(count int) *Fixture {
	fixture := NewFixture()
	for i := range count {
		fixture.File(fmt.Sprintf("Mod/file%05d.txt", i), "x")
	}
	return fixture
}

// afterStageAppears runs act in the background as soon as stage exists, and
// reports whether it got to run. The stage only exists after the source was
// statted and hashed, so anything act does to the source or the context is
// guaranteed to land between that stat and the post-extraction re-check: the
// interleaving these tests need is deterministic rather than lucky.
func afterStageAppears(t *testing.T, stage string, act func()) <-chan bool {
	t.Helper()
	acted := make(chan bool, 1)
	go func() {
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(stage); err == nil {
				act()
				acted <- true
				return
			}
			time.Sleep(100 * time.Microsecond)
		}
		acted <- false
	}()
	return acted
}
