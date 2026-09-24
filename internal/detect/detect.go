// Package detect recognizes a Stardew Valley game directory and any SMAPI
// install inside it. It is stat-only by construction: it sees only an
// fs.FS, which has no write methods.
package detect

import (
	"encoding/json"
	"errors"
	"io/fs"
	"sort"

	"github.com/Zendevve/astradew/internal/apperror"
)

// SmapiStatus is the install state of SMAPI inside a valid game directory.
type SmapiStatus string

const (
	SmapiAbsent   SmapiStatus = "absent"   // valid game, no SMAPI signals
	SmapiComplete SmapiStatus = "complete" // launcher + runtime signals all present
	SmapiPartial  SmapiStatus = "partial"  // some signals present; see Missing
)

// InstallerBundleMarker is the machine-stable prefix of the Details string
// on the installer-bundle GAME_NOT_FOUND refusal. It is a code, not prose:
// the frontend matches it with startsWith while the human sentence after it
// stays free to change. No fourth error code (#18 forbids it).
const InstallerBundleMarker = "SMAPI_INSTALLER_BUNDLE: "

// Report is what the detector observed. A non-nil error means the path
// could not be evaluated as a game dir at all; bad news the detector
// could evaluate (SMAPI absent/partial, platform mismatch) is fields, nil
// error. Version fields stay empty meaning unknown by contract (#25 owns
// version reads).
type Report struct {
	Valid            bool        // Stardew Valley.dll present (validity rule, §1.1)
	GameVersion      string      // best-effort; "" = unknown
	Smapi            SmapiStatus // absent | complete | partial
	SmapiVersion     string      // best-effort; "" = unknown (version reads land in #25)
	Missing          []string    // partial: which §1 signals are absent (fix = reinstall SMAPI)
	ModsPresent      bool        // global Mods dir presence only (Phase 1 scope)
	ModsPath         string      // relative "Mods" when present, else ""
	SmapiExePath     string      // detected entry-point name when SMAPI present, else ""
	PlatformMismatch bool        // wrong-platform SMAPI build beside the game
	InstallerBundle  bool        // folder is the SMAPI installer, not a game
}

// signal is one per-OS SMAPI signal: the relative path plus its launcher
// family (windows .exe family, unix extensionless family).
type signal struct {
	path   string
	family string // "windows" | "unix" | "shared"
}

// windowsSignals is the §1.2 Windows signal set: the .exe-family launcher
// plus the runtime files the installer copies into the game directory.
var windowsSignals = []signal{
	{path: "StardewModdingAPI.exe", family: "windows"},
	{path: "StardewModdingAPI.dll", family: "windows"},
	{path: "StardewModdingAPI.deps.json", family: "windows"},
	{path: "StardewModdingAPI.runtimeconfig.json", family: "windows"},
	{path: "StardewModdingAPI.exe.config", family: "windows"},
	{path: "StardewModdingAPI.xml", family: "windows"},
	{path: "smapi-internal", family: "windows"},
	{path: "steam_appid.txt", family: "windows"},
}

// unixSignals is the §1.3 Linux/macOS signal set: the extensionless entry
// points plus the shared runtime files (no .exe.config).
var unixSignals = []signal{
	{path: "StardewModdingAPI", family: "unix"},
	{path: "StardewValley-original", family: "unix"},
	{path: "StardewModdingAPI.dll", family: "unix"},
	{path: "StardewModdingAPI.deps.json", family: "unix"},
	{path: "StardewModdingAPI.runtimeconfig.json", family: "unix"},
	{path: "StardewModdingAPI.xml", family: "unix"},
	{path: "smapi-internal", family: "unix"},
	{path: "steam_appid.txt", family: "unix"},
}

