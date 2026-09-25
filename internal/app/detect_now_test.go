package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Zendevve/astradew/internal/apperror"
	"github.com/Zendevve/astradew/internal/discover"
	"github.com/Zendevve/astradew/internal/settings"
)

// stubDetectCandidates points the discovery pass at temp game dirs for one
// test: real on-disk fixtures through the real detector, never the machine.
func stubDetectCandidates(t *testing.T, candidates []discover.Candidate) {
	t.Helper()
	old := detectCandidates
	detectCandidates = func() []discover.Candidate { return candidates }
	t.Cleanup(func() { detectCandidates = old })
}

// stubLogCandidates silences the last-run log fallback so tests prove
// on-disk truth only.
func stubLogCandidates(t *testing.T) {
	t.Helper()
	old := smapiLogDirCandidates
	smapiLogDirCandidates = func() []string { return nil }
	t.Cleanup(func() { smapiLogDirCandidates = old })
}

// seedDetectDir writes a minimal game dir: the game DLL plus the full
// Windows SMAPI signal set when smapi is true.
func seedDetectDir(t *testing.T, dir string, smapi bool) {
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

// Zero finds: the pointer is untouched, the message guides to manual
// selection, and a healthy primary survives the failed pass.
func TestDetectNowZeroFindsKeepsPointer(t *testing.T) {
	svc, _ := openInstallStore(t)
	stubLogCandidates(t)
	base := t.TempDir()
	healthy := filepath.Join(base, "healthy")
	seedDetectDir(t, healthy, false)
	first, err := svc.AddGameInstall(healthy)
	if err != nil {
		t.Fatalf("AddGameInstall() error = %v", err)
	}
	stubDetectCandidates(t, []discover.Candidate{
		{Path: filepath.Join(base, "absent"), Source: discover.SourceSteam},
		{Path: filepath.Join(base, "empty"), Source: discover.SourceGOG},
	})
	if err := os.MkdirAll(filepath.Join(base, "empty"), 0o755); err != nil {
		t.Fatalf("seeding empty: %v", err)
	}
	result, err := svc.DetectNow()
	if err != nil {
		t.Fatalf("DetectNow() error = %v", err)
	}
	if len(result.Found) != 0 || result.NeedsChoice {
		t.Fatalf("result = %+v, want zero finds without a choice", result)
	}
	if result.Pointer != "kept-healthy" {
		t.Fatalf("Pointer = %q, want kept-healthy", result.Pointer)
	}
	if result.Message == "" {
		t.Fatal("Message is empty, want manual-pick guidance")
	}
	list, err := svc.GameInstalls()
	if err != nil {
		t.Fatalf("GameInstalls() error = %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("rows = %d, want the single healthy row to survive", len(list))
	}
	for _, view := range list {
		if view.ID == first.ID && !view.IsPrimary {
			t.Fatal("healthy primary moved on a failed pass")
		}
	}
}

// One find fills an unset pointer; a second pass with a healthy pointer
// never moves it.
func TestDetectNowOneFindFillsVacuumNeverMovesHealthy(t *testing.T) {
	svc, _ := openInstallStore(t)
	stubLogCandidates(t)
	base := t.TempDir()
	game := filepath.Join(base, "game")
	seedDetectDir(t, game, true)
	stubDetectCandidates(t, []discover.Candidate{{Path: game, Source: discover.SourceSteam}})

	result, err := svc.DetectNow()
	if err != nil {
		t.Fatalf("DetectNow() error = %v", err)
	}
	if result.Pointer != "adopted-vacuum" {
		t.Fatalf("Pointer = %q, want adopted-vacuum", result.Pointer)
	}
	if result.Adopted == nil || !result.Adopted.IsPrimary {
		t.Fatalf("result = %+v, want the single find adopted as primary", result)
	}
	if len(result.Found) != 1 || result.Found[0].Source != "steam" {
		t.Fatalf("Found = %+v, want one steam row", result.Found)
	}

	other := filepath.Join(base, "other")
	seedDetectDir(t, other, false)
	stubDetectCandidates(t, []discover.Candidate{{Path: other, Source: discover.SourceGOG}})
	second, err := svc.DetectNow()
	if err != nil {
		t.Fatalf("DetectNow() error = %v", err)
	}
	if second.Pointer != "kept-healthy" {
		t.Fatalf("Pointer = %q, want kept-healthy", second.Pointer)
	}
	if second.Adopted != nil {
		t.Fatalf("Adopted = %+v, want nil when the healthy pointer stays", second.Adopted)
	}
	list, err := svc.GameInstalls()
	if err != nil {
		t.Fatalf("GameInstalls() error = %v", err)
	}
	for _, view := range list {
		if view.ID == result.Adopted.ID && !view.IsPrimary {
			t.Fatal("first find lost primary to a later pass")
		}
	}
}

// One find repairs a stale (dangling) pointer.
func TestDetectNowOneFindRepairsStale(t *testing.T) {
	svc, _ := openInstallStore(t)
	stubLogCandidates(t)
	base := t.TempDir()
	game := filepath.Join(base, "game")
	seedDetectDir(t, game, false)
	dangling, err := svc.AddGameInstall(game)
	if err != nil {
		t.Fatalf("AddGameInstall() error = %v", err)
	}
	fresh := filepath.Join(base, "fresh")
	seedDetectDir(t, fresh, false)
	stubDetectCandidates(t, []discover.Candidate{{Path: fresh, Source: discover.SourceSteam}})
	// Dangle the pointer: point it at a row, then remove the row's meaning
	// by pointing past every id. Set cannot refuse, so write raw SQL.
	if _, err := svc.db.DB().Exec(`UPDATE settings SET value = ? WHERE key = ?`, "999001", settings.PrimaryGameInstallIDKey); err != nil {
		t.Fatalf("dangling the pointer: %v", err)
	}
	_ = dangling
	result, err := svc.DetectNow()
	if err != nil {
		t.Fatalf("DetectNow() error = %v", err)
	}
	if result.Pointer != "adopted-stale" {
		t.Fatalf("Pointer = %q, want adopted-stale", result.Pointer)
	}
	if result.Adopted == nil || !result.Adopted.IsPrimary {
		t.Fatalf("result = %+v, want the find adopted onto the stale pointer", result)
	}
}

// Many finds never auto-pick: the pointer stays and the summary reports
// needs-choice with per-row choosers downstream.
func TestDetectNowManyFindsNeedChoice(t *testing.T) {
	svc, _ := openInstallStore(t)
	stubLogCandidates(t)
	base := t.TempDir()
	first := filepath.Join(base, "first")
	second := filepath.Join(base, "second")
	seedDetectDir(t, first, false)
	seedDetectDir(t, second, false)
	stubDetectCandidates(t, []discover.Candidate{
		{Path: first, Source: discover.SourceSteam},
		{Path: second, Source: discover.SourceGOG},
	})
	result, err := svc.DetectNow()
	if err != nil {
		t.Fatalf("DetectNow() error = %v", err)
	}
	if !result.NeedsChoice || result.Adopted != nil {
		t.Fatalf("result = %+v, want needs-choice with no adoption", result)
	}
	if result.Pointer != "needs-choice" {
		t.Fatalf("Pointer = %q, want needs-choice", result.Pointer)
	}
	if len(result.Found) != 2 {
		t.Fatalf("Found = %+v, want both installs recorded", result.Found)
	}
	got, err := svc.GetSetting(settings.PrimaryGameInstallIDKey)
	if err != nil {
		t.Fatalf("GetSetting(pointer) error = %v", err)
	}
	if id, _ := got.(int); id != 0 {
		t.Fatalf("pointer = %#v, want unset until the per-row chooser decides", got)
	}
	if err := svc.SetPrimaryGameInstall(result.Found[1].ID); err != nil {
		t.Fatalf("SetPrimaryGameInstall() error = %v", err)
	}
	list, err := svc.GameInstalls()
	if err != nil {
		t.Fatalf("GameInstalls() error = %v", err)
	}
	for _, view := range list {
		if view.ID == result.Found[1].ID && !view.IsPrimary {
			t.Fatal("explicit choice did not take")
		}
	}
}

// Manual adds never steal either: a manual row after auto finds keeps the
// healthy auto pointer in place.
func TestDetectNowManualAddNeverStealsAutoPrimary(t *testing.T) {
	svc, _ := openInstallStore(t)
	stubLogCandidates(t)
	base := t.TempDir()
	auto := filepath.Join(base, "auto")
	seedDetectDir(t, auto, false)
	stubDetectCandidates(t, []discover.Candidate{{Path: auto, Source: discover.SourceSteam}})
	result, err := svc.DetectNow()
	if err != nil {
		t.Fatalf("DetectNow() error = %v", err)
	}
	manual := filepath.Join(base, "manual")
	seedDetectDir(t, manual, false)
	added, err := svc.AddGameInstall(manual)
	if err != nil {
		t.Fatalf("AddGameInstall() error = %v", err)
	}
	if added.IsPrimary {
		t.Fatal("manual add stole the healthy auto pointer")
	}
	if result.Adopted == nil {
		t.Fatal("first pass adopted nothing")
	}
	list, err := svc.GameInstalls()
	if err != nil {
		t.Fatalf("GameInstalls() error = %v", err)
	}
	for _, view := range list {
		if view.ID == result.Adopted.ID && !view.IsPrimary {
			t.Fatal("auto primary moved after a manual add")
		}
	}
}

// Upsert semantics: a repeated pass refreshes instead of duplicating, and
// auto sources are never recorded as manual.
func TestDetectNowRepeatedPassRefreshes(t *testing.T) {
	svc, _ := openInstallStore(t)
	stubLogCandidates(t)
	game := filepath.Join(t.TempDir(), "game")
	seedDetectDir(t, game, false)
	stubDetectCandidates(t, []discover.Candidate{{Path: game, Source: discover.SourceGOG}})
	first, err := svc.DetectNow()
	if err != nil {
		t.Fatalf("DetectNow() error = %v", err)
	}
	second, err := svc.DetectNow()
	if err != nil {
		t.Fatalf("DetectNow() error = %v", err)
	}
	if len(first.Found) != 1 || len(second.Found) != 1 || first.Found[0].ID != second.Found[0].ID {
		t.Fatalf("passes = %+v / %+v, want the same single row refreshed", first.Found, second.Found)
	}
	if second.Found[0].Source == "manual" {
		t.Fatalf("Found = %+v, want the auto source preserved, never manual", second.Found)
	}
	list, err := svc.GameInstalls()
	if err != nil {
		t.Fatalf("GameInstalls() error = %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("rows = %d, want 1 after a repeated pass", len(list))
	}
}

// macOS bundle inner dir: the Contents/MacOS game dir detects as a valid
// install when the fixture carries the game DLL.
func TestDetectNowMacOSBundleInnerDir(t *testing.T) {
	svc, _ := openInstallStore(t)
	stubLogCandidates(t)
	bundle := filepath.Join(t.TempDir(), "Stardew Valley.app", "Contents", "MacOS")
	seedDetectDir(t, bundle, false)
	stubDetectCandidates(t, []discover.Candidate{{Path: bundle, Source: discover.SourceGOG}})
	result, err := svc.DetectNow()
	if err != nil {
		t.Fatalf("DetectNow() error = %v", err)
	}
	if len(result.Found) != 1 {
		t.Fatalf("Found = %+v, want the bundle inner dir recorded", result.Found)
	}
}

// Nil-store honesty and wire shape: DetectNow refuses without a database,
// and the result marshals to the keys the frontend binds to.
func TestDetectNowRefusesNilStore(t *testing.T) {
	bare := New("Astradew", "1.4.2")
	if _, err := bare.DetectNow(); installCode(t, err) != apperror.CodeSettingUnavailable {
		t.Fatalf("DetectNow() nil-store code = %v, want SETTING_UNAVAILABLE", err)
	}
}

func TestDetectNowResultWireShape(t *testing.T) {
	svc, _ := openInstallStore(t)
	stubLogCandidates(t)
	game := filepath.Join(t.TempDir(), "game")
	seedDetectDir(t, game, false)
	stubDetectCandidates(t, []discover.Candidate{{Path: game, Source: discover.SourceSteam}})
	result, err := svc.DetectNow()
	if err != nil {
		t.Fatalf("DetectNow() error = %v", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshalling DetectNowResult: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decoding DetectNowResult JSON %s: %v", encoded, err)
	}
	for _, key := range []string{"found", "adopted", "pointerOutcome", "needsChoice", "message", "installsKnown"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("DetectNowResult JSON %s missing key %q", encoded, key)
		}
	}
	if fmt.Sprint(decoded["pointerOutcome"]) != "adopted-vacuum" {
		t.Fatalf("pointerOutcome = %v, want adopted-vacuum", decoded["pointerOutcome"])
	}
}
