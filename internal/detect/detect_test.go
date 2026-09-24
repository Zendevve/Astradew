package detect

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Zendevve/astradew/internal/apperror"
)

// gameFS composes an in-memory game directory: flags toggle every §4 state
// in a few lines, with zero disk. Base holds the game DLL unless omitted.
func gameFS(opts ...string) fstest.MapFS {
	enabled := map[string]bool{}
	for _, opt := range opts {
		enabled[opt] = true
	}
	m := fstest.MapFS{}
	if !enabled["no-dll"] {
		m["Stardew Valley.dll"] = &fstest.MapFile{Data: []byte("dll")}
	}
	put := func(name string) {
		m[name] = &fstest.MapFile{Data: []byte("x")}
	}
	if enabled["windows-launcher"] || enabled["smapi-full-windows"] {
		put("StardewModdingAPI.exe")
	}
	if enabled["unix-launcher"] || enabled["smapi-full-unix"] {
		put("StardewModdingAPI")
	}
	if enabled["unix-swap"] || enabled["smapi-full-unix"] {
		put("StardewValley-original")
	}
	if enabled["smapi-full-windows"] {
		for _, name := range []string{"StardewModdingAPI.dll", "StardewModdingAPI.deps.json", "StardewModdingAPI.runtimeconfig.json", "StardewModdingAPI.exe.config", "StardewModdingAPI.xml", "smapi-internal/config.json", "steam_appid.txt"} {
			put(name)
		}
	}
	if enabled["smapi-full-unix"] {
		for _, name := range []string{"StardewModdingAPI.dll", "StardewModdingAPI.deps.json", "StardewModdingAPI.runtimeconfig.json", "StardewModdingAPI.xml", "smapi-internal/config.json", "steam_appid.txt"} {
			put(name)
		}
	}
	if enabled["partial-launcher-only"] {
		put("StardewModdingAPI.exe")
	}
	if enabled["legacy-exe"] {
		put("Stardew Valley.exe")
		put("MonoGame.Framework.dll")
	}
	if enabled["compat-exe"] {
		put("Stardew Valley.exe")
	}
	if enabled["unix-game"] {
		put("StardewValley")
		put("MonoGame.Framework.dll")
	}
	if enabled["installer"] {
		put("install on Windows.bat")
		put("internal/install.dat")
	}
	if enabled["mods"] || enabled["smapi-full-windows"] || enabled["smapi-full-unix"] {
		m["Mods/.keep"] = &fstest.MapFile{Data: []byte{}}
	}
	if enabled["bundled-mods"] {
		m["Mods/ConsoleCommands/manifest.json"] = &fstest.MapFile{Data: []byte(`{"UniqueID":"SMAPI.ConsoleCommands","Name":"Console Commands"}`)}
		m["Mods/SaveBackup/manifest.json"] = &fstest.MapFile{Data: []byte(`{"UniqueID":"SMAPI.SaveBackup","Name":"Save Backup"}`)}
		m["Mods/.keep"] = &fstest.MapFile{Data: []byte{}}
	}
	if enabled["renamed-bundled"] {
		m["Mods/Renamed/manifest.json"] = &fstest.MapFile{Data: []byte(`{"UniqueID":"SMAPI.ConsoleCommands","Name":"Console Commands"}`)}
		m["Mods/.keep"] = &fstest.MapFile{Data: []byte{}}
	}
	if enabled["forged-mod"] {
		m["Mods/ConsoleCommands/manifest.json"] = &fstest.MapFile{Data: []byte(`{"UniqueID":"ThirdParty.Fake","Name":"Fake"}`)}
		m["Mods/.keep"] = &fstest.MapFile{Data: []byte{}}
	}
	return m
}

// codeOf extracts the typed code, failing when the error carries none.
func codeOf(t *testing.T, err error) apperror.Code {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error %#v carries no typed code", err)
	}
	return appErr.Code
}

func TestDetectNoGameFound(t *testing.T) {
	_, err := Detect(gameFS("no-dll"))
	if codeOf(t, err) != apperror.CodeGameNotFound {
		t.Fatalf("code = %v, want GAME_NOT_FOUND", err)
	}
	if err.Error() == "" {
		t.Fatal("empty error text")
	}
}

func TestDetectInstallerBundleHint(t *testing.T) {
	report, err := Detect(gameFS("no-dll", "installer"))
	if codeOf(t, err) != apperror.CodeGameNotFound {
		t.Fatalf("code = %v, want GAME_NOT_FOUND", err)
	}
	if !report.InstallerBundle {
		t.Fatal("InstallerBundle = false, want true for the installer bundle")
	}
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error %#v carries no details", err)
	}
	if got := appErr.Details; !contains(got, "run it instead") {
		t.Fatalf("details = %q, want the run-the-installer hint", got)
	}
	var bundleErr *apperror.AppError
	_ = errors.As(err, &bundleErr)
	if got := bundleErr.Details; !strings.HasPrefix(got, InstallerBundleMarker) {
		t.Fatalf("details = %q, want machine-stable prefix %q", got, InstallerBundleMarker)
	}
}

func TestDetectLegacyGame(t *testing.T) {
	_, err := Detect(gameFS("no-dll", "legacy-exe"))
	if codeOf(t, err) != apperror.CodeGameLegacy {
		t.Fatalf("code = %v, want GAME_LEGACY", err)
	}
}

