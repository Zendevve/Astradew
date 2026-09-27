// This file owns the temporary stage layout: where one inspection's extracted
// tree lives, and how leftovers from a crash are removed. The layout has
// exactly one owner because three callers must agree on it — the code that
// chooses the stage and deletes it when the inspection ends, the startup sweep
// that removes what a crash left behind, and the tests that prove neither
// leaves anything else on disk.
package archive

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// inspectDir is the one level between the application's temp root and any
// inspection stage. It is deliberately unexported: callers compose stage paths
// through StageDir, so the level's name cannot drift between the code that
// creates a stage and the code that sweeps stages.
const inspectDir = "inspect"

// StageDir returns the directory one inspection extracts into:
// <tempRoot>/inspect/<inspectionID>. The extra level keeps inspection stages
// apart from every other tenant of the application's temp root, which is what
// lets SweepStages remove everything under it without a judgement call about
// what is a stage and what is not.
func StageDir(tempRoot, inspectionID string) string {
	return filepath.Join(tempRoot, inspectDir, inspectionID)
}

// SweepStages removes every entry directly under <tempRoot>/inspect and reports
// how many it removed. A missing inspection area is not an error — it is the
// normal state of a fresh install — so it reports zero and no error.
//
// A failure does not stop the sweep: one directory that cannot be removed (a
// file still open, a permission only the user can fix) must not strand every
// other leftover, and a startup that refuses to run because yesterday's stage
// is stuck would be worse than the space it costs. The first error is returned
// alongside the count of what did go, so the caller can log one honest line.
func SweepStages(tempRoot string) (int, error) {
	dir := filepath.Join(tempRoot, inspectDir)
	// The area's absence and the area being something other than a directory
	// are different situations, and only the first is normal. On Windows a
	// stat through a non-directory reports "path not found", so the two are
	// told apart by the stat rather than by the read's error.
	info, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("reading the inspection area %q: %w", dir, err)
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("the inspection area %q is not a directory", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("reading the inspection area %q: %w", dir, err)
	}
	removed := 0
	var firstErr error
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("removing the inspection stage %q: %w", entry.Name(), err)
			}
			continue
		}
		removed++
	}
	return removed, firstErr
}
