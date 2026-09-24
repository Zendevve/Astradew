package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Zendevve/astradew/internal/apperror"
	"github.com/Zendevve/astradew/internal/approot"
)

// testPaths resolves the product layout under a temp base and returns it.
func testPaths(t *testing.T) approot.Paths {
	t.Helper()
	base := t.TempDir()
	paths, err := approot.ResolveWithBase(base)
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	return paths
}

// requireAppError asserts err is a recoverable *apperror.AppError with the
// given code.
func requireAppError(t *testing.T, err error, code apperror.Code) *apperror.AppError {
	t.Helper()
	if err == nil {
		t.Fatalf("err = nil, want recoverable %s", code)
	}
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("err type = %T, want *apperror.AppError", err)
	}
	if appErr.Code != code {
		t.Fatalf("code = %q, want %q", appErr.Code, code)
	}
	if !appErr.Recoverable {
		t.Fatal("recoverable = false, want true")
	}
	return appErr
}

// queryPragma reads a single PRAGMA value back from the database.
func queryPragma(t *testing.T, db *sql.DB, pragma string) string {
	t.Helper()
	var got string
	if err := db.QueryRow(`PRAGMA ` + pragma).Scan(&got); err != nil {
		t.Fatalf("PRAGMA %s: %v", pragma, err)
	}
	return got
}

// tableNames returns the user tables present in db (no sqlite_ internals).
func tableNames(t *testing.T, db *sql.DB) map[string]bool {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatalf("listing tables: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scanning table name: %v", err)
		}
		out[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating tables: %v", err)
	}
	return out
}

func TestFreshOpenCreatesVersionedDatabase(t *testing.T) {
	paths := testPaths(t)

	s, err := Open(paths)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	if s.Version() != 2 {
		t.Fatalf("Version() = %d, want 2", s.Version())
	}
	if want := filepath.Join(paths.Database, DBFileName); s.Path() != want {
		t.Fatalf("Path() = %q, want %q", s.Path(), want)
	}
	if got := s.Status(); got.Path != s.Path() || got.Version != 2 {
		t.Fatalf("Status() = %+v, want path and version 2", got)
	}
	tables := tableNames(t, s.DB())
	for _, want := range []string{"schema_migrations", "settings", "tasks", "game_installs"} {
		if !tables[want] {
			t.Fatalf("table %q missing, have %v", want, tables)
		}
	}

	if got := queryPragma(t, s.DB(), "foreign_keys"); got != "1" {
		t.Fatalf("PRAGMA foreign_keys = %q, want 1", got)
	}
	if got := queryPragma(t, s.DB(), "journal_mode"); !strings.EqualFold(got, "wal") {
		t.Fatalf("PRAGMA journal_mode = %q, want wal", got)
	}
	var timeout int
	if err := s.DB().QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		t.Fatalf("PRAGMA busy_timeout: %v", err)
	}
	if timeout != 5000 {
		t.Fatalf("PRAGMA busy_timeout = %d, want 5000", timeout)
	}
}

func TestFreshOpenTakesNoBackup(t *testing.T) {
	paths := testPaths(t)

	s, err := Open(paths)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	_ = s.Close()

	entries, err := os.ReadDir(paths.Backups)
	if err != nil {
		t.Fatalf("reading backups: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("backups = %v, want none: a fresh database has nothing to lose", entries)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	paths := testPaths(t)

	first, err := Open(paths)
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	_ = first.Close()

	second, err := Open(paths)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	defer func() { _ = second.Close() }()
	if second.Version() != 2 {
		t.Fatalf("Version() = %d, want 2: reaching the same version twice must be a no-op", second.Version())
	}
}

func TestUpgradeFromVersionZeroFixture(t *testing.T) {
	paths := testPaths(t)

	// Honest previous-schema fixture: a real database file whose version
	// history says 0 and which holds none of the app tables.
	dbPath := filepath.Join(paths.Database, DBFileName)
	seed, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening seed db: %v", err)
	}
	if _, err := seed.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		_ = seed.Close()
		t.Fatalf("seeding schema_migrations: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("closing seed db: %v", err)
	}

	s, err := Open(paths)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = s.Close() }()
	if s.Version() != 2 {
		t.Fatalf("Version() = %d, want 2", s.Version())
	}
	tables := tableNames(t, s.DB())
	for _, want := range []string{"schema_migrations", "settings", "tasks", "game_installs"} {
		if !tables[want] {
			t.Fatalf("table %q missing after upgrade, have %v", want, tables)
		}
	}
}

func TestExistingDatabaseIsBackedUpBeforeMigration(t *testing.T) {
	paths := testPaths(t)

	dbPath := filepath.Join(paths.Database, DBFileName)
	seed, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening seed db: %v", err)
	}
	if _, err := seed.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		_ = seed.Close()
		t.Fatalf("seeding schema_migrations: %v", err)
	}
	if _, err := seed.Exec(`CREATE TABLE legacy_keep (id INTEGER PRIMARY KEY)`); err != nil {
		_ = seed.Close()
		t.Fatalf("seeding legacy table: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("closing seed db: %v", err)
	}

	s, err := Open(paths)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = s.Close() }()

	newest, err := NewestBackup(paths.Backups)
	if err != nil {
		t.Fatalf("NewestBackup() error = %v", err)
	}
	backup, err := sql.Open("sqlite", newest)
	if err != nil {
		t.Fatalf("opening backup: %v", err)
	}
	defer func() { _ = backup.Close() }()
	if !tableNames(t, backup)["legacy_keep"] {
		t.Fatal("backup lost the pre-migration legacy_keep table")
	}
	if tableNames(t, backup)["settings"] {
		t.Fatal("backup gained the post-migration settings table: it must be the pre-migration file")
	}
}