func TestDetectCompatBranch(t *testing.T) {
	_, err := Detect(gameFS("no-dll", "compat-exe"))
	if codeOf(t, err) != apperror.CodeGameLegacy {
		t.Fatalf("code = %v, want GAME_LEGACY", err)
	}
	var appErr *apperror.AppError
	_ = errors.As(err, &appErr)
	if got := appErr.Details; !contains(got, "compatibility branch") {
		t.Fatalf("details = %q, want the branch named", got)
	}
}

func TestDetectValidVanilla(t *testing.T) {
	report, err := Detect(gameFS())
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if !report.Valid || report.Smapi != SmapiAbsent {
		t.Fatalf("report = %+v, want valid vanilla absent", report)
	}
	if report.GameVersion != "" {
		t.Fatalf("GameVersion = %q, want empty (unknown by contract)", report.GameVersion)
	}
	if report.Missing == nil {
		t.Fatal("Missing is nil, want empty slice (never null on the wire)")
	}
}

func TestDetectSmapiCompleteWindows(t *testing.T) {
	report, err := Detect(gameFS("smapi-full-windows"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if report.Smapi != SmapiComplete {
		t.Fatalf("Smapi = %q, want complete", report.Smapi)
	}
	if report.SmapiExePath != "StardewModdingAPI.exe" {
		t.Fatalf("SmapiExePath = %q, want the .exe entry point", report.SmapiExePath)
	}
	if len(report.Missing) != 0 {
		t.Fatalf("Missing = %v, want empty", report.Missing)
	}
}

func TestDetectSmapiCompleteUnix(t *testing.T) {
	report, err := Detect(gameFS("smapi-full-unix"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if report.Smapi != SmapiComplete {
		t.Fatalf("Smapi = %q, want complete", report.Smapi)
	}
	if report.SmapiExePath != "StardewModdingAPI" {
		t.Fatalf("SmapiExePath = %q, want the extensionless entry point", report.SmapiExePath)
	}
}

func TestDetectSmapiPartialListsMissing(t *testing.T) {
	report, err := Detect(gameFS("partial-launcher-only"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if report.Smapi != SmapiPartial {
		t.Fatalf("Smapi = %q, want partial", report.Smapi)
	}
	if len(report.Missing) == 0 {
		t.Fatal("Missing empty, want the absent §1 signals listed")
	}
	found := false
	for _, name := range report.Missing {
		if name == "smapi-internal" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Missing = %v, want smapi-internal named", report.Missing)
	}
}

func TestDetectPlatformMismatch(t *testing.T) {
	m := gameFS("smapi-full-windows")
	m["StardewModdingAPI"] = &fstest.MapFile{Data: []byte("x")}
	m["StardewValley-original"] = &fstest.MapFile{Data: []byte("x")}
	report, err := Detect(m)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if !report.PlatformMismatch {
		t.Fatalf("PlatformMismatch = false, want true: %+v", report)
	}
}

func TestDetectModsPresenceOnly(t *testing.T) {
	report, err := Detect(gameFS("mods"))
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if !report.ModsPresent || report.ModsPath != "Mods" {
		t.Fatalf("mods = %+v, want present with relative path", report)
	}
	plain, err := Detect(gameFS())
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if plain.ModsPresent || plain.ModsPath != "" {
		t.Fatalf("mods = %+v, want absent without path", plain)
	}
}

func TestDetectBundledModsResolveByUniqueID(t *testing.T) {
	if !bundledModPresent(gameFS("bundled-mods"), "ConsoleCommands") {
		t.Fatal("bundled ConsoleCommands by UniqueID not recognised")
	}
	if !bundledModPresent(gameFS("bundled-mods"), "SaveBackup") {
		t.Fatal("bundled SaveBackup by UniqueID not recognised")
	}
	if !bundledModPresent(gameFS("renamed-bundled"), "Renamed") {
		t.Fatal("renamed folder with bundled UniqueID not recognised: identity is manifest, not name")
	}
	if bundledModPresent(gameFS("forged-mod"), "ConsoleCommands") {
		t.Fatal("forged manifest with third-party UniqueID recognised: folder name must never forge a System Mod")
	}
}

func TestDetectNilFSRefusesInvalid(t *testing.T) {
	if _, err := Detect(nil); codeOf(t, err) != apperror.CodeGameInvalid {
		t.Fatalf("code = %v, want GAME_INVALID", err)
	}
}

// The single os.DirFS round-trip: fidelity anchor proving the in-memory
// suite matches the real filesystem once rather than on every case.
func TestDetectOsDirFSRoundTrip(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"Stardew Valley.dll", "StardewModdingAPI.exe", "StardewModdingAPI.dll", "StardewModdingAPI.deps.json", "StardewModdingAPI.runtimeconfig.json", "StardewModdingAPI.exe.config", "StardewModdingAPI.xml", "steam_appid.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("seeding %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "smapi-internal"), 0o755); err != nil {
		t.Fatalf("seeding smapi-internal: %v", err)
	}
	report, err := Detect(os.DirFS(dir))
	if err != nil {
		t.Fatalf("Detect(os.DirFS) error = %v", err)
	}
	if !report.Valid || report.Smapi != SmapiComplete {
		t.Fatalf("report = %+v, want valid complete over the real filesystem", report)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
