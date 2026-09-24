// Package store owns game_installs rows, so it owns the path rule.
// CanonicalGamePath returns the canonical form stored in
// game_installs.path: filepath.Abs + filepath.Clean, OS-native
// separators, no trailing separator, no symlink resolution, no case
// folding. Pure lexical — no I/O, safe on unmounted volumes.
package store

import (
	"database/sql"
	"path/filepath"
	"runtime"
	"strings"
)

// GameSourceAllowList is the Go-side allow-list for game_installs.source
// (decision #17): steam, gog, manual, other. The column deliberately has no
// SQL CHECK so a future source written by a newer build survives old
// readers; unknown sources pass through unread-but-preserved.
var GameSourceAllowList = []string{"steam", "gog", "manual", "other"}

// GameInstall is one game_installs row. Nullable columns arrive as nil when
// NULL: NULL means present-but-unknown (version) or not-yet-detected/absent
// (entry point), never guessed.
type GameInstall struct {
	ID           int64
	Path         string
	Source       string
	SmapiExePath *string
	GameVersion  *string
	SmapiVersion *string
}

// CanonicalGamePath returns the canonical form stored in
// game_installs.path: filepath.Abs + filepath.Clean, OS-native separators,
// no trailing separator, no symlink resolution, no case folding. Pure
// lexical — no I/O, safe on unmounted volumes.
func CanonicalGamePath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

// canonicalEqual reports whether two canonical paths name the same
// directory: EqualFold on windows/darwin (case-insensitive filesystems),
// strict == elsewhere (Linux stays exact).
func canonicalEqual(a, b string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// ValidGameSource reports whether source is in the Go-side allow-list.
func ValidGameSource(source string) bool {
	for _, allowed := range GameSourceAllowList {
		if source == allowed {
			return true
		}
	}
	return false
}

// ListGameInstalls returns every game_installs row ordered by id, selecting
// explicit columns (never SELECT *) so later columns do not break old
// readers.
func ListGameInstalls(db *sql.DB) ([]GameInstall, error) {
	rows, err := db.Query(`SELECT id, path, source, smapi_exe_path, game_version, smapi_version FROM game_installs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []GameInstall
	for rows.Next() {
		var install GameInstall
		var smapiExe, gameVersion, smapiVersion sql.NullString
		if err := rows.Scan(&install.ID, &install.Path, &install.Source, &smapiExe, &gameVersion, &smapiVersion); err != nil {
			return nil, err
		}
		if smapiExe.Valid {
			value := smapiExe.String
			install.SmapiExePath = &value
		}
		if gameVersion.Valid {
			value := gameVersion.String
			install.GameVersion = &value
		}
		if smapiVersion.Valid {
			value := smapiVersion.String
			install.SmapiVersion = &value
		}
		out = append(out, install)
	}
	return out, rows.Err()
}

// FindGameInstall reports the existing row matching candidate under the
// insert-time compare rule (CanonicalGamePath both sides; EqualFold on
// windows/darwin, strict == elsewhere). ok is false when no row matches.
func FindGameInstall(db *sql.DB, candidate string) (install GameInstall, ok bool, err error) {
	canonicalCandidate, err := CanonicalGamePath(candidate)
	if err != nil {
		return GameInstall{}, false, err
	}
	existing, err := ListGameInstalls(db)
	if err != nil {
		return GameInstall{}, false, err
	}
	for _, row := range existing {
		canonicalRow, err := CanonicalGamePath(row.Path)
		if err != nil {
			continue
		}
		if canonicalEqual(canonicalRow, canonicalCandidate) {
			return row, true, nil
		}
	}
	return GameInstall{}, false, nil
}

// GetGameInstall returns the game_installs row with id, or sql.ErrNoRows
// when no such row exists.
func GetGameInstall(db *sql.DB, id int64) (GameInstall, error) {
	var install GameInstall
	var smapiExe, gameVersion, smapiVersion sql.NullString
	if err := db.QueryRow(`SELECT id, path, source, smapi_exe_path, game_version, smapi_version FROM game_installs WHERE id = ?`, id).Scan(&install.ID, &install.Path, &install.Source, &smapiExe, &gameVersion, &smapiVersion); err != nil {
		return GameInstall{}, err
	}
	if smapiExe.Valid {
		value := smapiExe.String
		install.SmapiExePath = &value
	}
	if gameVersion.Valid {
		value := gameVersion.String
		install.GameVersion = &value
	}
	if smapiVersion.Valid {
		value := smapiVersion.String
		install.SmapiVersion = &value
	}
	return install, nil
}

// nullString maps a *string to a driver value: nil becomes NULL.
func nullString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

// UpsertGameInstall inserts a game_installs row for candidate (canonicalised
// before compare/insert) or updates the existing matching row's
// path/source/versions/smapi_exe_path/updated_at — never a duplicate. key is
// matched under the insert-time compare rule; the stored path is refreshed to
// the matched canonical form (post-Clean/Abs, no case folding). source
// outside the allow-list is preserved unread (no CHECK to trip on).
func UpsertGameInstall(db *sql.DB, candidate string, source string, smapiExePath, gameVersion, smapiVersion *string) (GameInstall, error) {
	canonical, err := CanonicalGamePath(candidate)
	if err != nil {
		return GameInstall{}, err
	}
	existing, found, err := FindGameInstall(db, canonical)
	if err != nil {
		return GameInstall{}, err
	}
	if found {
		if _, err := db.Exec(`UPDATE game_installs SET path = ?, source = ?, smapi_exe_path = ?, game_version = ?, smapi_version = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, canonical, source, nullString(smapiExePath), nullString(gameVersion), nullString(smapiVersion), existing.ID); err != nil {
			return GameInstall{}, err
		}
		return GetGameInstall(db, existing.ID)
	}
	result, err := db.Exec(`INSERT INTO game_installs(path, source, smapi_exe_path, game_version, smapi_version) VALUES(?, ?, ?, ?, ?)`, canonical, source, nullString(smapiExePath), nullString(gameVersion), nullString(smapiVersion))
	if err != nil {
		return GameInstall{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return GameInstall{}, err
	}
	return GetGameInstall(db, id)
}
