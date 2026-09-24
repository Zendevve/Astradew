package store

import (
	"runtime"
	"strings"
	"testing"

	"github.com/Zendevve/astradew/internal/approot"
)

// Fresh migration: a new database carries the game_installs table with the
// exact 0001 timestamp defaults and the UNIQUE(path) backstop.
func TestGameInstallsMigrationCreatesTable(t *testing.T) {
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	s, err := Open(paths)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = s.Close() }()
	if !tableNames(t, s.DB())["game_installs"] {
		t.Fatal("game_installs table missing after fresh open")
	}
	var sql string
	if err := s.DB().QueryRow(`SELECT sql FROM sqlite_master WHERE name = 'game_installs'`).Scan(&sql); err != nil {
		t.Fatalf("reading game_installs DDL: %v", err)
	}
	for _, want := range []string{
		"path TEXT NOT NULL",
		"source TEXT NOT NULL",
		"smapi_exe_path TEXT",
		"game_version TEXT",
		"smapi_version TEXT",
		"UNIQUE (path)",
		"strftime('%Y-%m-%dT%H:%M:%fZ', 'now')",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("game_installs DDL missing %q, got:\n%s", want, sql)
		}
	}
	if strings.Contains(sql, "CHECK") {
		t.Fatalf("game_installs DDL carries a CHECK constraint, want Go-side allow-list only:\n%s", sql)
	}
}

// CanonicalGamePath is pure lexical: Clean + Abs, no symlink resolution, no
// case folding, no trailing separator, no I/O.
func TestCanonicalGamePathIsLexical(t *testing.T) {
	got, err := CanonicalGamePath("some/../game")
	if err != nil {
		t.Fatalf("CanonicalGamePath() error = %v", err)
	}
	if strings.HasSuffix(got, "/") && len(got) > 1 {
		t.Fatalf("CanonicalGamePath() = %q, want no trailing separator", got)
	}
	if strings.Contains(got, "..") {
		t.Fatalf("CanonicalGamePath() = %q, want Clean applied", got)
	}
	upper, err := CanonicalGamePath("GAME")
	if err != nil {
		t.Fatalf("CanonicalGamePath() error = %v", err)
	}
	if !strings.HasSuffix(upper, "GAME") {
		t.Fatalf("CanonicalGamePath() = %q, want case preserved (no folding)", upper)
	}
}

// Insert-compare: re-picking a known folder refreshes the row instead of
// duplicating it; per-OS case behaviour follows the EqualFold rule.
func TestUpsertGameInstallUpdatesNotDuplicates(t *testing.T) {
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	s, err := Open(paths)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	first, err := UpsertGameInstall(s.DB(), "game", "manual", nil, nil, nil)
	if err != nil {
		t.Fatalf("UpsertGameInstall() error = %v", err)
	}
	second, err := UpsertGameInstall(s.DB(), "game/../game", "steam", nil, nil, nil)
	if err != nil {
		t.Fatalf("second UpsertGameInstall() error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("re-pick id = %d, want existing %d: a known folder must refresh, never duplicate", second.ID, first.ID)
	}
	if second.Source != "steam" {
		t.Fatalf("re-pick source = %q, want refreshed %q", second.Source, "steam")
	}
	wantPath, err := CanonicalGamePath("game/../game")
	if err != nil {
		t.Fatalf("CanonicalGamePath() error = %v", err)
	}
	if second.Path != wantPath {
		t.Fatalf("re-pick path = %q, want refreshed canonical %q", second.Path, wantPath)
	}
	rows, err := ListGameInstalls(s.DB())
	if err != nil {
		t.Fatalf("ListGameInstalls() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 after re-pick", len(rows))
	}
	if rows[0].Path != wantPath {
		t.Fatalf("stored path = %q, want refreshed canonical %q", rows[0].Path, wantPath)
	}
}

// Case-variant compare: EqualFold on windows/darwin merges, strict ==
// elsewhere (Linux stays exact, always).
func TestUpsertGameInstallCaseVariantPerOS(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		t.Skip("case-folding compare applies on windows/darwin only; Linux stays exact")
	}
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	s, err := Open(paths)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	first, err := UpsertGameInstall(s.DB(), "GameDir", "manual", nil, nil, nil)
	if err != nil {
		t.Fatalf("UpsertGameInstall() error = %v", err)
	}
	second, err := UpsertGameInstall(s.DB(), "gamedir", "manual", nil, nil, nil)
	if err != nil {
		t.Fatalf("second UpsertGameInstall() error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("case-variant id = %d, want existing %d on %s", second.ID, first.ID, runtime.GOOS)
	}
}

// Unknown sources pass through unread-but-preserved: no CHECK to trip on.
func TestUpsertGameInstallPreservesUnknownSource(t *testing.T) {
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	s, err := Open(paths)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	inserted, err := UpsertGameInstall(s.DB(), "game", "future-source", nil, nil, nil)
	if err != nil {
		t.Fatalf("UpsertGameInstall() error = %v", err)
	}
	if inserted.Source != "future-source" {
		t.Fatalf("source = %q, want preserved %q", inserted.Source, "future-source")
	}
	got, err := GetGameInstall(s.DB(), inserted.ID)
	if err != nil {
		t.Fatalf("GetGameInstall() error = %v", err)
	}
	if got.Source != "future-source" {
		t.Fatalf("re-read source = %q, want preserved %q", got.Source, "future-source")
	}
}

// Reopen persistence: rows survive a full close/reopen round trip.
func TestGameInstallsSurviveReopen(t *testing.T) {
	base := t.TempDir()
	paths, err := approot.ResolveWithBase(base)
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	s, err := Open(paths)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	inserted, err := UpsertGameInstall(s.DB(), "game", "manual", nil, nil, nil)
	if err != nil {
		t.Fatalf("UpsertGameInstall() error = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	reopened, err := Open(paths)
	if err != nil {
		t.Fatalf("reopen Open() error = %v", err)
	}
	defer func() { _ = reopened.Close() }()
	got, err := GetGameInstall(reopened.DB(), inserted.ID)
	if err != nil {
		t.Fatalf("GetGameInstall() after reopen error = %v", err)
	}
	if got.ID != inserted.ID || got.Path != inserted.Path || got.Source != inserted.Source {
		t.Fatalf("reopened row = %+v, want %+v", got, inserted)
	}
}
