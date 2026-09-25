package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain isolates the package from ambient machine state: a real
// SMAPI-latest.txt in the OS ErrorLogs dir must never leak last-run
// versions into AddGameInstall/Health probes. The default is no log
// candidates; individual tests stub smapiLogDirCandidates per-test.
func TestMain(m *testing.M) {
	smapiLogDirCandidates = func() []string { return nil }
	os.Exit(m.Run())
}

// stubLogDirs points the last-run log probe at dirs for one test,
// restoring the hermetic default afterwards.
func stubLogDirs(t *testing.T, dirs ...string) {
	t.Helper()
	old := smapiLogDirCandidates
	smapiLogDirCandidates = func() []string { return dirs }
	t.Cleanup(func() { smapiLogDirCandidates = old })
}

// seedLatestLog writes a parsable SMAPI-latest.txt header into dir.
func seedLatestLog(t *testing.T, dir, smapi, game string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seeding log dir: %v", err)
	}
	line := "[12:34:56 INFO  SMAPI] SMAPI " + smapi + " with Stardew Valley " + game + " on Microsoft Windows 11\n"
	if err := os.WriteFile(filepath.Join(dir, "SMAPI-latest.txt"), []byte(line), 0o600); err != nil {
		t.Fatalf("seeding log: %v", err)
	}
}

// seedManifest writes Mods/<dir>/manifest.json carrying the given UniqueID
// identity plus Version/MinimumApiVersion claims.
func seedManifest(t *testing.T, gameDir, dir, uniqueID, version, minApi string) {
	t.Helper()
	payload, err := json.Marshal(map[string]string{
		"Name":              dir,
		"Author":            "test",
		"Version":           version,
		"UniqueID":          uniqueID,
		"MinimumApiVersion": minApi,
	})
	if err != nil {
		t.Fatalf("marshalling manifest: %v", err)
	}
	full := filepath.Join(gameDir, "Mods", dir)
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatalf("seeding manifest dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(full, "manifest.json"), payload, 0o600); err != nil {
		t.Fatalf("seeding manifest: %v", err)
	}
}

func strVal(t *testing.T, p *string) string {
	t.Helper()
	if p == nil {
		t.Fatal("want non-nil version, got nil")
	}
	return *p
}

// A single UniqueID-gated bundled manifest resolves the SMAPI version and
// persists through the row into the view.
func TestAddGameInstallManifestVersionPersists(t *testing.T) {
	stubLogDirs(t)
	svc, _ := openInstallStore(t)
	game := filepath.Join(t.TempDir(), "game")
	seedGameDir(t, game, false)
	seedManifest(t, game, "ConsoleCommands", "SMAPI.ConsoleCommands", "4.5.2", "")

	view, err := svc.AddGameInstall(game)
	if err != nil {
		t.Fatalf("AddGameInstall() error = %v", err)
	}
	if got := strVal(t, view.SmapiVersion); got != "4.5.2" {
		t.Fatalf("SmapiVersion = %q, want 4.5.2 from the bundled manifest", got)
	}
	if view.GameVersion != nil {
		t.Fatalf("GameVersion = %q, want nil (no readable game version)", *view.GameVersion)
	}
	list, err := svc.GameInstalls()
	if err != nil {
		t.Fatalf("GameInstalls() error = %v", err)
	}
	if got := strVal(t, list[0].SmapiVersion); got != "4.5.2" {
		t.Fatalf("row SmapiVersion = %q, want persisted 4.5.2", got)
	}
}