func TestBadMigrationRollsBackFully(t *testing.T) {
	paths := testPaths(t)

	dbPath := filepath.Join(paths.Database, DBFileName)
	seed, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening seed db: %v", err)
	}
	if _, err := seed.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		_ = seed.Close()
		t.Fatalf("seeding schema_migrations: %v", err)
	}
	if _, err := seed.Exec(`CREATE TABLE pre_existing (id INTEGER PRIMARY KEY)`); err != nil {
		_ = seed.Close()
		t.Fatalf("seeding pre-existing table: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("closing seed db: %v", err)
	}

	bad := fstest.MapFS{
		"migrations/0001_partial_then_boom.sql": {Data: []byte("CREATE TABLE half_applied (id INTEGER PRIMARY KEY);\nTHIS IS NOT SQL;\n")},
	}
	_, err = OpenWithMigrations(paths, bad)
	appErr := requireAppError(t, err, apperror.CodeStoreMigrationFailed)
	if !strings.Contains(appErr.Details, "0001") || !strings.Contains(appErr.Details, dbPath) {
		t.Fatalf("details = %q, want failing version 0001 and db path %q", appErr.Details, dbPath)
	}

	probe, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopening db: %v", err)
	}
	defer func() { _ = probe.Close() }()
	var count int
	if err := probe.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 1`).Scan(&count); err != nil {
		t.Fatalf("reading versions: %v", err)
	}
	if count != 0 {
		t.Fatal("version 1 recorded despite the failed migration: rollback did not happen")
	}
	tables := tableNames(t, probe)
	if tables["half_applied"] {
		t.Fatal("half_applied table survived: the failed migration was not rolled back")
	}
	if !tables["pre_existing"] {
		t.Fatal("pre-existing table lost: the failed migration damaged prior schema")
	}
}

// A non-SQLite file where the database belongs must refuse with a typed
// STORE_OPEN_FAILED error, not a half-opened store.
func TestCorruptFileRefusesWithTypedError(t *testing.T) {
	paths := testPaths(t)

	dbPath := filepath.Join(paths.Database, DBFileName)
	if err := os.WriteFile(dbPath, []byte("this is not a sqlite database"), 0o600); err != nil {
		t.Fatalf("seeding corrupt file: %v", err)
	}

	_, err := Open(paths)
	appErr := requireAppError(t, err, apperror.CodeStoreOpenFailed)
	if !strings.Contains(appErr.Details, dbPath) {
		t.Fatalf("details = %q, want db path %q", appErr.Details, dbPath)
	}
	if !strings.Contains(appErr.Details, "restore the newest backup") ||
		!strings.Contains(appErr.Details, "move") {
		t.Fatalf("details = %q, want both repair actions", appErr.Details)
	}
}

// A database whose recorded version is newer than any known migration must
// refuse: something (a newer binary, a hand edit) owns schema this code does
// not describe, and applying older migrations on top would corrupt it.
func TestFutureVersionRefuses(t *testing.T) {
	paths := testPaths(t)

	dbPath := filepath.Join(paths.Database, DBFileName)
	seed, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("opening seed db: %v", err)
	}
	if _, err := seed.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		_ = seed.Close()
		t.Fatalf("seeding schema_migrations: %v", err)
	}
	if _, err := seed.Exec(`INSERT INTO schema_migrations(version) VALUES (99)`); err != nil {
		_ = seed.Close()
		t.Fatalf("seeding future version: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("closing seed db: %v", err)
	}

	_, err = Open(paths)
	if err == nil {
		t.Fatal("Open() = nil, want refusal on a newer-than-known schema version")
	}
	if !strings.Contains(err.Error(), "newer than the newest known migration") {
		t.Fatalf("err = %q, want the newer-than-known refusal", err)
	}
}

func TestNewestBackupFindsNewest(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"astradew-20240101-000000.db", "astradew-20240201-000000.db", "notes.txt", "astradew-20240115-000000.db"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("seeding %s: %v", name, err)
		}
	}
	got, err := NewestBackup(dir)
	if err != nil {
		t.Fatalf("NewestBackup() error = %v", err)
	}
	if want := filepath.Join(dir, "astradew-20240201-000000.db"); got != want {
		t.Fatalf("NewestBackup() = %q, want %q", got, want)
	}
}

func TestNewestBackupErrorsWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewestBackup(dir); err == nil {
		t.Fatal("NewestBackup() = nil, want an error when no backup exists")
	}
	if _, err := NewestBackup(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("NewestBackup() = nil, want an error for a missing directory")
	}
}

func TestStartFreshPreservingMovesFileAside(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, DBFileName)
	sentinel := []byte("precious library state")
	if err := os.WriteFile(dbPath, sentinel, 0o600); err != nil {
		t.Fatalf("seeding db: %v", err)
	}

	preserved, err := StartFreshPreserving(dbPath)
	if err != nil {
		t.Fatalf("StartFreshPreserving() error = %v", err)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("original %q still present: the next startup must create fresh", dbPath)
	}
	kept, err := os.ReadFile(preserved)
	if err != nil {
		t.Fatalf("reading preserved file: %v", err)
	}
	if string(kept) != string(sentinel) {
		t.Fatal("preserved file contents differ: the old database was not kept intact")
	}
}

func TestStartFreshPreservingErrorsWithoutFile(t *testing.T) {
	if _, err := StartFreshPreserving(filepath.Join(t.TempDir(), DBFileName)); err == nil {
		t.Fatal("StartFreshPreserving() = nil, want an error when no database exists")
	}
}
