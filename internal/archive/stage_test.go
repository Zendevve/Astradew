// This file holds the stage layout's tests: that StageDir composes the layout
// the sweep assumes, and that the sweep removes every leftover under it —
// including a run of them — without touching anything else under the
// application's temp root.
package archive

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStageDirKeepsStagesInTheirOwnArea(t *testing.T) {
	got := StageDir(filepath.Join("temp", "root"), "inspection-42")
	want := filepath.Join("temp", "root", "inspect", "inspection-42")
	if got != want {
		t.Fatalf("StageDir = %q, want %q", got, want)
	}
}

func TestSweepStagesRemovesEveryLeftover(t *testing.T) {
	tempRoot := t.TempDir()
	// A tenant that is not an inspection stage: the sweep must not touch it.
	other := filepath.Join(tempRoot, "tasks")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatalf("preparing the other tenant: %v", err)
	}
	writeInto(t, filepath.Join(other, "keep"), "keep.txt", "mine")

	writeInto(t, StageDir(tempRoot, "crashed-one"), "Mod/manifest.json", "{}")
	writeInto(t, StageDir(tempRoot, "crashed-two"), "Mod/Mod.dll", "MZ")
	if err := os.MkdirAll(StageDir(tempRoot, "crashed-three"), 0o755); err != nil {
		t.Fatalf("preparing an empty stage: %v", err)
	}

	removed, err := SweepStages(tempRoot)
	if err != nil {
		t.Fatalf("SweepStages = %v, want no error", err)
	}
	if removed != 3 {
		t.Fatalf("removed = %d, want the three leftover stages", removed)
	}
	if got := listing(t, filepath.Join(tempRoot, "inspect")); len(got) != 0 {
		t.Fatalf("the inspection area holds %v, want every stage gone", got)
	}
	if _, err := os.Stat(filepath.Join(other, "keep", "keep.txt")); err != nil {
		t.Fatalf("the other tenant's file is gone (%v), want it untouched", err)
	}
}

func TestSweepStagesOnAnEmptyArea(t *testing.T) {
	tempRoot := t.TempDir()
	if removed, err := SweepStages(tempRoot); removed != 0 || err != nil {
		t.Fatalf("SweepStages on a fresh temp root = %d, %v, want 0, nil", removed, err)
	}
	if err := os.MkdirAll(filepath.Join(tempRoot, "inspect"), 0o755); err != nil {
		t.Fatalf("preparing an empty inspection area: %v", err)
	}
	if removed, err := SweepStages(tempRoot); removed != 0 || err != nil {
		t.Fatalf("SweepStages on an empty inspection area = %d, %v, want 0, nil", removed, err)
	}
	if removed, err := SweepStages(filepath.Join(tempRoot, "does-not-exist")); removed != 0 || err != nil {
		t.Fatalf("SweepStages on a missing temp root = %d, %v, want 0, nil", removed, err)
	}
}

func TestSweepStagesReportsAnUnreadableArea(t *testing.T) {
	tempRoot := t.TempDir()
	// A file where the inspection area belongs: not a missing area, and not
	// one the sweep can read.
	if err := os.WriteFile(filepath.Join(tempRoot, "inspect"), []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("preparing the blocked area: %v", err)
	}
	removed, err := SweepStages(tempRoot)
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}
	if err == nil {
		t.Fatalf("SweepStages = nil error, want the unreadable area reported")
	}
}

// TestSweepStagesContinuesPastAFailedRemoval pins the sweep's promise that one
// stranded leftover does not strand the rest: a stage this host refuses to
// delete is reported as the error, the count covers only what actually went,
// and every other leftover is still removed. The host has to be able to refuse
// a removal at all — Windows refuses one while a handle is open, a unix-like
// host refuses one inside a directory it may not write — so a host that cannot
// (root, or a filesystem that permits anything) skips rather than asserts.
func TestSweepStagesContinuesPastAFailedRemoval(t *testing.T) {
	tempRoot := t.TempDir()
	stuck := StageDir(tempRoot, "stuck")
	writeInto(t, stuck, "held.txt", "held")
	later := StageDir(tempRoot, "later")
	writeInto(t, later, "keep.txt", "x")

	release, refused := blockRemoval(t, stuck, filepath.Join(stuck, "held.txt"))
	if !refused {
		t.Skip("this host cannot be made to refuse a removal")
	}
	defer release()

	removed, err := SweepStages(tempRoot)
	if err == nil {
		t.Fatalf("SweepStages = nil error, want the stage that could not be removed reported")
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want only the stage that went (%q still exists, %q must not)", removed, stuck, later)
	}
	if _, statErr := os.Stat(later); !os.IsNotExist(statErr) {
		t.Fatalf("the later stage still exists (stat err = %v), want it removed after the failed one", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(stuck, "held.txt")); statErr != nil {
		t.Fatalf("the stuck stage's file is gone (%v), want it left where it was", statErr)
	}
}

// blockRemoval makes one stage impossible to delete on this host and returns a
// release function that undoes the block. refused is false when the host cannot
// be made to refuse (running as root, whose permissions are not checked).
func blockRemoval(t *testing.T, stage, held string) (release func(), refused bool) {
	t.Helper()
	if runtime.GOOS == "windows" {
		// Windows refuses to delete a file another handle holds open, because
		// os.Open does not ask for FILE_SHARE_DELETE. This is the same rule
		// that makes Extract close its os.Root before removing a failed
		// stage.
		handle, err := os.OpenFile(held, os.O_RDONLY, 0)
		if err != nil {
			t.Fatalf("opening %q: %v", held, err)
		}
		return func() { handle.Close() }, true
	}
	// On a unix-like host a file cannot be unlinked from a directory the
	// caller may not write, and RemoveAll gives up on the directory it cannot
	// empty.
	if err := os.Chmod(stage, 0o555); err != nil {
		t.Fatalf("making %q undeletable: %v", stage, err)
	}
	if os.Geteuid() == 0 {
		os.Chmod(stage, 0o755)
		return nil, false
	}
	return func() { os.Chmod(stage, 0o755) }, true
}
