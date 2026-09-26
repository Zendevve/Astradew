# Real mod archive layouts and mod-root discovery (import research)

Research for ticket #46 (Phase 3 map). Scope: what Stardew Valley mod
distributions are actually shaped like inside, how SMAPI's installer and
SMAPI-adjacent tools decide which folder inside an archive is a mod, and the
failure shapes the community has documented. Upstream references: SMAPI
`develop` (4.5.x era), Vortex `master` (snapshot 2026-09-24), Stardrop
`development` (snapshot 2026-09-24), the Stardew Valley Wiki and the Stardew
Modding Wiki. Every claim below is read from source, from the live release
asset named, or from the wiki page quoted; `[INFERENCE]` marks anything I
concluded rather than read.

## 1. The mod-folder shape is a convention, not a file format

- SMAPI loads mods from a **directory tree**, never from an archive. The player
  guide's install instruction is "just unzip it into the `Mods` folder in your
  game folder" (`Modding:Player_Guide/Getting_Started`, stardewvalleywiki). The
  scanner walks `DirectoryInfo` objects
  (`src/SMAPI.Toolkit/Framework/ModScanning/ModScanner.cs`); nothing in SMAPI
  reads a user-supplied `.zip`. **There is no first-level stripping rule to copy
  from SMAPI, because SMAPI never unpacks a mod archive.**
- What makes a folder a mod is a `manifest.json` at that folder's top level:
  "Every SMAPI mod or content pack must have a `manifest.json` file in its
  folder." (`Modding:Modder_Guide/APIs/Manifest`). A content pack is the same
  shape with `ContentPackFor` instead of `EntryDll`
  (`Modding:Content_packs`, `ModScanner.ReadFolder`).
- The wiki's structural reference for a code mod lists: required - the DLL and
  `manifest.json`; optional - extra `.dll`/`.pdb`, an `i18n` translation folder,
  and any custom files "usually in an `assets` folder by convention"
  (`Modding:Modder_Guide/APIs/Mod_structure`).
- Content-pack folder names follow a community convention: "upper camel case,
  with an acronym prefix in square brackets showing which mod it's for", e.g.
  `[CP] SampleName`; the folder contains `manifest.json`, the framework's JSON
  (`content.json` for Content Patcher) and an `assets` folder
  (`Modding:Content_packs#Conventions`).

## 2. The canonical release layout produced by official tooling

SMAPI's mod build package (`Pathoschild.Stardew.ModBuildConfig`, the package the
wiki and docs tell authors to use) writes the release zip in
`DeployModTask.CreateReleaseZip`
(`src/SMAPI.ModBuildConfig/DeployModTask.cs`):

- **One mod in the project:**

  ```
  <ModFolderName>/…            # ModFolderName defaults to the project name
  ```

- **Multiple mods bundled (the project's own C# mod plus content packs):**

  ```
  <ModFolderName>/<main mod folder>/…
  <ModFolderName>/<content pack folder>/…
  ```

  i.e. an extra parent folder appears as soon as more than one unit is packaged
  (`modPackages.Count == 1 ? $"{modFolder}/{relativePath}" : $"{ModFolderName}/{modFolder}/{relativePath}"`).

- The **zip file name** carries the version - `<ModFolderName> <version>.zip` -
  while the wrapper folder inside the zip does not.
- Non-mod files are excluded by default, not cleaned up afterwards: the task
  drops things like `.zip`, `*.deps.json`, code-analysis output, `.DS_Store` and
  `Thumbs.db`, and offers `IgnoreModFilePaths` / `IgnoreModFilePatterns` for the
  rest (`DeployModTask.cs`, `MainModFileManager.ShouldIgnore`).
- The documented rationale for the wrapper folder, from the author docs: the zip
  is "in the format recommended for uploading to mod sites like Nexus Mods", and
  bundled content packs "will be grouped with the main mod into a parent folder
  automatically" (`docs/technical/mod-package.md`, sections *Release zip* and
  *Bundled content packs*).
