package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zendevve/astradew/internal/settings"
)

// plantDanglingPointer writes a primary pointer that names no row.
// settings.Service.Set validates the kind only, so 999 stores fine.
func plantDanglingPointer(t *testing.T, svc *ApplicationService, id int) {
	t.Helper()
	raw, err := json.Marshal(id)
	if err != nil {
		t.Fatalf("marshalling pointer: %v", err)
	}
	if err := settings.New(svc.db.DB()).Set(context.Background(), settings.PrimaryGameInstallIDKey, raw); err != nil {
		t.Fatalf("planting dangling pointer %d: %v", id, err)
	}
}

func findingText(f HealthFinding) string {
	return f.What + "\n" + f.Why + "\n" + f.Action
}

// A stale pointer keeps both sections unobserved, keeps every unavailable
// entry, and earns one error finding naming the missing id with a
// Detect-or-repick action — never a replacement guess.
func TestHealthStalePointerFinding(t *testing.T) {
	stubLogDirs(t)
	svc, _ := openInstallStore(t)
	game := filepath.Join(t.TempDir(), "game")
	seedGameDir(t, game, false)
	if _, err := svc.AddGameInstall(game); err != nil {
		t.Fatalf("AddGameInstall() error = %v", err)
	}
	plantDanglingPointer(t, svc, 999)

	report := svc.Health()
	if report.Game != nil || report.Smapi != nil {
		t.Fatalf("Health game/smapi = %+v/%+v, want null for a stale pointer", report.Game, report.Smapi)
	}
	for _, want := range []string{"Game detection", "SMAPI detection", "Mod health"} {
		found := false
		for _, entry := range report.Unavailable {
			if entry.Name == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("unavailable entry %q missing: stale keeps every entry", want)
		}
	}
	if len(report.Findings) != 1 {
		t.Fatalf("findings = %+v, want exactly the one stale finding", report.Findings)
	}
	finding := report.Findings[0]
	if finding.Severity != "error" {
		t.Fatalf("finding severity = %q, want error", finding.Severity)
	}
	text := findingText(finding)
	if !strings.Contains(finding.What, "999") {
		t.Fatalf("finding what = %q, want it to name the missing install id", finding.What)
	}
	if !strings.Contains(strings.ToLower(text), "guess") {
		t.Fatalf("finding = %q, want it to refuse a replacement guess", text)
	}
	if !strings.Contains(finding.Action, "Detect") || !strings.Contains(finding.Action, "re-pick") {
		t.Fatalf("finding action = %q, want it to direct Detect or a re-pick", finding.Action)
	}
}

// A partial SMAPI earns one finding naming exactly the missing pieces
// against the game path, directing a reinstall there.
func TestHealthPartialSmapiFinding(t *testing.T) {
	stubLogDirs(t)
	svc, _ := openInstallStore(t)
	game := filepath.Join(t.TempDir(), "game")
	seedGameDir(t, game, false)
	if err := os.WriteFile(filepath.Join(game, "StardewModdingAPI.exe"), []byte("x"), 0o600); err != nil {
		t.Fatalf("seeding launcher: %v", err)
	}
	if _, err := svc.AddGameInstall(game); err != nil {
		t.Fatalf("AddGameInstall() error = %v", err)
	}

	report := svc.Health()
	if report.Smapi == nil || report.Smapi.State != "partial" {
		t.Fatalf("Health smapi = %+v, want the observed partial state", report.Smapi)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("findings = %+v, want exactly the one partial finding", report.Findings)
	}
	finding := report.Findings[0]
	if !strings.Contains(finding.What, game) {
		t.Fatalf("finding what = %q, want it against the game path %q", finding.What, game)
	}
	for _, piece := range report.Smapi.Missing {
		if !strings.Contains(finding.What, piece) {
			t.Fatalf("finding what = %q, want it to name missing piece %q", finding.What, piece)
		}
	}
	if !strings.Contains(strings.ToLower(finding.Action), "reinstall") || !strings.Contains(finding.Action, game) {
		t.Fatalf("finding action = %q, want a reinstall directed at the game path", finding.Action)
	}
}

// A version conflict earns one finding naming the disagreeing sources while
// trusting none.
func TestHealthVersionConflictFinding(t *testing.T) {
	stubLogDirs(t)
	svc, _ := openInstallStore(t)
	game := filepath.Join(t.TempDir(), "game")
	seedGameDir(t, game, false)
	seedManifest(t, game, "ConsoleCommands", "SMAPI.ConsoleCommands", "4.5.2", "")
	seedManifest(t, game, "SaveBackup", "SMAPI.SaveBackup", "4.5.1", "")
	if _, err := svc.AddGameInstall(game); err != nil {
		t.Fatalf("AddGameInstall() error = %v", err)
	}

	report := svc.Health()
	if report.Smapi == nil || report.Smapi.Version != nil || report.Smapi.State != "partial" {
		t.Fatalf("Health smapi = %+v, want observed partial with a null version: conflict is partial state plus detail naming the sources", report.Smapi)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("findings = %+v, want exactly the one conflict finding", report.Findings)
	}
	text := findingText(report.Findings[0])
	if !strings.Contains(text, "ConsoleCommands") || !strings.Contains(text, "SaveBackup") {
		t.Fatalf("finding = %q, want both disagreeing sources named", text)
	}
	if !strings.Contains(strings.ToLower(text), "trusts none") {
		t.Fatalf("finding = %q, want it to trust none of its sources", text)
	}
}

// A platform mismatch earns one finding naming both platform families and
// directing the matching SMAPI build.
func TestHealthPlatformMismatchFinding(t *testing.T) {
	stubLogDirs(t)
	svc, _ := openInstallStore(t)
	game := filepath.Join(t.TempDir(), "game")
	seedGameDir(t, game, true)
	for _, name := range []string{"StardewModdingAPI", "StardewValley-original"} {
		if err := os.WriteFile(filepath.Join(game, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("seeding %s: %v", name, err)
		}
	}
	if _, err := svc.AddGameInstall(game); err != nil {
		t.Fatalf("AddGameInstall() error = %v", err)
	}

	report := svc.Health()
	if report.Smapi == nil {
		t.Fatal("Health smapi null, want the observed mismatch")
	}
	if len(report.Findings) != 1 {
		t.Fatalf("findings = %+v, want exactly the one mismatch finding", report.Findings)
	}
	text := findingText(report.Findings[0])
	if !strings.Contains(text, "Windows") || !strings.Contains(text, "Linux/macOS") {
		t.Fatalf("finding = %q, want both platform families named", text)
	}
	if !strings.Contains(strings.ToLower(report.Findings[0].Action), "matching") {
		t.Fatalf("finding action = %q, want it to direct the matching SMAPI build", report.Findings[0].Action)
	}
}

// A vanilla SMAPI-absent install earns no finding: absent is observed, not a
// problem.
func TestHealthVanillaAbsentEarnsNoFinding(t *testing.T) {
	stubLogDirs(t)
	svc, _ := openInstallStore(t)
	game := filepath.Join(t.TempDir(), "game")
	seedGameDir(t, game, false)
	if _, err := svc.AddGameInstall(game); err != nil {
		t.Fatalf("AddGameInstall() error = %v", err)
	}

	report := svc.Health()
	if report.Game == nil || report.Smapi == nil {
		t.Fatalf("Health game/smapi = %+v/%+v, want observed", report.Game, report.Smapi)
	}
	if report.Smapi.State != "absent" {
		t.Fatalf("smapi state = %q, want absent", report.Smapi.State)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("findings = %+v, want none for a vanilla install", report.Findings)
	}
}
