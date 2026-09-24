// Package store opens Astradew's SQLite database and applies its numbered
// migrations, failing closed.
//
// The database file lives at <paths.Database>/astradew.db and is opened with
// a single connection (SetMaxOpenConns(1)) so the required PRAGMAs hold for
// every statement. Schema changes ship only as numbered NNNN_name.sql files
// embedded in the binary (migrations/), applied in order inside one
// transaction each, with applied versions recorded in schema_migrations —
// never mutated ad hoc, per ADR 0008.
//
// Before applying any pending migration to an already-existing database file,
// the pre-migration file is copied to <backups>/astradew-<timestamp>.db. A
// fresh directory needs no backup: there is nothing to lose.
//
// Any open or migration failure refuses with a typed recoverable
// *apperror.AppError whose details name the failing version, the database
// path, and both explicit repair actions with concrete paths (restore the
// newest backup, or start fresh while preserving the old file), per ADR 0010.
// Nothing is ever silently repaired or rebuilt.
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Zendevve/astradew/internal/apperror"
	"github.com/Zendevve/astradew/internal/approot"
)

// DBFileName is the SQLite file inside the database directory.
const DBFileName = "astradew.db"

// backupPrefix prefixes every pre-migration backup file in the backups
// directory; the remainder is a filename-safe UTC timestamp.
const backupPrefix = "astradew-"

// timestampFormat is filename-safe (no colons) and sorts lexicographically in
// chronological order, which is what NewestBackup relies on.
const timestampFormat = "20060102-150405"

// migrationsDir holds the numbered migration files, both in the embedded FS
// and in any fs.FS override passed to OpenWithMigrations or Migrate.
const migrationsDir = "migrations"

// migrationFilePattern accepts NNNN_name.sql and captures the leading number.
var migrationFilePattern = regexp.MustCompile(`^(\d+)_.*\.sql$`)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// Store is an open Astradew database at a known schema version. It stays open
// for the life of the process; services build on DB.
type Store struct {
	db      *sql.DB
	path    string
	version int
}

// Status is the observable state of an open Store: where it lives and what
// schema version it serves.
type Status struct {
	Path    string `json:"path"`
	Version int    `json:"version"`
}

// DB exposes the underlying pool for services built on the store.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Path reports the database file this store was opened on.
func (s *Store) Path() string {
	return s.path
}

// Version reports the schema version this store migrated to.
func (s *Store) Version() int {
	return s.version
}

// Status reports the path and schema version of this store.
func (s *Store) Status() Status {
	return Status{Path: s.path, Version: s.version}
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Open opens the database at <paths.Database>/astradew.db and applies the
// embedded migrations. Any failure refuses with a typed recoverable
// *apperror.AppError; callers must not continue as if startup succeeded.
func Open(paths approot.Paths) (*Store, error) {
	return OpenWithMigrations(paths, embeddedMigrations)
}

// OpenWithMigrations opens the database like Open but applies migrations
// from migrationsFS instead of the embedded files, so tests can inject bad
// SQL without touching the embedded migrations. migrationsFS must expose the
// numbered files under a migrations/ directory, the same layout as the
// embedded FS.
func OpenWithMigrations(paths approot.Paths, migrationsFS fs.FS) (*Store, error) {
	dbPath := filepath.Join(paths.Database, DBFileName)
	if err := os.MkdirAll(paths.Database, 0o755); err != nil {
		return nil, openFailed(dbPath, err)
	}
	if err := os.MkdirAll(paths.Backups, 0o755); err != nil {
		return nil, openFailed(dbPath, err)
	}
	existed := fileExists(dbPath)

	db, err := openPool(dbPath)
	if err != nil {
		return nil, openFailed(dbPath, err)
	}
	pending, current, err := pendingMigrations(db, migrationsFS)
	if err != nil {
		_ = db.Close()
		return nil, openFailed(dbPath, err)
	}

	backupPath := ""
	if len(pending) > 0 && existed {
		// Copy the pre-migration file before touching it. The pool closes
		// first so a WAL sidecar checkpoints into the main file and the
		// copy is whole. Fresh files skip this: nothing to lose.
		if err := db.Close(); err != nil {
			return nil, openFailed(dbPath, err)
		}
		backupPath, err = takeBackup(dbPath, paths.Backups)
		if err != nil {
			return nil, openFailed(dbPath, err)
		}
		db, err = openPool(dbPath)
		if err != nil {
			return nil, openFailed(dbPath, err)
		}
	}

	version, failed := applyMigrations(db, pending, current)
	if failed != nil {
		_ = db.Close()
		return nil, migrationFailed(dbPath, failed.version, failed.cause, backupPath, paths.Backups)
	}
	return &Store{db: db, path: dbPath, version: version}, nil
}

// NewestBackup returns the most recent pre-migration backup in backupsDir.
// Backup names sort chronologically, so the newest is the lexicographic
// maximum. It errors when the directory holds no backup.
func NewestBackup(backupsDir string) (string, error) {
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		return "", fmt.Errorf("listing backups in %s: %w", backupsDir, err)
	}
	newest := ""
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, backupPrefix) || !strings.HasSuffix(name, ".db") {
			continue
		}
		if name > newest {
			newest = name
		}
	}
	if newest == "" {
		return "", fmt.Errorf("no database backups in %s", backupsDir)
	}
	return filepath.Join(backupsDir, newest), nil
}