- Naming note for anyone grepping for it: there is no `ModZipWriter` type in
  SMAPI's tree any more. The zip-writing code is the build task
  (`DeployModTask.CreateReleaseZip`) plus the file managers
  (`MainModFileManager`, `ContentPackFileManager`, `IModFileManager`,
  `BundleFile`); the mod build package's repo was merged into
  `Pathoschild/SMAPI` under `src/SMAPI.ModBuildConfig/`.

**A real multi-unit project:** Stardew Valley Expanded's project file declares
two content packs alongside the main mod
(`StardewValleyExpanded.csproj`, `FlashShifter/StardewValleyExpanded`):

```xml
<ContentPacks Include="../[CP] Stardew Valley Expanded" Version="$(Version)" />
<ContentPacks Include="../[FTM] Stardew Valley Expanded" Version="$(Version)" />
```

[INFERENCE] applying the packaging rule above, the published SVE archive is
therefore `Stardew Valley Expanded/StardewValleyExpanded/…`,
`Stardew Valley Expanded/[CP] Stardew Valley Expanded/…`,
`Stardew Valley Expanded/[FTM] Stardew Valley Expanded/…` - one wrapper folder,
three sibling units. The archive itself was not downloadable (see §7), so the
layout is derived from the build configuration, not observed.

## 3. What releases actually look like (sampled)

Sample: 18 mod archives pulled from GitHub release assets (2026-09-26), chosen
because they were reachable without an account, plus SMAPI's own 4.5.2 release
assets. The sampled asset names and their repositories are listed in *Sources*.

Finding, uniform across the mod sample:

- **18 of 18 archives have exactly one top-level folder and no loose top-level
  files.** The wrapper folder name always equals the mod name (no version
  suffix); the version lives in the archive file name (e.g.
  `FishZones.0.3.2.zip` -> `FishZones/`).
- **18 of 18 have exactly one `manifest.json`, at the top of that wrapper
  folder.** No sample carried a nested or misplaced manifest.
- Everything the mod needs is inside the wrapper. Observed contents:

  | content | example archive |
  |---|---|
  | `manifest.json` + `<Mod>.dll` (minimum) | `FixIndoorItemMoves.0.1.0.zip` (2 entries, 11 KB) |
  | `i18n/*.json` | `ZoomLevel.4.5.1.zip` (7 locales + duplicated `default.json`/`en.json`) |
  | `assets/*.png` | `EvilFarmOwner.0.5.2.zip`, `Informant-1.4.0.zip`, `QisFadingElevator.0.8.7.zip` |
  | extra `.dll`/`.pdb`, third-party DLLs | `EnergyStar.1.2.1.zip` (two DLLs + pdb), `ProjectFluent.2.0.0.zip` (3 `Linguini.*.dll`) |
  | license inside the wrapper | `EvilFarmOwner`, `Informant`, `Challenger`, `WarpToTeammate` |
  | readme inside the wrapper | `Informant/Readme.html`, `Challenger/Readme.html`, `WarpToTeammate/README.txt`, `MappingExtensionsAndExtraProperties/release-notes.md` |
  | **pre-shipped `config.json`** | `BetterRanching.2.0.5.zip` -> `BetterRanching/config.json` (defaults: `{"PreventFailedHarvesting": true, "DisplayHearts": true, …}`) |

