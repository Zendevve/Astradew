package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Zendevve/astradew/internal/apperror"
	"github.com/Zendevve/astradew/internal/approot"
	"github.com/Zendevve/astradew/internal/detect"
	"github.com/Zendevve/astradew/internal/settings"
)

// seedGameDir writes a minimal valid game directory: the game DLL plus,
// when smapi is true, the full Windows SMAPI signal set.
func seedGameDir(t *testing.T, dir string, smapi bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seeding dir: %v", err)
	}
	names := []string{"Stardew Valley.dll"}
	if smapi {
		names = append(names,
			"StardewModdingAPI.exe",
			"StardewModdingAPI.dll",
			"StardewModdingAPI.deps.json",
			"StardewModdingAPI.runtimeconfig.json",
			"StardewModdingAPI.exe.config",
			"StardewModdingAPI.xml",
			"steam_appid.txt",
		)
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("seeding %s: %v", name, err)
		}
	}
	if smapi {
		if err := os.MkdirAll(filepath.Join(dir, "smapi-internal"), 0o755); err != nil {
			t.Fatalf("seeding smapi-internal: %v", err)
		}
	}
}

// openInstallStore opens a real migrated database under a temp root for
// install tests. The store is real so rows prove durability.
func openInstallStore(t *testing.T) (*ApplicationService, approot.Paths) {
	t.Helper()
	paths, err := approot.ResolveWithBase(t.TempDir())
	if err != nil {
		t.Fatalf("ResolveWithBase() error = %v", err)
	}
	db := openTestStore(t, paths)
	return NewWithPathsAndStore("Astradew", "1.4.2", paths, db), paths
}

// installCode extracts the typed code from an error, failing when none.
func installCode(t *testing.T, err error) apperror.Code {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error %#v carries no typed code", err)
	}
	return appErr.Code
}

// Add happy path: picking a valid game folder records a durable row as the
// primary card with unknown versions (nil) and the SMAPI entry point/state.
func TestAddGameInstallHappy(t *testing.T) {
	svc, _ := openInstallStore(t)
	dir := t.TempDir()
	seedGameDir(t, filepath.Join(dir, "game"), true)

	view, err := svc.AddGameInstall(filepath.Join(dir, "game"))
	if err != nil {
		t.Fatalf("AddGameInstall() error = %v", err)
	}
	if view.Path == "" || view.Source != "manual" {
		t.Fatalf("view = %+v, want stored path with manual source", view)
	}
	if view.GameVersion != nil || view.SmapiVersion != nil {
		t.Fatalf("view = %+v, want nil versions (unknown by contract)", view)
	}
	if view.SmapiExePath == nil || *view.SmapiExePath != "StardewModdingAPI.exe" {
		t.Fatalf("view = %+v, want the .exe entry point", view)
	}
	if view.SmapiState != "complete" {
		t.Fatalf("SmapiState = %q, want complete", view.SmapiState)
	}
	if !view.IsPrimary {
		t.Fatal("IsPrimary = false, want true: first add fills the vacuum")
	}
	list, err := svc.GameInstalls()
	if err != nil {
		t.Fatalf("GameInstalls() error = %v", err)
	}
	if len(list) != 1 || !list[0].IsPrimary {
		t.Fatalf("GameInstalls() = %+v, want one primary row", list)
	}
}