// StartFreshPreserving moves the database file aside so the next startup
// creates a fresh one. The old file is renamed, never deleted, and its path
// is returned. WAL sidecars move with it when present.
func StartFreshPreserving(dbPath string) (string, error) {
	if !fileExists(dbPath) {
		return "", fmt.Errorf("no database at %s: nothing to preserve", dbPath)
	}
	preserved := dbPath + ".preserved-" + time.Now().UTC().Format(timestampFormat)
	if err := os.Rename(dbPath, preserved); err != nil {
		return "", fmt.Errorf("moving %s aside to %s: %w", dbPath, preserved, err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		_ = os.Rename(dbPath+suffix, preserved+suffix)
	}
	return preserved, nil
}

// openPool opens the SQLite file with a single connection and establishes the
// required PRAGMAs: foreign-key enforcement, WAL mode, and a busy timeout.
func openPool(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	for _, pragma := range []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	return db, nil
}

// migration is one numbered migration file in apply order.
type migration struct {
	version int
	name    string
	sql     string
}

// migrationFailure names the migration whose transaction did not commit.
type migrationFailure struct {
	version int
	cause   error
}

// pendingMigrations ensures the version table exists, reads the current
// version, and returns the migrations still to apply, in order.
func pendingMigrations(db *sql.DB, migrationsFS fs.FS) (pending []migration, current int, err error) {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return nil, 0, fmt.Errorf("ensuring schema_migrations: %w", err)
	}
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return nil, 0, fmt.Errorf("reading schema version: %w", err)
	}
	all, err := loadMigrations(migrationsFS)
	if err != nil {
		return nil, 0, err
	}
	newest := 0
	for _, m := range all {
		if m.version > newest {
			newest = m.version
		}
	}
	if current > newest {
		return nil, 0, fmt.Errorf("schema version %d is newer than the newest known migration %04d: refusing to run against schema this code does not describe", current, newest)
	}
	for _, m := range all {
		if m.version > current {
			pending = append(pending, m)
		}
	}
	return pending, current, nil
}

