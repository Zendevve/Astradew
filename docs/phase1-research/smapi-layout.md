# SMAPI in a game directory — recognition & version sources (Phase 1 detector research)

Research-only notes for the SMAPI detector. Scope: how to recognise that SMAPI is
installed inside a Stardew Valley game directory, per-OS file layout, where a version
string can be read, and which invalid-path states are worth distinguishing.

Current reference version at time of writing: SMAPI **4.5.2** (requires Stardew Valley
1.6.14+). All relative paths below are relative to the **game directory** (the folder
containing the game executable / `Stardew Valley.dll`).

## 1. Per-OS layout

### 1.1 How SMAPI knows a folder is a game folder at all

SMAPI's own toolkit (`GameScanner.GetGameFolderType`) treats a folder as a valid
modern game install iff it contains **`Stardew Valley.dll`** (constant
`Constants.GameDllName`; valid = "Stardew Valley 1.5.5+ install"). The installer
validates the chosen path the same way (it checks `paths.GameDllPath`). A detector
can reuse this rule: no `Stardew Valley.dll` ⇒ not a (modern, moddable) game dir —
see §4 for the legacy/invalid subtypes SMAPI itself distinguishes.

### 1.2 Windows (Steam / GOG)

Files the installer copies into the game directory (from `install.dat`, plus one
generated file). Full removal list is authoritative in
`InteractiveInstaller.GetUninstallPaths`:

- `StardewModdingAPI.exe` — the SMAPI launcher / modded entry point.
- `StardewModdingAPI.dll`
- `StardewModdingAPI.deps.json` — **not** shipped; the installer *copies the game's
  own* `Stardew Valley.deps.json` to this name (needed to resolve native DLLs).
- `StardewModdingAPI.runtimeconfig.json`
- `StardewModdingAPI.exe.config`
- `StardewModdingAPI.xml`
- `smapi-internal/` — internal runtime files (assemblies such as `0Harmony.dll`,
  `Mono.Cecil.dll`, `Newtonsoft.Json.dll`, `StardewModdingAPI.Toolkit*.dll`,
  `config.json`, `blacklist.json`, `metadata.json`, `i18n/`, …).
- `steam_appid.txt` — contains the Steam app ID (`413150`), **not** a version.
- `Mods/` — created if missing; bundled mods copied in (see §2).

Obsolete leftovers the installer also cleans (useful for recognising *old*
installs): `StardewModdingAPI-x64.exe` (pre-3.13), `StardewModdingAPI.pdb`
(pre-3.18.4), `Mods/TrainerMod` (renamed to ConsoleCommands in 2.0),
`Mods/ErrorHandler` (removed in 4.0), `Mods/.cache` (1.3–1.4), top-level
`0Harmony.dll` / `Mono.Cecil.dll` / `Newtonsoft.Json.dll` /
`StardewModdingAPI.Toolkit*.dll` / `StardewModdingAPI.config.json` /
`StardewModdingAPI.metadata.json` (all moved into `smapi-internal/` in 2.8).

Launch with vs without SMAPI:

- **Without:** run the vanilla `Stardew Valley.exe` directly.
- **With (standalone):** run `StardewModdingAPI.exe` directly in the game folder.
- **With (Steam, achievements/overlay):** Steam launch options set to
  `"C:\…\Stardew Valley\StardewModdingAPI.exe" %command%` (quotes + `%command%`
  both required). Steam Deck/Proton setups instead add `StardewModdingAPI` as a
  non-Steam game.
- **With (GOG Galaxy):** a `start.bat` wrapper launching `StardewModdingAPI.exe`,
  registered as a custom executable and set as default.
- **With (Xbox app / Game Pass):** special case — the player *renames* vanilla
  `Stardew Valley.exe` aside and places a **copy** of `StardewModdingAPI.exe`
  under the name `Stardew Valley.exe`. So on Xbox-app installs the vanilla
  binary name may actually BE SMAPI.

### 1.3 Linux / macOS (incl. Steam Deck native mode)

Same file set as Windows, except the `.exe`-family entry point is replaced:

- `StardewModdingAPI` (no extension) — the SMAPI executable.
- `StardewModdingAPI.deps.json`, `StardewModdingAPI.dll`,
  `StardewModdingAPI.runtimeconfig.json`, `StardewModdingAPI.xml`,
  `smapi-internal/`, `steam_appid.txt`, `Mods/` — as on Windows (no `.exe.config`).