// The three refusals refuse with their typed codes and name recovery: a
// folder with no game (GAME_NOT_FOUND), the installer bundle (hint in
// Details), legacy (GAME_LEGACY), and corrupt (GAME_INVALID).
func TestAddGameInstallRefusals(t *testing.T) {
	svc, _ := openInstallStore(t)
	base := t.TempDir()

	empty := filepath.Join(base, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatalf("seeding empty: %v", err)
	}
	if _, err := svc.AddGameInstall(empty); installCode(t, err) != apperror.CodeGameNotFound {
		t.Fatalf("empty dir code = %v, want GAME_NOT_FOUND", err)
	}

	bundle := filepath.Join(base, "bundle")
	if err := os.MkdirAll(filepath.Join(bundle, "internal"), 0o755); err != nil {
		t.Fatalf("seeding bundle: %v", err)
	}
	for _, name := range []string{"install on Windows.bat", "internal/install.dat"} {
		if err := os.WriteFile(filepath.Join(bundle, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("seeding %s: %v", name, err)
		}
	}
	_, err := svc.AddGameInstall(bundle)
	if installCode(t, err) != apperror.CodeGameNotFound {
		t.Fatalf("bundle code = %v, want GAME_NOT_FOUND", err)
	}
	var bundleErr *apperror.AppError
	_ = errors.As(err, &bundleErr)
	if got := bundleErr.Details; !containsFold(got, "run it instead") {
		t.Fatalf("bundle details = %q, want the run-the-installer hint", got)
	}
	if got := bundleErr.Details; !strings.HasPrefix(got, detect.InstallerBundleMarker) {
		t.Fatalf("bundle details = %q, want machine-stable prefix %q", got, detect.InstallerBundleMarker)
	}

	legacy := filepath.Join(base, "legacy")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatalf("seeding legacy: %v", err)
	}
	for _, name := range []string{"Stardew Valley.exe", "MonoGame.Framework.dll"} {
		if err := os.WriteFile(filepath.Join(legacy, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("seeding %s: %v", name, err)
		}
	}
	if _, err := svc.AddGameInstall(legacy); installCode(t, err) != apperror.CodeGameLegacy {
		t.Fatalf("legacy code = %v, want GAME_LEGACY", err)
	}

	if _, err := svc.AddGameInstall(filepath.Join(base, "missing")); installCode(t, err) != apperror.CodeGameInvalid {
		t.Fatalf("unreadable code = %v, want GAME_INVALID", err)
	}
}

// Refusal codes survive the real boundary mechanism: through the bound
// method's call path with the MarshalError hook, the marshalled error still
// names the typed code.
func TestAddGameInstallRefusalSurvivesBoundaryCall(t *testing.T) {
	_ = application.New(application.Options{})
	bindings := application.NewBindings(nil, nil)
	svc, _ := openInstallStore(t)
	if err := bindings.Add(application.NewServiceWithOptions(svc, application.ServiceOptions{
		MarshalError: apperror.MarshalError,
	})); err != nil {
		t.Fatalf("bindings.Add() error = %v", err)
	}
	bound := bindings.Get(&application.CallOptions{
		MethodName: "github.com/Zendevve/astradew/internal/app.ApplicationService.AddGameInstall",
	})
	if bound == nil {
		t.Fatal("bound AddGameInstall method not found")
	}
	empty := t.TempDir()
	arg, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshalling path: %v", err)
	}
	_, err = bound.Call(context.TODO(), []json.RawMessage{arg})
	var callErr *application.CallError
	if !errors.As(err, &callErr) {
		t.Fatalf("Call err = %#v, want *application.CallError", err)
	}
	var payload apperror.AppError
	if err := json.Unmarshal(callErr.Cause.(json.RawMessage), &payload); err != nil {
		t.Fatalf("decoding cause %v: %v", callErr.Cause, err)
	}
	if payload.Code != apperror.CodeGameNotFound {
		t.Fatalf("boundary error code = %q, want GAME_NOT_FOUND", payload.Code)
	}
}

// Never-steal pointer matrix: a second add never moves a healthy primary;
// switching happens only through the explicit per-row choice.
func TestAddSecondInstallNeverStealsHealthyPrimary(t *testing.T) {
	svc, _ := openInstallStore(t)
	base := t.TempDir()
	firstDir := filepath.Join(base, "first")
	secondDir := filepath.Join(base, "second")
	for _, dir := range []string{firstDir, secondDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("seeding dir: %v", err)
		}
		seedGameDir(t, dir, false)
	}
	first, err := svc.AddGameInstall(firstDir)
	if err != nil {
		t.Fatalf("AddGameInstall(first) error = %v", err)
	}
	second, err := svc.AddGameInstall(secondDir)
	if err != nil {
		t.Fatalf("AddGameInstall(second) error = %v", err)
	}
	if second.IsPrimary {
		t.Fatal("second IsPrimary = true, want false: never steal a healthy primary")
	}
	list, err := svc.GameInstalls()
	if err != nil {
		t.Fatalf("GameInstalls() error = %v", err)
	}
	for _, view := range list {
		if view.ID == first.ID && !view.IsPrimary {
			t.Fatal("first lost primary: a healthy pointer survives a second add")
		}
	}
	if err := svc.SetPrimaryGameInstall(second.ID); err != nil {
		t.Fatalf("SetPrimaryGameInstall(second) error = %v", err)
	}
	list, err = svc.GameInstalls()
	if err != nil {
		t.Fatalf("GameInstalls() error = %v", err)
	}
	for _, view := range list {
		if view.ID == second.ID && !view.IsPrimary {
			t.Fatal("second not primary after explicit choice")
		}
		if view.ID == first.ID && view.IsPrimary {
			t.Fatal("first still primary after explicit switch")
		}
	}
}