- The pre-shipped config is a real shape to expect: SMAPI only *creates*
  `config.json` when it is missing ("SMAPI will create the `config.json` file
  automatically if it doesn't exist yet", `Modding:Modder_Guide/APIs/Config`),
  and the player guide notes some mods ship one while most only generate it on
  first launch (`Modding:Player_Guide/Getting_Started#Configure_mods`),
  `ModScanner.ConfigFileName` is `config.json`.
- `readme`-class files are ignored by SMAPI's scanner anyway (see §4), so they
  neither create nor destroy a unit once installed.

### 3a. SMAPI's own installer archive (observed, full listing)

`SMAPI-4.5.2-installer.zip` (41,889,142 bytes, latest release at time of
writing) has one wrapper folder whose name **does** carry the version:

```
SMAPI 4.5.2 installer/
├── README.txt
├── install on Windows.bat
├── install on Linux.sh
├── install on macOS.command
└── internal/
    ├── windows/{SMAPI.Installer.exe, SMAPI.Installer.dll, …, install.dat}
    ├── linux/{SMAPI.Installer, …, install.dat}
    └── macOS/{SMAPI.Installer, …, install.dat}
```

Each `install.dat` is a **nested zip** (renamed so people don't "helpfully"
extract it), and its payload is an archive that itself contains a `Mods/`
folder:

```
Mods/ConsoleCommands/{manifest.json, ConsoleCommands.dll}
Mods/SaveBackup/{manifest.json, SaveBackup.dll}
StardewModdingAPI.exe, StardewModdingAPI.dll, …, steam_appid.txt
smapi-internal/** (38 entries: DLLs, i18n/, config.json, metadata.json, …)
```

The bundled `README.txt` documents the manual path - unzip
`internal/windows/install.dat` (the text says `internal/unix/install.dat` for
Linux/macOS, while the archive actually ships `internal/linux` and
`internal/macOS`) and "change '.dat' to '.zip', it's just a normal zip file
renamed". The `.bat` refuses to run from a temporary extraction directory with
"Oops! It looks like you're running the installer from inside a zip file."

### 3b. A deliberately nested archive in the wild

The same release publishes two assets: `SMAPI-4.5.2-installer.zip` **and**
`SMAPI-4.5.2-installer-double-zipped.zip`. The latter has exactly one entry,
`SMAPI-4.5.2-installer.zip` (deflate, 41,766,330 bytes stored / 41,889,142 bytes
as the payload - byte-for-byte the size of the plain asset), i.e. a zip whose
only content is the zip, published on purpose (macOS unzip behaviour). Nesting
is therefore not only an accident shape a sampler has to tolerate: it is shipped
by the ecosystem's canonical tool, which then tells users to unzip it first.

## 4. What SMAPI considers a unit once files are on disk

Verified in `ModScanner.cs` (line numbers from the 4.5.x tree):

- Manifest lookup is **top level only** (`FindManifest`, ~261-283): plain
  `manifest.json`, plus a case-insensitive fallback when the
  `useCaseInsensitiveFilePaths` flag is on (default true on Android/Linux).
- Traversal (`GetModFolders`, ~207-236): a dot-prefixed folder at any depth
  yields an explicit `Ignored` record and prunes the subtree; a folder failing
  the name filter is pruned silently.
- `IsModSearchFolder` (~286-294) is the rule that decides "organizer folder" vs
  "mod folder": a non-root folder is descended into only if it has >= 1 relevant
  subfolder **and zero relevant files**; otherwise it becomes the leaf candidate
  and is reported `ManifestMissing` if it has no manifest. **A single stray
  relevant file next to a nested mod folder therefore hides the whole subtree.**
- `IsRelevant` (~316-331) filters files whose extension is in
  `IgnoreFileExtensions` (`doc(x)`, `md`, `rtf`, `txt`, `bmp/gif/ico/jpeg/jpg/`
  `png/psd/tif/xcf`, `rar/zip/7z/tar/tar.gz`, `backup/bak/old`, `url/lnk`) or
  whose name starts with `.`; and folders/files matching
  `IgnoreFilesystemNames`: `__folder_managed_by_vortex`,
  `^\._|^\.DS_Store$|^__MACOSX$|^mcs$`, `desktop.ini|Thumbs.db`. The `mcs` entry
  is the macOS symlink the installer also skips (§5); `__MACOSX` and `._*` are
  the classic macOS zipping artifacts.
- A manifest-less folder of `.xnb` files (plus `.json`/`.yaml`) is reported as
  `ModType.Xnb` with the message "it's not a SMAPI mod (see
  https://smapi.io/xnb for info)" (`IsXnbMod`, ~334-355). Manifest presence wins
  over XNB detection (`ReadFolder` checks the manifest first).
- A folder containing the installer scripts (`install on Linux.sh`,
  `install on macOS.command`, `install on Windows.bat`) is reported
  `ManifestMissing` with "the SMAPI installer isn't a mod (you can delete this
  folder after running the installer file)" (`ReadFolder`, ~149) - a
  known-in-the-wild shape SMAPI recognizes by name.
