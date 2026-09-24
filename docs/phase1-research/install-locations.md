# Stardew Valley install locations (Phase 1 detector research)

Research-only notes for the install detector. All paths below are locations a
detector can probe or derive; manual folder selection always remains as fallback
(no research needed — it is the catch-all when automatic detection fails).

Key fact used throughout: Stardew Valley's Steam app ID is **413150**, so each
Steam library root `<lib>` holds the game at
`<lib>/steamapps/common/Stardew Valley` with manifest
`<lib>/steamapps/appmanifest_413150.acf` (`installdir "Stardew Valley"`).

## Windows

### Steam — default library
- Default Steam root: `C:\Program Files (x86)\Steam`
- Default game path: `C:\Program Files (x86)\Steam\steamapps\common\Stardew Valley`

### Steam — locating the Steam root (registry mechanism)
- `HKLM\SOFTWARE\WOW6432Node\Valve\Steam` value `InstallPath` (Steam is
  registered as a 32-bit app, so the `WOW6432Node` branch is the usual one).
- Also check `HKLM\SOFTWARE\Valve\Steam` and `HKCU\Software\Valve\Steam` as
  fallbacks.

### Steam — alternate libraries (`libraryfolders.vdf` mechanism)
- Modern file: `<SteamRoot>\config\libraryfolders.vdf`
- Legacy file (check as fallback): `<SteamRoot>\steamapps\libraryfolders.vdf`
- Format: Valve KeyValues/VDF text (not JSON). Modern shape:
  `"libraryfolders" { "<n>" { "path" "<library root>" ... "apps" { ... } } }`;
  legacy shape maps `"<n>"` directly to a path string
  (`"LibraryFolders" { "0" "C:\..." }`). Backslashes are escaped (`\\`).
- Each entry's `path` is the **library root** (contains `steamapps/` beneath it),
  never the `steamapps` dir itself. Enumerate `steamapps/appmanifest_*.acf`
  per library (or match app `413150`) rather than trusting the `apps` block alone.
- Do not assume numeric keys are contiguous; tolerate missing/unmounted entries.

### GOG Galaxy — defaults
- `C:\Program Files (x86)\GOG Galaxy\Games\Stardew Valley`
- `C:\GOG Games\Stardew Valley` (offline-installer-style default)

### GOG — registry/config mechanism
- Installed-game keys under `HKLM\SOFTWARE\WOW6432Node\GOG.com\Games\<id>` (also
  check `HKLM\SOFTWARE\GOG.com\Games\<id>`); per-game values expose the install
  path under names that vary by installer generation (`path`, `InstallLocation`,
  `InstallDirectory`, `LauncherPath`) `[UNVERIFIED — value names and the exact
  Stardew Valley GOG game-ID subkey confirmed only via forum/third-party
  tooling, not first-party docs]`.
