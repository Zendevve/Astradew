// This file holds the extraction seam's performance guard: a package with
// thousands of small entries must extract in linear-ish time and still report
// progress. The budget is deliberately loose — it exists to catch an accidental
// quadratic, not to police a slow disk — and the entry count is high enough
// that a per-entry scan of everything already extracted would blow it.
package archive

import (
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestExtractManyEntriesStaysWithinBudget(t *testing.T) {
	const entries = 4000
	const budget = 30 * time.Second

	fixture := NewFixture()
	for i := range entries {
		fixture.File(fmt.Sprintf("Mod/pack/file%05d.json", i), `{"index":`+strconv.Itoa(i)+`}`)
	}
	srcPath := writeFixture(t, fixture)

	log := &progressLog{}
	started := time.Now()
	result := extractInto(t, srcPath, t.TempDir(), DefaultLimits(), log.record)
	elapsed := time.Since(started)

	if result.Entries != entries {
		t.Fatalf("Entries = %d, want %d", result.Entries, entries)
	}
	// Counted with one directory listing rather than a walk that reads every
	// file: the budget below is about the extractor's work, not the test's.
	if got := len(listing(t, filepath.Join(result.StagePath, "Mod", "pack"))); got != entries {
		t.Fatalf("stage holds %d files, want %d", got, entries)
	}
	if len(log.phase(PhaseHashing)) == 0 || len(log.phase(PhaseExtracting)) == 0 {
		t.Fatalf("progress = %+v, want both phases reported", log.events)
	}
	if elapsed > budget {
		t.Fatalf("extracting %d entries took %v, over the %v budget: the extractor is doing more work than its size", entries, elapsed, budget)
	}
	t.Logf("extracted %d entries in %v", entries, elapsed)
}