- Loose files directly under `Mods/` are never units: `SCore.cs` (~466-479)
  logs "Detected mod files directly inside the 'Mods' folder. These will be
  ignored. Each mod must have its own subfolder instead." (that error fires when
  a loose file is `manifest.json` or `*.dll`) plus the full
  "Ignored loose files: …" list.

## 5. How SMAPI's own installer decides what to install from a zip

The installer only ever consumes **its own** bundle; it has no third-party
archive import path.

- `src/SMAPI.Installer/Program.cs`: reads `install.dat` next to the platform
  executable and calls `ZipFile.ExtractToDirectory` into
  `%TEMP%/SMAPI-installer-<guid>`. It then expects `smapi-internal` **directly at
  that zip's root** (`InternalFilesPath = <extracted>/smapi-internal`) for
  assembly resolution - a wrapper folder inside `install.dat` would break the
  installer.
- `InteractiveInstaller.Run` (`src/SMAPI.Installer/InteractiveInstaller.cs`):
  copies everything from the bundle root into the game folder except entries
  rejected by `ShouldCopy`, which is exactly:
  `"mcs" => false, // ignore macOS symlink` and
  `"Mods" => false, // Mods folder handled separately`.
  So there is **no first-level stripping and no root detection**; the bundle's
  own layout is fixed by the build script.