// Re-pick refreshes the existing row instead of duplicating it.
func TestAddGameInstallRepicksRefresh(t *testing.T) {
	svc, _ := openInstallStore(t)
	dir := t.TempDir()
	game := filepath.Join(dir, "game")
	if err := os.MkdirAll(game, 0o755); err != nil {
		t.Fatalf("seeding dir: %v", err)
	}
	seedGameDir(t, game, false)
	first, err := svc.AddGameInstall(game)
	if err != nil {
		t.Fatalf("AddGameInstall() error = %v", err)
	}
	second, err := svc.AddGameInstall(game + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "game")
	if err != nil {
		t.Fatalf("re-pick AddGameInstall() error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("re-pick id = %d, want existing %d", second.ID, first.ID)
	}
	list, err := svc.GameInstalls()
	if err != nil {
		t.Fatalf("GameInstalls() error = %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("rows = %d, want 1 after re-pick", len(list))
	}
}

// SetPrimaryGameInstall with an unknown id refuses SETTING_INVALID and
// leaves the stored pointer untouched.
func TestSetPrimaryGameInstallUnknownRefuses(t *testing.T) {
	svc, _ := openInstallStore(t)
	dir := t.TempDir()
	game := filepath.Join(dir, "game")
	if err := os.MkdirAll(game, 0o755); err != nil {
		t.Fatalf("seeding dir: %v", err)
	}
	seedGameDir(t, game, false)
	first, err := svc.AddGameInstall(game)
	if err != nil {
		t.Fatalf("AddGameInstall() error = %v", err)
	}
	if err := svc.SetPrimaryGameInstall(first.ID + 999); installCode(t, err) != apperror.CodeSettingInvalid {
		t.Fatalf("unknown id code = %v, want SETTING_INVALID", err)
	}
	got, err := svc.GetSetting(settings.PrimaryGameInstallIDKey)
	if err != nil {
		t.Fatalf("GetSetting(pointer) error = %v", err)
	}
	if id, ok := got.(int); !ok || int64(id) != first.ID {
		t.Fatalf("pointer = %#v, want preserved %d", got, first.ID)
	}
}

// Nil-store honesty: GameInstalls/AddGameInstall/SetPrimaryGameInstall
// refuse with SETTING_UNAVAILABLE, never fabricated rows.
func TestGameInstallsRefuseNilStore(t *testing.T) {
	bare := New("Astradew", "1.4.2")
	if _, err := bare.GameInstalls(); installCode(t, err) != apperror.CodeSettingUnavailable {
		t.Fatalf("GameInstalls() nil-store code = %v, want SETTING_UNAVAILABLE", err)
	}
	if _, err := bare.AddGameInstall(t.TempDir()); installCode(t, err) != apperror.CodeSettingUnavailable {
		t.Fatalf("AddGameInstall() nil-store code = %v, want SETTING_UNAVAILABLE", err)
	}
	if err := bare.SetPrimaryGameInstall(1); installCode(t, err) != apperror.CodeSettingUnavailable {
		t.Fatalf("SetPrimaryGameInstall() nil-store code = %v, want SETTING_UNAVAILABLE", err)
	}
}

// A DB write failure surfaces the error, never a fabricated view: an insert
// trigger aborts the UpsertGameInstall write while reads still succeed, so
// without the err check AddGameInstall would continue with a zero install ID
// and return a misleading view. The trigger is dropped by cleanup.
func TestAddGameInstallClosedStoreReturnsWriteError(t *testing.T) {
	svc, _ := openInstallStore(t)
	dir := t.TempDir()
	seedGameDir(t, filepath.Join(dir, "game"), false)
	if _, err := svc.db.DB().Exec(`CREATE TRIGGER abort_game_install_insert BEFORE INSERT ON game_installs BEGIN SELECT RAISE(ABORT, 'forced write failure'); END`); err != nil {
		t.Fatalf("creating abort trigger: %v", err)
	}
	view, err := svc.AddGameInstall(filepath.Join(dir, "game"))
	if err == nil {
		t.Fatalf("AddGameInstall() = %+v, want the write failure, never a fabricated view", view)
	}
	if view != (GameInstallView{}) {
		t.Fatalf("AddGameInstall() view = %+v, want zero view on write failure", view)
	}
}

func containsFold(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	lower := func(r byte) byte {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}
	for i := range len(s) - len(sub) + 1 {
		match := true
		for j := range len(sub) {
			if lower(s[i+j]) != lower(sub[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