// Disagreeing version sources trust none: conflict persists as null
// versions, both for mutual manifest disagreement and for a manifest
// Version against its own MinimumApiVersion.
func TestAddGameInstallConflictPersistsNull(t *testing.T) {
	stubLogDirs(t)
	svc, _ := openInstallStore(t)
	base := t.TempDir()

	disagree := filepath.Join(base, "disagree")
	seedGameDir(t, disagree, false)
	seedManifest(t, disagree, "ConsoleCommands", "SMAPI.ConsoleCommands", "4.5.2", "")
	seedManifest(t, disagree, "SaveBackup", "SMAPI.SaveBackup", "4.5.1", "")
	view, err := svc.AddGameInstall(disagree)
	if err != nil {
		t.Fatalf("AddGameInstall(disagree) error = %v", err)
	}
	if view.SmapiVersion != nil {
		t.Fatalf("SmapiVersion = %q, want nil: conflicting manifests trust none", *view.SmapiVersion)
	}

	minapi := filepath.Join(base, "minapi")
	seedGameDir(t, minapi, false)
	seedManifest(t, minapi, "ConsoleCommands", "SMAPI.ConsoleCommands", "4.5.2", "4.5.1")
	view, err = svc.AddGameInstall(minapi)
	if err != nil {
		t.Fatalf("AddGameInstall(minapi) error = %v", err)
	}
	if view.SmapiVersion != nil {
		t.Fatalf("SmapiVersion = %q, want nil: Version vs MinimumApiVersion trusts none", *view.SmapiVersion)
	}
}

// No readable source anywhere stays null: garbage assembly bytes, no
// manifests, no log.
func TestAddGameInstallUnknownStaysNull(t *testing.T) {
	stubLogDirs(t)
	svc, _ := openInstallStore(t)
	game := filepath.Join(t.TempDir(), "game")
	seedGameDir(t, game, false)

	view, err := svc.AddGameInstall(game)
	if err != nil {
		t.Fatalf("AddGameInstall() error = %v", err)
	}
	if view.GameVersion != nil || view.SmapiVersion != nil {
		t.Fatalf("view = %+v, want nil versions (unknown by contract)", view)
	}
}