- Galaxy client data lives at `C:\ProgramData\GOG.com\Galaxy\`
  (e.g. `config.json`, `storage\galaxy-2.0.db`) `[UNVERIFIED — parsing Galaxy's
  internal database is undocumented; prefer registry keys + default paths]`.

### Xbox app (PC Game Pass)
- Game files live under `C:\XboxGames\Stardew Valley` (inside the `Content`
  subfolder); older/UWP-style packages live under the ACL-protected
  `C:\Program Files\WindowsApps`.
- **Feasibility verdict: NOT feasible to detect reliably** — `WindowsApps` is
  OS-protected/read-only to normal processes (taking ownership breaks Store
  apps), and even `C:\XboxGames` layout is an internal detail; the supported
  route is the Xbox client's per-game "Manage → Files → Browse" / mod-folder UI,
  i.e. effectively manual selection.

## macOS

### Steam — default
- Steam root: `~/Library/Application Support/Steam`
- Default game path:
  `~/Library/Application Support/Steam/steamapps/common/Stardew Valley`
  (game binary inside at `.../Stardew Valley/Contents/MacOS`).
  Note: the wiki prints this path with `SteamApps` capital-A in one place;
  treat the `steamapps` component case-insensitively / probe both.

### Steam — alternates (config mechanism)
- Enumerate libraries via
  `~/Library/Application Support/Steam/config/libraryfolders.vdf`
  (modern location; same VDF shape as Windows), falling back to
  `~/Library/Application Support/Steam/steamapps/libraryfolders.vdf`
  (legacy location) `[UNVERIFIED — legacy-path presence on current macOS Steam
  clients not directly confirmed]`.
- Alternates are typically user-chosen roots on external volumes, e.g.
  `/Volumes/<Disk>/SteamLibrary` (must not point at the `steamapps` dir itself;
  target volumes must be case-insensitive per Valve support docs).

### GOG — defaults
- `/Applications/Stardew Valley.app` with game files at
  `/Applications/Stardew Valley.app/Contents/MacOS`.
- No reliable GOG registry equivalent exists on macOS; Galaxy installs are
  user-relocatable, so probe `/Applications` by bundle name and fall back to
  manual selection.

### Standard placement
- `/Applications/Stardew Valley.app` is the standard macOS placement for both
  GOG and DRM-free copies; always probe it.

## Linux

### Steam — default library
- `~/.local/share/Steam/steamapps/common/Stardew Valley`
- Legacy/compat symlink usually resolving to the same place:
  `~/.steam/steam/steamapps/common/Stardew Valley`

### Steam — alternates (config mechanism)
- `libraryfolders.vdf` lives under the active Steam data dir:
  `<steam-data>/config/libraryfolders.vdf` (modern) with
  `<steam-data>/steamapps/libraryfolders.vdf` as legacy fallback; same VDF
  shape as Windows (POSIX paths, no drive letters).
- Steam data-dir forms by install method:
  - Native/distro package: `~/.local/share/Steam`
  - Compat symlink: `~/.steam/steam`
  - Snap: `~/snap/steam/common/.local/share/Steam` `[UNVERIFIED — Snap path
    confirmed only via forum/packaging sources, not Valve docs]`
  - Flatpak: `~/.var/app/com.valvesoftware.Steam/.local/share/Steam`
    (also seen with `/data/Steam` infix depending on version/distro)
    `[UNVERIFIED — exact Flatpak sub-path varies; verify on target distro]`

### Flatpak Steam — feasibility verdict
- **Feasible (with sandbox caveat)** — game files sit under the Flatpak data
  dir above on the host filesystem, which a host-side detector can read
  normally; only a detector running *inside* a different sandbox would need an
  explicit filesystem permission grant.

### GOG — default
- `~/GOGGames/StardewValley/game` (offline-installer default layout).

### Steam Deck — standard paths
- Internal storage: `/home/deck/.local/share/Steam/steamapps/common/Stardew Valley`
- microSD card: `/run/media/mmcblk0p1/steamapps/...` on older SteamOS, current
  form `/run/media/deck/<card-label>/steamapps/...` (label-dependent, so
  enumerate mount points rather than hardcoding).
- Proton (Windows-version) installs keep prefixes under
  `.../steamapps/compatdata/413150/pfx/...`; prefer the native library path for
  detection and treat compatdata only as a hint.

## Manual selection fallback
- Always offer manual folder selection (user picks the folder containing the
  game executable). No research needed; it covers Xbox-app installs, relocated
  GOG copies, and any layout missed above.

## Sources
- https://stardewvalleywiki.com/Modding:Player_Guide/Getting_Started (game-folder table, per-platform defaults incl. Linux GOG `~/GOGGames/StardewValley/game`, Xbox `C:\XboxGames\Stardew Valley`)
- https://stardewvalleywiki.com/Modding:Installing_SMAPI_on_Windows (Xbox app Manage/Files/Browse flow)
- https://stardewvalleywiki.com/Modding:Installing_SMAPI_on_Steam_Deck (native vs Proton, compatdata path shape)
- https://stardewvalleywiki.com/Saves (Deck save paths corroborating Steam data-dir layout)
- https://smapi.io/ (SMAPI homepage/docs entry)
- https://smapi.io/log (SMAPI log parser)
- https://steamdb.info/app/413150/info/ (Stardew Valley Steam app ID 413150)
- https://help.steampowered.com/en/faqs/view/4B8B-9697-2338-40EC (Steam support FAQ index)
- https://help.steampowered.com/en/faqs/view/0395-A862-13F3-6E82 (Valve: case-sensitive filesystems unsupported on Mac)
- https://store.steampowered.com/news/posts/?enddate=1631920019&feed=mygames%29 (Valve: managing multiple library folders via Storage UI)
- https://github.com/SpecialKO/SKIF/blob/master/src/stores/Steam/steam_library.cpp (config/ vs steamapps/ libraryfolders.vdf locations)
- https://github.com/Playmoir/knowledge-base/blob/main/steam-library-integration.md (VDF escaping, appmanifest enumeration)
- https://deepwiki.com/akorb/SteamShutdown/2.2.2-library-path-detection (modern libraryfolders.vdf field reference)
- https://docs.steambrew.app/users/guides/finding-steam (Windows registry InstallPath discovery)
- https://stackoverflow.com/questions/39557722/where-does-steam-store-library-directories (library discovery via libraryfolders.vdf)
- https://docs.gog.com/faq/ (GOG developer docs confirming `C:\ProgramData\GOG.com\Galaxy` data location)
- https://www.gog.com/forum/general/gog_galaxy_beta_2/page161 (GOG forum identifying `GOG.com\Games` registry location)
- https://www.gog.com/en/game/stardew_valley (GOG store page: Windows/macOS, Galaxy optional)
- https://learn.microsoft.com/en-us/windows/apps/develop/files/file-access-permissions (WindowsApps protection)
- https://learn.microsoft.com/en-us/gaming/gdk/docs/features/common/packaging/packaging-flatfileinstall?view=gdk-2510 (flat-file `C:\XboxGames` vs UWP `WindowsApps`)
- https://github.com/ValveSoftware/steam-for-linux/issues/11109 (`~/.local/share/Steam` vs `~/.steam/steam`)
- https://github.com/ValveSoftware/steam-for-linux/issues/11602 (Flatpak `com.valvesoftware.Steam` prefix)
- https://github.com/ValveSoftware/SteamOS/issues/1043 (Deck SD-card mount paths)
- https://forum.snapcraft.io/t/steam-snap-cant-access-existing-steam-deb-library/32332 (Snap Steam data path)
- https://www.pcgamingwiki.com/wiki/Stardew_Valley (PCGamingWiki cross-reference)
