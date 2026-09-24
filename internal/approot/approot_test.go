package approot

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Zendevve/astradew/internal/apperror"
)

// requireUnusable asserts err is a recoverable APPROOT_UNUSABLE *AppError
// whose details name the offending path.
func requireUnusable(t *testing.T, err error, pathFrag string) {
	t.Helper()
	if err == nil {
		t.Fatal("err = nil, want recoverable APPROOT_UNUSABLE")
	}
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("err type = %T, want *apperror.AppError", err)
	}
	if appErr.Code != apperror.CodeAppRootUnusable {
		t.Fatalf("code = %q, want %q", appErr.Code, apperror.CodeAppRootUnusable)
	}
	if !appErr.Recoverable {
		t.Fatal("recoverable = false, want true: the user can fix the path and retry")
	}
	if !strings.Contains(appErr.Details, pathFrag) {
		t.Fatalf("details = %q, want it to name path %q", appErr.Details, pathFrag)
	}
}

func requireDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("%q is not a directory", path)
	}
}

func TestResolveWithBaseCreatesProductLayout(t *testing.T) {
	base := t.TempDir()

	got, err := ResolveWithBase(base)
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}

	root := filepath.Join(base, AppDirName)
	want := map[string]string{
		"Root":     root,
		"Database": filepath.Join(root, DirDatabase),
		"Library":  filepath.Join(root, DirLibrary),
		"Profiles": filepath.Join(root, DirProfiles),
		"Backups":  filepath.Join(root, DirBackups),
		"Cache":    filepath.Join(root, DirCache),
		"Logs":     filepath.Join(root, DirLogs),
		"Temp":     filepath.Join(root, DirTemp),
	}
	fields := map[string]string{
		"Root":     got.Root,
		"Database": got.Database,
		"Library":  got.Library,
		"Profiles": got.Profiles,
		"Backups":  got.Backups,
		"Cache":    got.Cache,
		"Logs":     got.Logs,
		"Temp":     got.Temp,
	}
	for field, wantPath := range want {
		if fields[field] != wantPath {
			t.Errorf("Paths.%s = %q, want %q", field, fields[field], wantPath)
		}
		requireDir(t, wantPath)
	}
}

func TestResolveRefusesFileWhereDirectoryBelongs(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, AppDirName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("seeding root: %v", err)
	}
	blocker := filepath.Join(root, DirLogs)
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("seeding blocker file: %v", err)
	}

	_, err := ResolveWithBase(base)
	requireUnusable(t, err, blocker)
}

func TestResolveRefusesBaseThatIsAFile(t *testing.T) {
	base := t.TempDir()
	fileBase := filepath.Join(base, "afile")
	if err := os.WriteFile(fileBase, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("seeding file base: %v", err)
	}

	_, err := ResolveWithBase(fileBase)
	requireUnusable(t, err, filepath.Join(fileBase, AppDirName))
}

func TestResolveRefusesUnwritableBase(t *testing.T) {
	base := t.TempDir()
	if err := os.Chmod(base, 0o555); err != nil {
		t.Fatalf("chmod base read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(base, 0o700) })

	// Windows ignores the read-only bit on directories for write access, so
	// a chmod that denies nothing must not masquerade as a refusal test.
	probe, err := os.CreateTemp(base, ".probe-*")
	if err == nil {
		_ = probe.Close()
		_ = os.Remove(probe.Name())
		t.Skip("platform does not enforce directory write bits; unwritable parents are covered by TestResolveRefusesBaseThatIsAFile")
	}

	_, err = ResolveWithBase(base)
	requireUnusable(t, err, filepath.Join(base, AppDirName))
}

func TestResolveRefusesEmptyBase(t *testing.T) {
	_, err := ResolveWithBase("")
	requireUnusable(t, err, "empty base directory")
}

// DataHome must resolve through the framework helper to the local
// non-roaming profile. Verified against the module cache:
// wails path.go maps PathDataHome to xdg.DataHome, and adrg/xdg
// paths_windows.go initBaseDirs sets dataHome from kf.localAppData, which
// initKnownFolders derives from FOLDERID_LocalAppData (%LOCALAPPDATA%) —
// never FOLDERID_RoamingAppData (%APPDATA%), which feeds only the
// lower-priority search dirs. This test pins the runtime property: the
// resolved home must not sit at or under the roaming profile.
func TestDataHomeIsLocalNeverRoaming(t *testing.T) {
	home := DataHome()
	if home == "" {
		t.Fatal("DataHome() = empty, want the framework data-home directory")
	}
	if runtime.GOOS != "windows" {
		t.Skip("non-roaming assertion is Windows-specific; DataHome() is non-empty")
	}
	roaming := os.Getenv("APPDATA")
	if roaming == "" {
		t.Skip("no roaming profile in this environment")
	}
	rel, err := filepath.Rel(roaming, home)
	if err != nil {
		t.Fatalf("rel(roaming, home): %v", err)
	}
	if rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..") {
		t.Fatalf("DataHome() = %q sits at or under roaming %q, want the local non-roaming profile", home, roaming)
	}
}