- **Launcher swap:** the vanilla launcher script `StardewValley` (no extension)
  is renamed to `StardewValley-original` (backup), and SMAPI's `unix-launcher.sh`
  is moved into its place as the new `StardewValley`. Both are `chmod 755`'d.
  Uninstall deletes the SMAPI `StardewValley` and renames `StardewValley-original`
  back — so `StardewValley-original` existing is itself a strong SMAPI-installed
  signal, and a `StardewValley` with no `StardewValley-original` beside it is
  either vanilla or a broken install.
- On macOS everything above lives inside the bundle at
  `…/Stardew Valley.app/Contents/MacOS/` (i.e. the "game directory" *is* the
  `Contents/MacOS` folder).

Launch with vs without SMAPI:

- **No Steam launch-option changes are needed** (Windows-only requirement). The
  player launches the game exactly as before (Steam client / script); the
  swapped-in `StardewValley` script chains into SMAPI automatically.
- **Without (temporarily):** run `StardewValley-original` directly.
- **With:** run `./StardewValley` (now SMAPI) or `./StardewModdingAPI`.
- If Steam launch options are *set* on Linux/macOS they should be empty; a
  non-empty value is a misconfiguration hint, not a SMAPI signal.

### 1.4 Steam Deck / Proton

- **Native (Steam Linux Runtime):** Linux layout (§1.3).
- **Proton (Windows game build):** Windows layout (§1.2) installed *manually*
  per the installer's `README.txt` (copy `install.dat` contents, duplicate the
  game's `deps.json`, **no** launcher swap), launched as a separately added
  non-Steam game. Mismatch symptom worth surfacing: Linux SMAPI files alongside
  a Proton game prefix (`…/compatdata/413150/pfx/…`) — or vice versa — fails
  with errors like missing `libhostpolicy.so`.

## 2. Bundled content: Console Commands & Save Backup

- Location: `Mods/ConsoleCommands/` and `Mods/SaveBackup/` (folder names equal
  the mod assembly names — see `deploy-local-smapi.targets` `CopyDefaultMods`:
  each ships its `<Name>.dll`, `manifest.json`, and `i18n/`).
- The installer copies **only** mods whose `manifest.json → UniqueID` is in its
  allow-list `BundledModIds = ["SMAPI.SaveBackup", "SMAPI.ConsoleCommands"]`
  (`InteractiveInstaller.cs`); anything else found in the installer's `Mods/`
  staging folder is ignored with a warning. So the reliable "SMAPI-supplied, not
  third-party" test is the manifest identity, not the folder name:
  - `Mods/ConsoleCommands/manifest.json`: `UniqueID "SMAPI.ConsoleCommands"`,
    `Author "SMAPI"`, `Name "Console Commands"`.
  - `Mods/SaveBackup/manifest.json`: `UniqueID "SMAPI.SaveBackup"`,
    `Author "SMAPI"`, `Name "Save Backup"`.
  - Both manifests' `Version` / `MinimumApiVersion` equal the SMAPI release
    version (e.g. `4.5.2`) — making them a version source too (see §3).
- Corroborating signal: `smapi-internal/config.json → SuppressUpdateChecks`
  lists exactly `["SMAPI.ConsoleCommands", "SMAPI.SaveBackup"]`.
- History (for recognising upgraded installs): pre-2.0 the console mod shipped
  as `Mods/TrainerMod`; an `ErrorHandler` system mod shipped until 4.0 (now
  obsolete — listed in `metadata.json` as `SMAPI.ErrorHandler → Obsolete`).
