// Package approot resolves and prepares Astradew's application data root.
//
// The root is a single Astradew/ directory under the framework's per-user
// data directory (application.Path(application.PathDataHome)), holding every
// product subdirectory from the PRD layout: database, library, profiles,
// backups, cache, logs, temp. Resolution and creation go through
// ResolveWithBase so tests inject t.TempDir() instead of the real data home.
package approot

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Zendevve/astradew/internal/apperror"
)

// AppDirName is the single directory Astradew owns under the data home.
const AppDirName = "Astradew"

// Product subdirectory names under the root (PRD 3.3 layout plus cache/,
// logs/, temp/).
const (
	DirDatabase = "database"
	DirLibrary  = "library"
	DirProfiles = "profiles"
	DirBackups  = "backups"
	DirCache    = "cache"
	DirLogs     = "logs"
	DirTemp     = "temp"
)

// Paths is the resolved application data layout. The JSON keys are the
// contract the frontend binds to.
type Paths struct {
	Root     string `json:"root"`
	Database string `json:"database"`
	Library  string `json:"library"`
	Profiles string `json:"profiles"`
	Backups  string `json:"backups"`
	Cache    string `json:"cache"`
	Logs     string `json:"logs"`
	Temp     string `json:"temp"`
}

// Dirs returns every directory the root owns, root first, in stable order.
func (p Paths) Dirs() []string {
	return []string{p.Root, p.Database, p.Library, p.Profiles, p.Backups, p.Cache, p.Logs, p.Temp}
}

// DataHome reports the framework's per-user data directory,
// application.Path(application.PathDataHome), never hand-rolled per-platform
// logic. That helper delegates to github.com/adrg/xdg, whose Windows mapping
// (paths_windows.go initBaseDirs) derives dataHome from the Local known
// folder (FOLDERID_LocalAppData, %LOCALAPPDATA%) — the local non-roaming
// profile — while the roaming profile (FOLDERID_RoamingAppData, %APPDATA%)
// feeds only the lower-priority search dirs. A multi-gigabyte Library must
// never roam, so the non-roaming property is asserted by
// TestDataHomeIsLocalNeverRoaming, not assumed.
func DataHome() string {
	return application.Path(application.PathDataHome)
}

// Resolve resolves the data root under the real data home, creates the full
// product layout on demand, and proves the root writable. Any failure refuses
// with a recoverable APPROOT_UNUSABLE *apperror.AppError naming the path and
// the reason; callers must not continue as if startup succeeded, and nothing
// here silently repairs a broken root.
func Resolve() (Paths, error) {
	return ResolveWithBase(DataHome())
}

// ResolveWithBase resolves the data root under base (normally DataHome()).
// Tests pass t.TempDir() so the real LOCALAPPDATA is never touched.
func ResolveWithBase(base string) (Paths, error) {
	if base == "" {
		return Paths{}, apperror.NewRecoverable(apperror.CodeAppRootUnusable, "application data root unusable", "empty base directory: no data-home location to resolve under")
	}
	root := filepath.Join(base, AppDirName)
	paths := Paths{
		Root:     root,
		Database: filepath.Join(root, DirDatabase),
		Library:  filepath.Join(root, DirLibrary),
		Profiles: filepath.Join(root, DirProfiles),
		Backups:  filepath.Join(root, DirBackups),
		Cache:    filepath.Join(root, DirCache),
		Logs:     filepath.Join(root, DirLogs),
		Temp:     filepath.Join(root, DirTemp),
	}
	for _, dir := range paths.Dirs() {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Paths{}, unusable(dir, err)
		}
	}
	if err := probeWritable(root); err != nil {
		return Paths{}, unusable(root, err)
	}
	return paths, nil
}

// unusable builds the refusal error: recoverable, naming the path and why it
// cannot serve as application data.
func unusable(path string, err error) *apperror.AppError {
	return apperror.NewRecoverable(apperror.CodeAppRootUnusable, "application data root unusable", fmt.Sprintf("%s: %s", path, err))
}

// probeWritable proves dir accepts writes by creating and removing a temp
// file inside it. MkdirAll succeeding is not enough: the directories may
// pre-exist under read-only permissions.
func probeWritable(dir string) error {
	probe, err := os.CreateTemp(dir, ".writability-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}