- Bundled mods: the `Mods/` folder from the bundle is scanned with the same
  `ModToolkit.GetModFolders` as normal mods, each result must have a manifest,
  its `UniqueID` must be in the hard-coded list `["SMAPI.SaveBackup",
  "SMAPI.ConsoleCommands"]` (anything else logs "ignored unknown '…' mod in the
  installer folder"), and it is copied into the game's `Mods` folder replacing
  the existing folder with the same `UniqueID` (with a staging rename if the
  folder was renamed by the user).
- Release packaging: `build/scripts/prepare-install-package.ps1` builds
  `bin/SMAPI <version> installer/internal/<platform>/bundle/{smapi-internal,
  Mods/<bundled mod>, StardewModdingAPI*}` and compresses each `bundle/*` into
  `install.dat`; `build/scripts/finalize-install-package.sh` fixes Unix
  permissions, zips `install.dat`, then zips the wrapper folder.
- The Vortex extension carries a **captured real listing** of the same layout
  (`.../installers/smapi/fixtures/archiveListings.ts`, "captured from
  `~/Downloads/temp/SMAPI 4.5.1 installer`"), matching the 4.5.2 asset I listed.

## 6. How SMAPI-adjacent tools locate mod roots inside archives

### 6.1 Vortex (Nexus Mod Manager) - Stardew Valley game extension

`extensions/games/game-stardewvalley/` (Vortex `master`, 2026-09-24). Its
`README.md` states the decision matrix: `SMAPI.Installer.dll` present -> SMAPI
installer; else top-level `Content/` present -> root-folder installer; else a
valid `manifest.json` -> standard mod installer.

- `installers/archiveClassifier.ts`: `hasManifest` is true if **any** archive
  entry ends in `manifest.json` (case-insensitive) **excluding** paths with a
  `locale` segment; `hasContentFolder` true if any directory entry ends in
  `Content/` (the `fakeDir` prefix trick); `hasSmapiInstallerDll` matches
  `smapi.installer.dll` anywhere.
- `installers/stardewValleyInstaller.ts` (multi-unit support): every
  `manifest.json` entry is a candidate root; the root folder is that manifest's
  directory. Copy instructions are
  `destination = path.join(modName, file.substr(index_of("manifest.json")))`,
  where `modName` is the root folder name, or the manifest's `Name` when the
  manifest sits at the archive root. **The wrapper folder is preserved as the
  installed folder name; it is not stripped.** Manifests that fail to parse are
  warned about and skipped (with an offer to "Unpack (as-is)").
- `installers/rootFolderInstaller.ts` (legacy/root-shaped archives): requires a
  `Content/` directory entry, then rewrites paths relative to the folder
  *containing* `Content/` - i.e. this is the one path where a wrapper folder is
  stripped, and `.txt` files are dropped:
  `Will be deployed => ../SomeMod/Content/`, `Will NOT be deployed =>
  ../Readme.doc`.
- Mod types (`registration/registerModTypes.ts`): `SMAPI` (deploys to the SMAPI
  path), `sdvrootfolder` (deploys to the game root, matched by instructions
  targeting `Content/`), `sdv-configuration-mod` (a synthetic mod that stores
  generated `config.json` files in staging and re-deploys them; see
  `configMod/README.md`: many mods only create `config.json` after the game runs
  and it "can be lost when a mod is updated, replaced, or removed").
- Generic archive hygiene: `src/renderer/.../mod_management/constants.ts`
  `DEPLOY_BLACKLIST` never deploys `.git`, `.gitignore`, `meta.ini`, the
  override-instructions file, and **both `_macosx/**` and `__MACOSX/**`**.
- SMAPI archive handling (`installers/smapi/index.ts`): finds
  `internal/<platform>/install.dat` (or `*-install.dat` / `install.dat`;
  `windows.ts`, `linux.ts`, `macos.ts`), extracts that **nested** archive with
  the bundled 7-Zip, then re-roots every extracted path at the SMAPI executable
  (`file.substr(index_of(executableName))`) and records the names under
  `Mods/<name>` as bundled mods. macOS is stubbed (`implemented: false`).

### 6.2 Stardrop (community mod manager)

`Stardrop/ViewModels/MainWindowViewModel.cs` (branch `development`,
2026-09-24):

- Directory scan (`GetModFolders`): recurse subfolders; a folder containing
  `manifest.json` (case-insensitive) is a mod and is **not descended into**; the
  same enumeration picks up `config.json` when present.
- Archive install (`DirectModInstallAsync`): opens the archive with
  `ArchiveFactory` (SharpCompress) and treats **every** `manifest.json` entry as
  a mod root (multi-unit supported). If the manifest path is at the archive root,
  the mod is installed into `Mods/<UniqueID>`; otherwise the archive's own
  folder path is kept.
- Entries containing `__MACOSX` or `.DS_Store` are skipped on install (also in
  the update and collection paths: `MainWindow.axaml.cs`,
  `MainWindowCollections.cs`), and destinations are re-sanitized underneath the
  install path.

### 6.3 smapi.io web app

`src/SMAPI.Web/Framework/Caching/ModDataset/ModDatasetRepository.cs` downloads
the mod dataset as a zip archive (a GitHub branch download) and then **searches
the whole extracted tree for a directory named `dataset`** - it does not assume
the dataset sits at the archive root -
("Locating 'dataset' folder…", `EnumerateDirectories("*", AllDirectories)`,
error if absent). The official web tooling therefore handles the wrapper by
looking for a marker folder at arbitrary depth rather than assuming a depth.

## 7. Documented "extracted wrong" and stray-file shapes

- Player guide (`Modding:Player_Guide/Getting_Started#Install_mods`), verbatim
  tips: "Download mods into a folder other than `Mods`, unzip them there, and
  then move their folder(s) into `Mods`. That helps prevent errors related to
  extra files." and "If you have a folder that looks like
  `PineapplesEverywhere-1234567890`, check inside it for the actual mod folder.
  Folders named like this often have more folders and possibly readme files
  inside them." - the canonical description of a container folder holding a
  readme plus the real mod folder (the name pattern matches the
  `Name-<mod id>-<version>-<timestamp>` form Nexus uses for downloads; one
  GitHub-hosted release asset observed in this research follows it:
  `Transtar-20435-3-0-3-1766741885.exe` in `wanniwa/transtar`).
- Stardew Modding Wiki (`Installing Mods`): "Do NOT put the zip files in the
  `Mods` folder, and do NOT use an option to create a new folder with the same
  name as the zip file. Just 'extract here' or 'extracts to
  Stardew Valley\Mods'." - the documented cause of the extra wrapper level.
- SMAPI log tutorial (`How to read your SMAPI log`, §5b) documents the
  double-install variant: "`[JA] Water Bottle` is in 2 different folders: in
  `Mods > [JA] Water Bottle`, but also in a subfolder,
  `Mods > [JA] New Items > [JA] Water Bottle` … often happens when users group
  their mods into folders by category … or if they downloaded a **mega pack** of
  a mod, as well as an individual mod that's a part of the mega pack, and
  installed both (for example, the PPJA collection as well as Fruits and
  Veggies)". PPJA is a real example of a project that publishes many packs
  (`paradigmnomad/PPJA` holds 14 `[PPJA] …` pack folders and ships per-pack
  downloads: "download the file for the content pack you have chosen, extract
  it, and move it to the `Mods` folder", repo README).
- Loose files at the `Mods` root: SMAPI's own error (§4). A stray *relevant*
  file next to nested folders is the scanner-level version of the same mistake
  (§4, `IsModSearchFolder`); a stray `.txt`/`.md`/`.png` is ignored by the name
  filter and harmless.
- macOS metadata: `__MACOSX/**`, `._*`, `.DS_Store` and `mcs` are handled
  defensively by three independent codebases (SMAPI scanner ignore list and
  installer `ShouldCopy`; Vortex `DEPLOY_BLACKLIST`; Stardrop install/update/
  collection paths). `mcs` is a macOS symlink, not a folder. My 18-archive
  sample was GitHub-hosted and contained none of these, so the evidence that
  they occur in the wild is the tooling that skips them, not a specimen.
- Legacy XNB-only packs (`Modding:Using_XNB_mods`, `smapi.io/xnb` redirects
  there): "XNB mods replace files in your game's `Content` folder"; "If a mod
  has some `.xnb` files and no `manifest.json`, it's an XNB mod"; install is
  "unzip it somewhere (not in your game folder!) … replace the existing `.xnb`
  files under `Content` with the ones from the mod" - i.e. the archive mirrors
  the game's `Content/` tree and cannot be installed into `Mods`. SMAPI reports
  such a folder as `ModType.Xnb` and never loads it; Vortex routes archives with
  a top-level `Content/` to `sdvrootfolder` (game root) instead of `Mods`.
  Consoles aside, the wiki's own position is that XNB mods are discouraged and
  Content Patcher packs are the replacement.

### 7.1 Corpus limitations (what could not be verified here)

- Nexus Mods (403 to this client) and CurseForge (403) were not downloadable, so
  the bulk of *real* Stardew distribution volume is not in the sample. The 18
  archives sampled are GitHub release assets only, and GitHub mirrors are
  produced by CI, which biases the sample towards tidy, single-wrapper layouts.
  Nexus-side shapes (version/download-id suffixed container folders,
  `__MACOSX` strays, screenshots dumped next to the mod folder) are therefore
  evidenced from wiki text and tool behavior, not specimens.
- The unauthenticated GitHub API rate limit forced the sample to be assembled
  from repo searches; no exhaustive release survey was possible.
- SVE's and PPJA's actual download archives were not obtainable (Nexus-hosted);
  their layouts are cited from project files/readme instead and marked as such.
- The `SMAPI-4.5.2-installer-double-zipped.zip` listing was obtained by reading
  the archive's central directory over HTTP range requests (the full 41 MB body
  was still downloading); it reports exactly one entry, matching the plain
  installer asset. Everything else in §3a was read from the complete archive.

## 8. What this means for the Phase 3 importer (facts, not decisions)

1. **`manifest.json` presence is the only root marker every tool agrees on**
   (SMAPI scanner, build package, Vortex, Stardrop). A folder is a candidate
   unit if it has a manifest at its top; anything deeper is a wrapper to keep,
   not to search.
2. **Do not "strip the first level".** The observed norm is exactly one wrapper
   folder, but the wrapper is semantically the mod folder name (Vortex installs
   it verbatim; Stardrop keeps it), and multi-unit archives put an extra parent
   level above several units (build-package rule, SVE). Root-level manifests
   exist as a supported edge (Stardrop renames to `UniqueID`, Vortex uses the
   manifest `Name`), so both depths must be handled.
3. **Extra files are normal.** Readmes, licenses, release notes, screenshots in
   `assets/`, `.pdb`, third-party DLLs and a pre-shipped `config.json` all appear
   inside real wrappers. A taxonomy of "unexpected file" is a product decision,
   not something the archive layout can settle: SMAPI itself only cares about
   manifests, DLLs and XNB detection, and ignores `.txt/.md/.png/...` entirely.
4. **`config.json` has a lifecycle.** It may ship in the archive (BetterRanching)
   and it is also generated at runtime; Vortex maintains a synthetic mod purely
   to preserve it across installs. Any install step that treats `config.json` as
   junk risks destroying user settings.
5. **Wrapper shapes that must be recognized as *not* a Mods unit:** a top-level
   `Content/` (XNB/root-folder mod -> game root, never `Mods`) and the SMAPI
   installer bundle (`install on …` scripts / `SMAPI.Installer.dll`).
6. **Nested archives are real**: SMAPI ships `install.dat` inside its installer
   zip and publishes a double-zipped variant; the installer refuses to run from
   inside a zip. A policy is needed for "archive contains another archive".
7. **Environment noise to keep quiet about:** `__MACOSX`, `._*`, `.DS_Store`,
   `mcs`, `Thumbs.db`, `desktop.ini`, `__folder_managed_by_vortex` - SMAPI
   ignores them, Vortex never deploys them, Stardrop skips them. They should not
   be surfaced as content, and `__MACOSX` folders must not make a package look
   empty or nested.
8. **`Mods/` as a wrapper:** the installer payload and community modpacks ship
   `Mods/<mod>/…`; SMAPI's runtime agrees that a `Mods` folder is a container,
   not a unit.

## Sources

SMAPI source (read from the `develop` snapshot, 2026-09-26):

- `src/SMAPI.Toolkit/Framework/ModScanning/ModScanner.cs` - ignore tables
  (`IgnoreFilesystemNames`, `IgnoreFileExtensions`), `GetModFolders`,
  `IsModSearchFolder`, `ReadFolder`, `FindManifest`, `IsXnbMod`,
  `IsEmptyVortexFolder`, `ConfigFileName`.
- `src/SMAPI.ModBuildConfig/DeployModTask.cs` - `CreateReleaseZip`,
  `CreateModFolder`, content-pack bundling, `EnableModZip`.
- `src/SMAPI.ModBuildConfig/Framework/MainModFileManager.cs`,
  `.../BundleFile.cs` - what gets included and how paths are relativized.
- `docs/technical/mod-package.md` - "Release zip", "Bundled content packs".
- `src/SMAPI.Installer/Program.cs`, `.../InteractiveInstaller.cs` - bundle
  extraction, `ShouldCopy`, `BundledModIds`, bundled-mod install rules,
  uninstall paths.
- `build/scripts/prepare-install-package.ps1`,
  `build/scripts/finalize-install-package.sh` - how the installer zip and
  `install.dat` are assembled.
- `src/SMAPI/Framework/SCore.cs` - loose-file error at the `Mods` root.
- `src/SMAPI.Web/Framework/Caching/ModDataset/ModDatasetRepository.cs` -
  "Locating 'dataset' folder" by recursive search.

Release assets read (GitHub):

- `Pathoschild/SMAPI` 4.5.2: `SMAPI-4.5.2-installer.zip` (41,889,142 bytes,
  full listing) and `SMAPI-4.5.2-installer-double-zipped.zip` (41,766,494
  bytes; first entry read), including the nested
  `internal/windows/install.dat` payload and `README.txt`,
  `install on Windows.bat`.
- `Pathoschild/StardewMods` - no GitHub releases (tags only), distribution is
  Nexus-side.
- Mod zips sampled (all single-wrapper, one manifest each):
  `Mushymato/StardewMods` (`FixIndoorItemMoves.0.1.0.zip`,
  `FishZones.0.3.2.zip`, `ActualFishInsteadOfIcon.0.1.0.zip`),
  `catamari/GiftTasteHelper` (2.9.1), `thespbgamer/ZoomLevel` (4.5.1, 4.4.0),
  `Shockah/Stardew-Valley-Mods` (`ProjectFluent.2.0.0.zip`),
  `AlanDavison/StardewValleyMods`
  (`MappingExtensionsAndExtraProperties.2.5.2.zip`),
  `urbanyeti/stardew-better-ranching` (`BetterRanching.2.0.5.zip`, ships
  `config.json`), `slothsoft/stardew-informant` (`Informant-1.4.0.zip`),
  `slothsoft/stardew-challenger` (`Challenger-0.5.0.zip`,
  `ChallengerAutomate-0.5.0.zip`), `neoiw0/WarpToTeammate` (1.0.0),
  `Lucenx9/EnergyStar` (1.2.1), `Lucenx9/NpcRadar` (1.2.1),
  `Aveouter/EvilFarmOwner` (0.5.2), `nganlinh4/QisFadingElevator` (0.8.7),
  `reicheltmediadesign/stardew-valley-sign-lock` (`SignLock.1.0.0.zip`).
- `FlashShifter/StardewValleyExpanded`
  `Stardew Valley Expanded/StardewValleyExpanded/StardewValleyExpanded.csproj`
  (`ContentPacks` for `[CP]` and `[FTM]` packs).
- `paradigmnomad/PPJA` (repo tree + README; per-pack downloads).

Tool source (cloned 2026-09-26):

- `Nexus-Mods/Vortex` `master`,
  `extensions/games/game-stardewvalley/` - `README.md`,
  `src/installers/README.md`, `archiveClassifier.ts`,
  `stardewValleyInstaller.ts`, `rootFolderInstaller.ts`, `smapi/index.ts`,
  `smapi/{windows,linux,macos,types}.ts`,
  `smapi/fixtures/archiveListings.ts`, `registration/registerModTypes.ts`,
  `configMod/README.md`, `src/common.ts`; plus
  `src/renderer/src/extensions/mod_management/constants.ts`
  (`DEPLOY_BLACKLIST`).
- `Floogen/Stardrop` `development`,
  `Stardrop/ViewModels/MainWindowViewModel.cs`,
  `Stardrop/Views/MainWindow.axaml.cs`,
  `Stardrop/Views/MainWindowCollections.cs`.

Wiki pages (raw wikitext fetched 2026-09-26):

- stardewvalleywiki.com: `Modding:Player_Guide/Getting_Started`
  (install/update/config tips, XNB section),
  `Modding:Modder_Guide/APIs/Manifest`,
  `Modding:Modder_Guide/APIs/Mod_structure`,
  `Modding:Modder_Guide/APIs/Config`,
  `Modding:Content_packs` (folder naming, pack contents),
  `Modding:Content_pack_frameworks`,
  `Modding:Using_XNB_mods` (also served at `https://smapi.io/xnb`),
  `Modding:Player_Guide/Troubleshooting`.
- stardewmodding.wiki.gg: `Installing_Mods`, `Uninstalling_Mods`, `XNB`,
  `Debugging`, `Getting_Started`, `How_to_read_your_SMAPI_log`.

Unavailable: Nexus Mods and CurseForge (HTTP 403 to this client), the
unauthenticated GitHub API (rate limited; `gh` authenticated calls used instead),
and the full body of `SMAPI-4.5.2-installer-double-zipped.zip`.