// Re-picking a known path refreshes its versions instead of duplicating:
// unknown becomes known once a manifest appears.
func TestAddGameInstallRepicksRefreshVersions(t *testing.T) {
	stubLogDirs(t)
	svc, _ := openInstallStore(t)
	game := filepath.Join(t.TempDir(), "game")
	seedGameDir(t, game, false)

	first, err := svc.AddGameInstall(game)
	if err != nil {
		t.Fatalf("AddGameInstall() error = %v", err)
	}
	if first.SmapiVersion != nil {
		t.Fatalf("SmapiVersion = %q, want nil before any manifest", *first.SmapiVersion)
	}
	seedManifest(t, game, "SaveBackup", "SMAPI.SaveBackup", "4.5.2", "")
	second, err := svc.AddGameInstall(game)
	if err != nil {
		t.Fatalf("re-pick AddGameInstall() error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("re-pick id = %d, want existing %d", second.ID, first.ID)
	}
	if got := strVal(t, second.SmapiVersion); got != "4.5.2" {
		t.Fatalf("re-pick SmapiVersion = %q, want refreshed 4.5.2", got)
	}
}

// With no on-disk version, the last-run log header fills both versions;
// log-derived values are stored plainly (Health re-derives the flag).
func TestAddGameInstallLogFallback(t *testing.T) {
	logdir := filepath.Join(t.TempDir(), "logs")
	seedLatestLog(t, logdir, "4.5.2", "1.6.15")
	stubLogDirs(t, logdir)
	svc, _ := openInstallStore(t)
	game := filepath.Join(t.TempDir(), "game")
	seedGameDir(t, game, false)

	view, err := svc.AddGameInstall(game)
	if err != nil {
		t.Fatalf("AddGameInstall() error = %v", err)
	}
	if got := strVal(t, view.GameVersion); got != "1.6.15" {
		t.Fatalf("GameVersion = %q, want 1.6.15 from the last-run log", got)
	}
	if got := strVal(t, view.SmapiVersion); got != "4.5.2" {
		t.Fatalf("SmapiVersion = %q, want 4.5.2 from the last-run log", got)
	}
	health := svc.Health()
	if health.Smapi == nil || !strings.Contains(health.Smapi.Detail, "last SMAPI run") {
		t.Fatalf("Health Smapi detail = %+v, want the last-run-only caveat", health.Smapi)
	}
}

// No primary install means no observation: both sections are null and all
// unavailable entries stay.
func TestHealthNullSectionsWhenNoPrimary(t *testing.T) {
	stubLogDirs(t)
	svc, _ := openInstallStore(t)
	report := svc.Health()
	if report.Game != nil || report.Smapi != nil {
		t.Fatalf("Health game/smapi = %+v/%+v, want null with no primary", report.Game, report.Smapi)
	}
	for _, want := range []string{"Game detection", "SMAPI detection", "Mod health"} {
		found := false
		for _, entry := range report.Unavailable {
			if entry.Name == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("unavailable entry %q missing with no primary", want)
		}
	}
}

// An observed primary renders sections and drops exactly the Game/SMAPI
// unavailable entries; Mod health stays. Missing is empty, never null.
func TestHealthObservedSectionsDropUnavailable(t *testing.T) {
	stubLogDirs(t)
	svc, _ := openInstallStore(t)
	game := filepath.Join(t.TempDir(), "game")
	seedGameDir(t, game, false)
	if _, err := svc.AddGameInstall(game); err != nil {
		t.Fatalf("AddGameInstall() error = %v", err)
	}

	report := svc.Health()
	if report.Game == nil || report.Smapi == nil {
		t.Fatalf("Health game/smapi = %+v/%+v, want observed with a primary", report.Game, report.Smapi)
	}
	if report.Game.GameVersion != nil {
		t.Fatalf("gameVersion = %q, want null (unknown)", *report.Game.GameVersion)
	}
	if report.Game.InstallsKnown != 1 {
		t.Fatalf("installsKnown = %d, want 1", report.Game.InstallsKnown)
	}
	if report.Smapi.State != "absent" {
		t.Fatalf("smapi state = %q, want absent", report.Smapi.State)
	}
	if report.Smapi.Version != nil {
		t.Fatalf("smapi version = %q, want null (unknown)", *report.Smapi.Version)
	}
	if report.Smapi.Missing == nil || len(report.Smapi.Missing) != 0 {
		t.Fatalf("missing = %+v, want empty never null", report.Smapi.Missing)
	}
	if report.Smapi.Detail == "" {
		t.Fatal("detail empty, want one honest sentence")
	}
	for _, entry := range report.Unavailable {
		if entry.Name == "Game detection" || entry.Name == "SMAPI detection" {
			t.Fatalf("unavailable entry %q present with an observed primary", entry.Name)
		}
	}
	foundMod := false
	for _, entry := range report.Unavailable {
		if entry.Name == "Mod health" {
			foundMod = true
		}
	}
	if !foundMod {
		t.Fatal("Mod health entry missing: it stays for all of Phase 1")
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshalling Health: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decoding Health JSON: %v", err)
	}
	for _, key := range []string{"game", "smapi"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("Health JSON missing key %q", key)
		}
	}
}

// A conflicted install stores nothing and Health names the disagreeing
// sources; even a parsable log never clears a conflict.
func TestHealthConflictDetailNamesSources(t *testing.T) {
	logdir := filepath.Join(t.TempDir(), "logs")
	seedLatestLog(t, logdir, "4.5.1", "1.6.15")
	stubLogDirs(t, logdir)
	svc, _ := openInstallStore(t)
	game := filepath.Join(t.TempDir(), "game")
	seedGameDir(t, game, false)
	seedManifest(t, game, "ConsoleCommands", "SMAPI.ConsoleCommands", "4.5.2", "")
	seedManifest(t, game, "SaveBackup", "SMAPI.SaveBackup", "4.5.1", "")
	if _, err := svc.AddGameInstall(game); err != nil {
		t.Fatalf("AddGameInstall() error = %v", err)
	}

	report := svc.Health()
	if report.Smapi == nil {
		t.Fatal("Health smapi null, want the observed conflict")
	}
	if report.Smapi.Version != nil {
		t.Fatalf("smapi version = %q, want null: conflict trusts none", *report.Smapi.Version)
	}
	if !strings.Contains(report.Smapi.Detail, "ConsoleCommands") || !strings.Contains(report.Smapi.Detail, "SaveBackup") {
		t.Fatalf("detail = %q, want both disagreeing manifest sources named", report.Smapi.Detail)
	}
}