// loadMigrations parses and sorts every numbered migration in migrationsFS.
// Malformed names, duplicate versions, and empty files fail loudly: a
// migration the code cannot describe exactly is one it must not apply.
func loadMigrations(migrationsFS fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", migrationsDir, err)
	}
	seen := map[int]string{}
	var out []migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		match := migrationFilePattern.FindStringSubmatch(entry.Name())
		if match == nil {
			return nil, fmt.Errorf("migration file %q does not match NNNN_name.sql", entry.Name())
		}
		version, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, fmt.Errorf("migration file %q has a bad version number: %w", entry.Name(), err)
		}
		if prev, dup := seen[version]; dup {
			return nil, fmt.Errorf("duplicate migration version %04d in %q and %q", version, prev, entry.Name())
		}
		seen[version] = entry.Name()
		raw, err := fs.ReadFile(migrationsFS, migrationsDir+"/"+entry.Name())
		if err != nil {
			return nil, fmt.Errorf("reading migration %q: %w", entry.Name(), err)
		}
		if strings.TrimSpace(string(raw)) == "" {
			return nil, fmt.Errorf("migration %q is empty", entry.Name())
		}
		out = append(out, migration{version: version, name: entry.Name(), sql: string(raw)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// applyMigrations runs each pending migration inside its own transaction and
// records its version on commit. A failure rolls its transaction back and
// reports the failing version; earlier migrations stay committed and later
// ones never run. The returned version is the newest committed one.
func applyMigrations(db *sql.DB, pending []migration, current int) (int, *migrationFailure) {
	version := current
	for _, m := range pending {
		tx, err := db.Begin()
		if err != nil {
			return version, &migrationFailure{version: m.version, cause: err}
		}
		if _, err := tx.Exec(m.sql); err != nil {
			_ = tx.Rollback()
			return version, &migrationFailure{version: m.version, cause: err}
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, m.version); err != nil {
			_ = tx.Rollback()
			return version, &migrationFailure{version: m.version, cause: err}
		}
		if err := tx.Commit(); err != nil {
			return version, &migrationFailure{version: m.version, cause: err}
		}
		version = m.version
	}
	return version, nil
}

// takeBackup copies the pre-migration database file into backupsDir and
// returns the backup path.
func takeBackup(dbPath, backupsDir string) (string, error) {
	dest := filepath.Join(backupsDir, backupPrefix+time.Now().UTC().Format(timestampFormat)+".db")
	if err := copyFile(dbPath, dest); err != nil {
		return "", fmt.Errorf("backing up %s to %s: %w", dbPath, dest, err)
	}
	return dest, nil
}

// copyFile copies src to dest, fsyncing dest so a crash cannot leave a torn
// backup behind.
func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// fileExists reports whether path names an existing non-directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// openFailed builds the refusal for a database that cannot be opened or
// brought to a known schema state.
func openFailed(dbPath string, cause error) *apperror.AppError {
	return apperror.NewRecoverable(apperror.CodeStoreOpenFailed,
		fmt.Sprintf("cannot open database at %s: %s", dbPath, cause),
		fmt.Sprintf("database: %s\ncause: %s\nrepair (a) restore the newest backup over %q, then restart\nrepair (b) start fresh while preserving the old file: move %q aside, then restart; the old file is kept, never deleted",
			dbPath, cause, dbPath, dbPath))
}

// migrationFailed builds the refusal for a migration that did not commit.
// backupPath names the pre-migration backup taken by this run, or is empty
// when there was nothing to back up; either way details point at the newest
// backup on disk when one exists.
func migrationFailed(dbPath string, version int, cause error, backupPath, backupsDir string) *apperror.AppError {
	newest, newestErr := NewestBackup(backupsDir)
	if backupPath != "" {
		newest, newestErr = backupPath, nil
	}
	var details strings.Builder
	fmt.Fprintf(&details, "database: %s\nfailing migration: %04d\ncause: %s\n", dbPath, version, cause)
	if newestErr == nil {
		fmt.Fprintf(&details, "pre-migration backup: %s\n", newest)
		fmt.Fprintf(&details, "repair (a) restore the newest backup: copy %q over %q, then restart\n", newest, dbPath)
	} else {
		fmt.Fprintf(&details, "pre-migration backup: none found in %s\n", backupsDir)
		fmt.Fprintf(&details, "repair (a) restore a backup: copy your newest %s backup over %q, then restart\n", backupsDir, dbPath)
	}
	fmt.Fprintf(&details, "repair (b) start fresh while preserving the old file: move %q aside (for example to %q.preserved-<timestamp>), then restart; the old file is kept, never deleted",
		dbPath, dbPath)
	return apperror.NewRecoverable(apperror.CodeStoreMigrationFailed,
		fmt.Sprintf("migration %04d failed for database at %s: %s", version, dbPath, cause),
		details.String())
}