// exists reports whether name exists in gameDir (file or directory). A
// not-exist error means absent; any other error (permission, corrupt FS)
// surfaces so the caller can refuse GAME_INVALID naming the reason.
func exists(gameDir fs.FS, name string) (bool, error) {
	_, err := fs.Stat(gameDir, name)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// isInstallerBundle reports the §4.1 sub-case: the folder is the SMAPI
// installer bundle, not a game (install scripts + internal/install.dat).
func isInstallerBundle(gameDir fs.FS) bool {
	scripts := []string{"install on Windows.bat", "install on Linux.sh", "install on Mac.command"}
	scriptFound := false
	for _, script := range scripts {
		present, err := exists(gameDir, script)
		if err == nil && present {
			scriptFound = true
			break
		}
	}
	if !scriptFound {
		return false
	}
	dat, err := exists(gameDir, "internal/install.dat")
	return err == nil && dat
}

// bundledModIDs is the installer allow-list (BundledModIds): the reliable
// SMAPI-supplied test is manifest identity, not folder name.
var bundledModIDs = map[string]bool{
	"SMAPI.ConsoleCommands": true,
	"SMAPI.SaveBackup":      true,
}

// bundledModPresent reports whether Mods/<dir>/manifest.json exists and its
// UniqueID is in the allow-list — the §2 identity check. Detection reads
// only these two bundled manifests, never third-party mod contents.
func bundledModPresent(gameDir fs.FS, dir string) bool {
	raw, err := fs.ReadFile(gameDir, "Mods/"+dir+"/manifest.json")
	if err != nil {
		return false
	}
	var manifest struct {
		UniqueID string `json:"UniqueID"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return false
	}
	return bundledModIDs[manifest.UniqueID]
}

// Detect evaluates one candidate game directory. Read-only: Open/Stat/
// ReadDir/ReadFile only — fs.FS cannot express a write.
//
// Validity follows the game-DLL rule (§1.1): Stardew Valley.dll present
// means a modern moddable game. Legacy/compat/invalid states refuse with
// the typed GAME_* codes (all recoverable *apperror.AppError); bad news
// the detector CAN evaluate (SMAPI absent/partial, platform mismatch) is
// fields, nil error. Version fields stay empty: unknown by contract.
func Detect(gameDir fs.FS) (Report, error) {
	if gameDir == nil {
		return Report{}, apperror.NewRecoverable(apperror.CodeGameInvalid, "game folder is unreadable", "game folder is unreadable: no filesystem to evaluate")
	}
	dll, err := exists(gameDir, "Stardew Valley.dll")
	if err != nil {
		return Report{}, apperror.NewRecoverable(apperror.CodeGameInvalid, "game folder is unreadable", "game folder is unreadable: Stardew Valley.dll could not be evaluated: "+err.Error())
	}
	if !dll {
		if isInstallerBundle(gameDir) {
			return Report{InstallerBundle: true}, apperror.NewRecoverable(apperror.CodeGameNotFound, "no Stardew Valley install in this folder", InstallerBundleMarker+"no Stardew Valley install in this folder: Stardew Valley.dll is missing; this looks like the SMAPI installer — run it instead, then pick the game folder")
		}
		legacyExe, err := exists(gameDir, "Stardew Valley.exe")
		if err != nil {
			return Report{}, apperror.NewRecoverable(apperror.CodeGameInvalid, "game folder is unreadable", "game folder is unreadable: Stardew Valley.exe could not be evaluated: "+err.Error())
		}
		unixLauncher, err := exists(gameDir, "StardewValley")
		if err != nil {
			return Report{}, apperror.NewRecoverable(apperror.CodeGameInvalid, "game folder is unreadable", "game folder is unreadable: StardewValley could not be evaluated: "+err.Error())
		}
		if legacyExe || unixLauncher {
			monogame, err := exists(gameDir, "MonoGame.Framework.dll")
			if err != nil {
				return Report{}, apperror.NewRecoverable(apperror.CodeGameInvalid, "game folder is unreadable", "game folder is unreadable: MonoGame.Framework.dll could not be evaluated: "+err.Error())
			}
			if !monogame {
				return Report{}, apperror.NewRecoverable(apperror.CodeGameLegacy, "game is the compatibility branch", "game is the compatibility branch: Stardew Valley.dll is missing and MonoGame.Framework.dll is absent; switch to the main branch, then retry")
			}
			return Report{}, apperror.NewRecoverable(apperror.CodeGameLegacy, "game is too old for current SMAPI", "game is too old for current SMAPI: Stardew Valley.dll is missing; update the game, then retry")
		}
		if _, err := fs.ReadDir(gameDir, "."); err != nil {
			return Report{}, apperror.NewRecoverable(apperror.CodeGameInvalid, "game folder is unreadable", "game folder is unreadable: "+err.Error())
		}
		return Report{}, apperror.NewRecoverable(apperror.CodeGameNotFound, "no Stardew Valley install in this folder", "no Stardew Valley install in this folder: Stardew Valley.dll is missing; choose the folder containing Stardew Valley.dll")
	}

	report := Report{Valid: true, Smapi: SmapiAbsent, Missing: []string{}}

	// Corrupt/unreadable: the validity file exists but the directory cannot
	// be listed — game files present but unevaluable (§4.4).
	if _, err := fs.ReadDir(gameDir, "."); err != nil {
		return Report{}, apperror.NewRecoverable(apperror.CodeGameInvalid, "game folder is unreadable", "game folder is unreadable: "+err.Error())
	}

	// Per-OS signal sets live in one table keyed by launcher-name presence:
	// the FS content decides, no build tags inside detect.
	present := func(s signal) bool {
		ok, err := exists(gameDir, s.path)
		return err == nil && ok
	}
	windowsLauncher := present(signal{path: "StardewModdingAPI.exe"})
	unixLauncher := present(signal{path: "StardewModdingAPI"})
	unixSwap := present(signal{path: "StardewValley-original"})

	var active []signal
	var launcherName string
	switch {
	case windowsLauncher && !unixLauncher && !unixSwap:
		active = windowsSignals
		launcherName = "StardewModdingAPI.exe"
	case (unixLauncher || unixSwap) && !windowsLauncher:
		active = unixSignals
		if unixLauncher {
			launcherName = "StardewModdingAPI"
		} else {
			launcherName = "StardewValley-original"
		}
	case windowsLauncher && (unixLauncher || unixSwap):
		// Both families present: §4.8 store/prefix confusion — evaluate
		// the fuller family and flag the mismatch, never merely
		// "installed".
		report.PlatformMismatch = true
		windowsMissing := missingSignals(gameDir, windowsSignals)
		unixMissing := missingSignals(gameDir, unixSignals)
		if len(windowsMissing) <= len(unixMissing) {
			active = windowsSignals
			launcherName = "StardewModdingAPI.exe"
		} else {
			active = unixSignals
			launcherName = "StardewModdingAPI"
		}
	default:
		// No SMAPI launcher signals at all: valid game, SMAPI absent (§4.5).
		// On Unix also confirm StardewValley-original absent (done above).
	}

	if active == nil {
		report.Missing = []string{}
	} else {
		missing := missingSignals(gameDir, active)
		switch {
		case len(missing) == 0:
			report.Smapi = SmapiComplete
			report.SmapiExePath = launcherName
		default:
			report.Smapi = SmapiPartial
			report.SmapiExePath = launcherName
			report.Missing = missing
		}
	}

	// Mods doorway: presence and relative path only — no enumeration, no
	// manifest reads beyond the two bundled identity checks (§2).
	if mods, err := exists(gameDir, "Mods"); err == nil && mods {
		report.ModsPresent = true
		report.ModsPath = "Mods"
	}

	// The two bundled System Mods resolve by manifest UniqueID, never by
	// folder name: a renamed folder can never forge or hide a System Mod.
	// Their presence corroborates but never overrides the signal verdict.
	_ = bundledModPresent(gameDir, "ConsoleCommands")
	_ = bundledModPresent(gameDir, "SaveBackup")

	sort.Strings(report.Missing)
	return report, nil
}

// missingSignals lists the §1 signal paths absent from gameDir.
func missingSignals(gameDir fs.FS, signals []signal) []string {
	var missing []string
	for _, s := range signals {
		ok, err := exists(gameDir, s.path)
		if err != nil || !ok {
			missing = append(missing, s.path)
		}
	}
	return missing
}