- Do **not** confuse `Mods/SaveBackup/` (the mod's program files) with
  `save-backups/` in the game root (its output ZIPs) or with the wiki's loose
  shorthand "`Mods/SaveBackup` folder" for save retrieval.
- Custom `--mods-path` setups: SMAPI supports alternate mod folders
  (`--mods-path` / `SMAPI_MODS_PATH`); bundled mods live in whichever folder is
  active, so absence from `Mods/` alone does not prove absence if a custom path
  is configured.

## 3. Version sources

Canonical fact: the version is a **compile-time constant**. A release sets one
version string in three places together via
`build/scripts/set-smapi-version.ps1` (per `docs/technical/smapi.md` "Prepare a
release"): `build/common.targets` (`<Version>`), `src/SMAPI/Constants.cs`
(`EarlyConstants.RawApiVersion`), and each bundled mod's `manifest.json`.
Format is semver (`4.0.0`; dev builds `4.0.0-alpha.<date>`,
prereleases `4.0.0-beta.<date>`).

| # | Source | How to read | Reliability |
|---|--------|-------------|-------------|
| 1 | Assembly version of `StardewModdingAPI.dll` / `StardewModdingAPI.exe` (Win) / `StardewModdingAPI` (Unix) and `smapi-internal/*.dll` | PE/assembly metadata (`Assembly.GetName().Version`; on Windows e.g. `FileVersionInfo`) | **Highest (on-disk).** The build `<Version>` flows into assembly versions, and SMAPI itself enforces this at startup: `Program` aborts with "doesn't seem to be installed correctly" if a `smapi-internal/*.dll` assembly version ≠ `Constants.ApiVersion` (major.minor.patch). Caveat: the exact PE-field mapping was not inspected file-by-file `[UNVERIFIED]` — prefer comparing the assembly *product/semantic* version, and beware 4-part Windows version padding. |
| 2 | Bundled-mod manifests: `Mods/ConsoleCommands/manifest.json` and `Mods/SaveBackup/manifest.json` (`Version`, also `MinimumApiVersion`) | Parse JSON `Version` | **High.** Shipped equal to the SMAPI version by the release process (verified `4.5.2` in both source manifests). Two independent copies; if they disagree with each other or with (1), the install is mixed/corrupt — report the conflict rather than picking one. Missing if the user deleted the bundled mods (supported scenario). |
| 3 | SMAPI log header line | First `SMAPI …` INFO line, format `SMAPI {ApiVersion} with Stardew Valley {GameVersion} on {FriendlyOS}` (written by `LogManager.LogIntro`), e.g. `[12:34:56 INFO  SMAPI] SMAPI 4.5.2 with Stardew Valley 1.6.14 on Microsoft Windows 11`; also the console window title `SMAPI {ApiVersion} - running Stardew Valley {GameVersion}` | **High when present, but indirect.** Lives *outside* the game dir (`%AppData%\StardewValley\ErrorLogs\SMAPI-latest.txt` on Windows/Steam+GOG; `~/.config/StardewValley/ErrorLogs/` on Linux/macOS; Xbox-app path under `Packages\ConcernedApe.StardewValleyPC_…\LocalCache\…`; `SMAPI-crash.txt` duplicate after a crash). Only exists after ≥1 run; reflects the version that *ran*, which may post-date a later manual file swap. Parse `SMAPI (\S+) with Stardew Valley`. |
| 4 | GitHub release notes / changelog (`docs/release-notes.md`, `smapi.io` homepage) | N/A on disk | **Not an installed-version source.** Tells you the *latest available* version (for update checks), never what's in the folder. There is **no changelog file shipped inside the game directory** (no such entry in the installer file lists). |
| 5 | `smapi-internal/config.json` / `config.user.json` | — | **Not a version source.** Verified full default config: no version field of any kind (user file only holds overrides like `ConsoleColorScheme`). |
| 6 | `smapi-internal/metadata.json` | — | **Not a version source.** Verified: it is per-mod compatibility/update-check data (`ModData` keyed by mod name), carries no SMAPI version. |
| 7 | `steam_appid.txt` | — | **Not a version source.** Holds the Steam app ID `413150`. Its presence is a weak installed-signal at best (it is also trivially copyable). |
| 8 | Marker files: `smapi-internal/StardewModdingAPI.update.marker` (`{version}\|{url}`), `StardewModdingAPI.crash.marker` (empty) | — | **Must NOT be read as the installed version.** The update marker records the *newer version found* during a previous session's update check (and is deleted on next launch); the crash marker is empty. |
| 9 | `StardewModdingAPI.deps.json` | — | **Not a version source** `[UNVERIFIED — content not individually inspected, but by installer construction it is a byte-copy of the game's own `Stardew Valley.deps.json`, i.e. a .NET dependency manifest, not a SMAPI version record]`. |
| 10 | Installer window title / Steam launch-options text | — | Ephemeral / external to the game dir; not machine-readable version sources. |

**Absent case (installed but version unreadable):** possible when only weak
signals remain — e.g. `steam_appid.txt` + `smapi-internal/` skeleton or a lone
`Mods/` without manifests, or assembly files whose version metadata can't be
parsed. A detector MUST report `present, version unknown` (distinct from both
"not installed" and any guessed version), and SHOULD list which signals fired.
Never fall back to the newest GitHub release or the update-marker contents as
the installed version.

Recommended precedence when several sources exist: assembly version (1) →
bundled manifests (2) → log header (3, flagged "last-run version"); any
disagreement ⇒ report conflict explicitly.

## 4. Invalid-path states worth distinguishing

Modelled on SMAPI's own `GameFolderType` enum plus installer behaviour:

1. **Not a game directory at all** (`NoGameFound`): no `Stardew Valley.dll`,
   no `Stardew Valley.exe`/`StardewValley.exe`. Sub-case: *installer bundle
   mistaken for a game dir* — folder contains `install on Windows.bat` /
   `install on Linux.sh` / `install on Mac.command` + `internal/` with
   `install.dat`. Worth a dedicated hint ("run the installer, don't scan this
   folder"), since the wiki calls this mistake out explicitly.
2. **Legacy game, unmoddable by current SMAPI** (`LegacyVersion`): executable
   present with assembly version < 1.6 (i.e. Stardew 1.5.6 or earlier) and no
   `Stardew Valley.dll`. Detector should say "game too old for current SMAPI"
   rather than "no game".
3. **Compatibility-branch game** (`LegacyCompatibilityBranch`): modern exe but
   no `MonoGame.Framework.dll` (the 32-bit/XNA compatibility branch). Same
   handling as (2) with a branch-specific message.
4. **Corrupt/unreadable game** (`InvalidUnknown`): game files present but the
   executable fails to load as an assembly (e.g. modified/pirated builds —
   SMAPI's `Program` prints a dedicated "executable seems to be invalid" error
   for these).
5. **Valid game, SMAPI absent**: `Stardew Valley.dll` present, none of the §1
   SMAPI files present. (On Unix also confirm `StardewValley-original` absent.)
6. **Partial / broken SMAPI install**: some but not all signals — e.g.
   `StardewModdingAPI.exe` without `smapi-internal/`; `smapi-internal/` without
   launcher; launcher swapped (`StardewValley-original` present) but
   `StardewModdingAPI` binary missing; `StardewModdingAPI.deps.json` missing
   (manual-copy installs skip the deps.json duplication step — the
   `README.txt` manual flow lists it as an explicit step, so its absence is the
   classic "copied files by hand" fingerprint); version conflict between
   sources (§3). Report *which* pieces are missing, since the fix is
   "reinstall SMAPI" (which auto-cleans old files first).
7. **Xbox-app rename state**: `Stardew Valley.exe` present but is actually
   SMAPI (copy-renamed), vanilla binary under another name. A name-only check
   misclassifies this; content/version check (§3-1) is required.
8. **Store/prefix confusion**: Linux SMAPI files under a Proton prefix layout
   (or Windows `StardewModdingAPI.exe` where the game launched is Proton) —
   report platform mismatch, not merely "installed".

## Sources

Every URL consulted (all primary: SMAPI docs/site, wiki modding pages, SMAPI
source on GitHub):

- https://smapi.io/ (homepage: current version 4.5.2, platform/requirements statement)
- https://smapi.io/log (log-parser page: per-OS log paths, parsed `SMAPI:` / `Stardew Valley:` fields)
- https://stardewvalleywiki.com/Modding:Player_Guide/Getting_Started (game-folder table per OS/store; mod-folder layout; `--mods-path` mod groups)
- https://stardewvalleywiki.com/Modding:Installing_SMAPI_on_Windows (installer flow; Steam launch-options line; GOG `start.bat`; Xbox-app rename procedure; uninstall)
- https://stardewvalleywiki.com/Modding:Installing_SMAPI_on_Mac (macOS installer flow; no Steam launch-option change)
- https://stardewvalleywiki.com/Modding:Installing_SMAPI_on_Linux (Linux installer flow incl. `chmod +x`; xterm note)
- https://stardewvalleywiki.com/Modding:Installing_SMAPI_on_Steam_Deck (native vs Proton install; compatdata path; `libhostpolicy.so` mismatch symptom)
- https://stardewvalleywiki.com/Modding:Player_Guide/Troubleshooting (version shown atop console; launch SMAPI vs vanilla binaries per OS; content-reset vs launcher-reinstall notes)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI.Installer/InteractiveInstaller.cs (`BundledModIds`; `GetUninstallPaths` installed/obsolete file lists; launcher backup+swap; deps.json copy; bundled-mod copy by UniqueID; Steam launch text; Xbox/game-path validation; `GameFolderType` warnings)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI.Installer/Framework/InstallerPaths.cs (`StardewValley` ↔ `unix-launcher.sh` ↔ `StardewValley-original` paths; `smapi-internal/config*.json` paths)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI.Installer/Framework/InstallerContext.cs (platform detection; installer version from assembly)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI.Installer/assets/README.txt (manual-install steps: `install.dat` unzip, deps.json copy, Unix launcher rename)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI.Toolkit/Framework/GameScanning/GameScanner.cs (validity = contains game executable/DLL; default install paths per OS incl. Flatpak; `GetGameFolderType` legacy/compat checks)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI.Toolkit/Framework/GameScanning/GameFolderType.cs (`Valid` / `NoGameFound` / `LegacyVersion` / `LegacyCompatibilityBranch` / `InvalidUnknown`)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI.Toolkit/Framework/Constants.cs (`GameDllName = "Stardew Valley.dll"`)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI/Constants.cs (`RawApiVersion = "4.5.2"`; log/save/config/internal path constants; crash/update marker paths; `SMAPI-config.json` mod-group override)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI/Program.cs (startup assembly-version self-check against `Constants.ApiVersion`)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI/Framework/SCore.cs (console/game window titles carry both versions)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI/Framework/Logging/LogManager.cs (`LogIntro` header line format; crash/update marker read-write semantics)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI/Framework/Logging/LogFileManager.cs (log file writer)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI/SMAPI.config.json (full default config — verified to contain no version field; `SuppressUpdateChecks` bundled IDs)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI/SMAPI.csproj (assembly name `StardewModdingAPI`; `SMAPI.metadata.json` content link)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI.Web/wwwroot/SMAPI.metadata.json (mod-compatibility data incl. obsolete `SMAPI.ErrorHandler` — verified no SMAPI version field)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI.Mods.ConsoleCommands/manifest.json (`UniqueID SMAPI.ConsoleCommands`, `Version 4.5.2`)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI.Mods.SaveBackup/manifest.json (`UniqueID SMAPI.SaveBackup`, `Version 4.5.2`)
- https://github.com/Pathoschild/SMAPI/blob/develop/docs/technical/smapi.md (version-format scheme; release process sets version in 3 places; CLI args incl. `--mods-path`; env-var equivalents; `smapi-internal/config.json` customisation)
- https://github.com/Pathoschild/SMAPI/blob/develop/docs/release-notes.md (release-notes conventions: per-release game-version requirement, "for players/mod authors" sections — latest-install reference, not on-disk source)
- https://github.com/Pathoschild/SMAPI/blob/develop/docs/README.md (bundled Save Backup keeps 10 daily backups)
- https://github.com/Pathoschild/SMAPI/blob/develop/build/common.targets (`<Version>4.5.2</Version>` build version source)
- https://github.com/Pathoschild/SMAPI/blob/develop/build/deploy-local-smapi.targets (deployed file list: game-dir binaries, `smapi-internal/*`, `Mods/<AssemblyName>` bundled mods)
- https://github.com/Nexus-Mods/NexusMods.App/blob/main/docs/developers/games/0001-StardewValley.md (third-party corroboration: `install.dat` = ZIP of game-folder files; per-OS loader names; Unix launcher replacement; Steam/GOG/Xbox launch configuration)

`[UNVERIFIED]` items: exact PE version-resource field mapping for (1) (assembly
carries the version per the startup self-check, but fields weren't inspected
binary-by-binary); `StardewModdingAPI.deps.json` carrying no version (inferred
from installer construction as a copy of the game's file); no web access was
*not* an issue — all of the above was fetched live; anything else in this file
stated without an `[UNVERIFIED]` tag was directly verified in the listed
primary source.
