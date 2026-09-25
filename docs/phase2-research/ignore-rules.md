# SMAPI ignore and nested-loading rules (scanner input research)

Research for ticket #31 (child of map #29). Scope: the exact folder ignore and
nested-loading rules the Phase 2 scanner MUST honor to match SMAPI behavior.
Upstream reference: SMAPI `develop` at time of writing (4.5.x era). All
`ModScanner.cs` line numbers below refer to
`src/SMAPI.Toolkit/Framework/ModScanning/ModScanner.cs` unless noted.

## 1. Authority: where the rules live

Mod discovery is owned by the toolkit scanner, not the loader:

- `ModToolkit.GetModFolders(rootPath, useCaseInsensitiveFilePaths)` delegates to
  `ModScanner.GetModFolders` (`src/SMAPI.Toolkit/ModToolkit.cs`).
- `ModResolver.ReadManifests` (`src/SMAPI/Framework/ModLoading/ModResolver.cs`,
  ~line 30) only iterates the `ModFolder`s the scanner already returned, then
  maps `ModType`/`ModParseError` to load status. It discovers nothing itself.
- `SCore` calls `resolver.ReadManifests(toolkit, this.ModsPath, …,
  useCaseInsensitiveFilePaths: this.Settings.UseCaseInsensitivePaths)` and
  afterwards logs each ignored mod as
  `Skipped <path> (folder name starts with a dot).`
  (`src/SMAPI/Framework/SCore.cs`, ~line 481+).
- **Scanner implication:** our scanner seam MUST replicate `ModScanner`'s
  traversal + ignore logic; replicating only `ModResolver`'s status mapping is
  insufficient.

## 2. Ignore rules (verified in `ModScanner.cs`)

Three independent mechanisms, applied in this order per non-root folder in
`GetModFolders` (~lines 207–236):

### 2a. Dot-prefix folders — hard ignore, still reported

```csharp
if (folder.Name.StartsWith("."))
    yield return new ModFolder(root, folder, ModType.Ignored, null,
        ModParseError.IgnoredFolder, "ignored folder because its name starts with a dot.");
    yield break; // whole subtree pruned, never descended into
```

- Case-sensitive `StartsWith(".")` on the **folder name only** (`.DisabledMod`,
  `.backup`, `.git` all match; `My.Mod` does not).
- Applies to **every non-root folder at any depth**, not just top level.
- Produces a real `ModFolder` with `Type == ModType.Ignored`,
  `ModParseError.IgnoredFolder`, so it IS visible in scan results; `ModResolver`
  then marks it `Failed / DisabledByDotConvention` ("disabled by dot
  convention"). This is how users disable a mod: rename to `.ModName`.
- `yield break` means dot-folder contents are never scanned — a manifest nested
  under a dot-folder is invisible.
- **The root `Mods/` folder itself is exempt** (`isRoot` check): a dot in the
  root path does not ignore the whole scan.

### 2b. `IsRelevant` name filter — silent skip (~lines 316–331)

`IgnoreFilesystemNames` (case-insensitive regexes, matched against file AND
folder names):

| Pattern | Covers |
|---|---|
| `^__folder_managed_by_vortex$` | Vortex mod-manager marker |
| `(?:^\._\|^\.DS_Store$\|^__MACOSX$\|^mcs$)` | macOS metadata (`._` prefix, `.DS_Store`, `__MACOSX`, `mcs`) |
| `^(?:desktop\.ini\|Thumbs\.db)$` | Windows metadata |

- A non-root folder failing `IsRelevant` is skipped with **`yield break` and no
  `ModFolder` at all** — unlike dot-folders, it is invisible in results, and its
  subtree is pruned.
- Note the asymmetry: `__MACOSX` anywhere in the tree silently vanishes
  (no record), while `.OldMods` yields an explicit `Ignored` record.

### 2c. `IsRelevant` file filter — ignored extensions + dot-files

A file is irrelevant if EITHER is true (~line 319):

1. Its extension is in `IgnoreFileExtensions` (case-insensitive): `.doc .docx
   .md .rtf .txt` (text); `.bmp .gif .ico .jpeg .jpg .png .psd .tif .xcf`
   (images); `.rar .zip .7z .tar .tar.gz` (archives); `.backup .bak .old`;
   `.url .lnk` (Windows shortcuts).
2. Its name starts with `.` (dot-files like `.gitignore`).

- Irrelevant files do not count toward `IsModSearchFolder`'s "has files" test
  (§3), do not count as "relevant files" in `ReadFolder`, and are skipped by
  `IsXnbMod` and `RecursivelyGetFiles` (which prunes irrelevant subdirs at
  ~line 301–304).
- **Scanner implication:** a folder containing ONLY `readme.md` + `screenshot.png`
  is an `EmptyFolder`, not `ManifestMissing` — the ignored extensions decide
  which error surfaces.

## 3. Nested-loading rules (verified)

### 3a. The core rule: `IsModSearchFolder` (~lines 286–294)

```csharp
if (root.FullName == folder.FullName) return true; // Mods/ root always descends
DirectoryInfo[] subfolders = folder.GetDirectories().Where(this.IsRelevant).ToArray();
FileInfo[] files = folder.GetFiles().Where(this.IsRelevant).ToArray();
return subfolders.Length > 0 && files.Length == 0;
```

A non-root folder is **descended into (treated as an organizer/grouping folder)**
iff it has ≥1 relevant subfolder AND zero relevant files. Otherwise it is a
**leaf mod candidate** passed to `ReadFolder`, which looks for `manifest.json`
in that folder only (top level, plus a case-insensitive fallback when
`useCaseInsensitiveFilePaths` is set — §6).

Consequences:

- **Arbitrary nesting depth works**: `Mods/GroupA/GroupB/RealMod/manifest.json`
  loads as one live unit with directory `Mods/GroupA/GroupB/RealMod`, PROVIDED
  every intermediate folder holds only subfolders (no relevant files of its
  own). There is no depth limit in code.
- **The ticket's example `Mods/OldMods/ContentPatcher`**: if `OldMods/`
  contains only subfolders and `ContentPatcher/` contains `manifest.json`, then
  YES — `ContentPatcher` loads as a live unit. `OldMods` is just an organizer.
- **One stray relevant file collapses an organizer into a leaf**: if
  `OldMods/` also contains e.g. `notes.json` (relevant extension), `OldMods`
  itself becomes the mod candidate; `ReadFolder` looks for
  `OldMods/manifest.json`, fails, and reports `ManifestMissing` — the nested
  `ContentPatcher/` underneath is NEVER scanned (its manifests are invisible).
  Mixed file+subfolder folders are the classic "extracted the zip one level
  wrong" failure.
- **Root `Mods/` always descends** regardless of its own files; loose files at
  root are handled separately (§4).

### 3b. `TryConsolidate` — the multi-child merge (~lines 242–258)

After recursing into a non-root organizer's children, if there are **>1** child
results:

- All `EmptyFolder` → collapse to ONE `Invalid/EmptyFolder` record on the parent.
- All `Xnb` or `EmptyFolder` (≥1 Xnb) → collapse to ONE `Xnb` record on the parent.
- Otherwise children pass through unchanged (each nested unit stays its own record).

Single-child organizers pass the child through with its own directory (no
merging). Consolidation never merges live SMAPI/content-pack units.

### 3c. What `ReadFolder` accepts as a unit

- `manifest.json` MUST be in the candidate folder's top level
  (`FindManifest`, ~lines 261–283). `Mods/Foo/src/manifest.json` with nothing
  at `Foo/` top level does not load — `Foo` is a leaf (subfolder + no files →
  wait, no: `Foo` HAS a relevant subfolder `src` and no relevant files, so it
  IS a search folder and descends; then `src/` is the leaf and loads fine).
  Correct restatement: the manifest must sit exactly at the leaf the traversal
  bottoms out at; there is no upward or deep search for it.
- Type split on the parsed manifest: `EntryDll` set (and no `ContentPackFor`) →
  `Smapi`; `ContentPackFor.UniqueID` set (and no `EntryDll`) → `ContentPack`;
  both or neither → `Invalid`. The folder-nesting logic does not distinguish
  code mods from content packs — both are "units" found the same way.

## 4. Files at the Mods root (verified in `SCore.cs`, ~lines 466–479)

- The root is always a search folder, so **loose files directly under `Mods/`
  are never treated as a mod** — no `ModFolder` is produced for them.
- Separately, `SCore` logs an error when loose files are present:
  `Detected mod files directly inside the 'Mods' folder. These will be ignored.
  Each mod must have its own subfolder instead.` followed by
  `Ignored loose files: <names>`. (Trigger detail: the first message fires only
  if a loose file is named `manifest.json` (case-insensitive) or ends `.dll`;
  the name list logs regardless. `[VERIFIED in SCore.cs source]`.)
- **Scanner implication:** report root-level files as ignored/loose with the
  same "own subfolder" wording; never synthesize a unit from them.

## 5. Symlinks `[INFERENCE — no explicit handling found in SMAPI source]`

- Searched terms `LinkTarget`, `ReparsePoint`, `symbolic`, `ResolveLinkTarget`
  across SMAPI source: **zero hits** in mod-scanning/installer paths. No
  setting (`SMAPI.config.json` / `SConfig`) governs link behavior.
- Discovery uses plain `System.IO.DirectoryInfo` enumeration
  (`EnumerateDirectories`, `GetDirectories`, `GetFiles`, `GetFileSystemInfos`),
  which on .NET follows directory symlinks/junctions transparently. So a
  symlinked mod folder (`Mods/MyMod → D:\Projects\MyMod` with `manifest.json`
  at the target top) is expected to scan exactly like a real folder, and
  symlinked organizer folders should descend normally.
- Consequences for the scanner seam: mirror `DirectoryInfo` semantics — follow
  directory links during traversal (cycle risk only if a link loop exists on
  disk; SMAPI itself has no cycle guard, so parity means no guard either, but
  flag it as a robustness decision for #33). Do NOT add link-specific
  ignore/display rules SMAPI lacks.
- Whole-`Mods`-path redirection is a separate, supported feature: `--mods-path`
  / `SMAPI_MODS_PATH` selects the active mods folder; bundled-mod identity is
  by manifest `UniqueID`, not path.

## 6. `.xnb` interplay (verified)

- Strict XNB extensions (case-insensitive): `.xgs .xnb .xsb .xwb`, plus
  `.json .yaml` allowed alongside (`PotentialXnbModExtensions`).
- `IsXnbMod` (~lines 334–355): after filtering to relevant files, true iff ≥1
  strict-XNB file exists AND every other relevant file is `.json`/`.yaml`.
  The check runs on the **recursively** collected files of a manifest-less leaf
  (`RecursivelyGetFiles`, which skips irrelevant subdirs).
- A manifest-less leaf that is XNB-shaped → `Type = ModType.Xnb`,
  `ModParseError.XnbMod` ("it's not a SMAPI mod (see https://smapi.io/xnb for
  info)". `ModResolver` maps it to `Failed / XnbMod`. XNB units are reported,
  never loaded.
- Because `manifest.json` is found FIRST in `ReadFolder`, **a folder containing
  both `manifest.json` and `.xnb` files is a normal SMAPI/content-pack unit,
  not an XNB mod** — manifest presence always wins.
- Consolidation (§3b) can promote an organizer to `Xnb` when all its children
  are XNB/empty: split XNB content across sibling subfolders still reports one
  XNB record at the parent.
- **Scanner implication:** XNB detection must run on the same
  relevant-files-only, recursive basis; `.png`-beside-`.xnb` is NOT XNB-shaped
  (a `.png` is ignored → filtered out, so it doesn't disqualify; but a `.dll`
  or `.tbin` alongside DOES disqualify → falls through to `ManifestMissing`).
  Careful: ignored-extension files are filtered before the test, so they
  neither prove nor disprove XNB-ness.

## 7. Case-insensitivity flag (verified)

- `useCaseInsensitiveFilePaths` threads
  `SCore(Settings.UseCaseInsensitivePaths)` → `ReadManifests` → `GetModFolders`
  → `FindManifest`, where it enables a case-insensitive `manifest.json` lookup.
  `SConfig.UseCaseInsensitivePaths` defaults true on Android/Linux, false
  elsewhere (`src/SMAPI/Framework/Models/SConfig.Defaults`, line ~31).
- The same setting picks `CaseInsensitiveFileLookup` vs `MinimalFileLookup`
  for later `EntryDll`/content-file validation (`SCore.GetFileLookup`).
- **Scanner implication:** the scanner seam needs the same flag: with it off on
  Linux, `MANIFEST.JSON` does not identify a unit.

## 8. What this means for scanner-seam ticket #33

1. Replicate `GetModFolders` traversal exactly: root-always-descends,
   dot-prefix → `Ignored` record + prune, `IsRelevant`-fail → silent prune,
   `IsModSearchFolder` file/subfolder test, `TryConsolidate` merge.
2. Replicate `IsRelevant` tables verbatim (regexes + extension set above) —
   they decide `EmptyFolder` vs `ManifestMissing` vs `XnbMod` outcomes.
3. Nested units at any depth are live; organizer folders with mixed
   files+subfolders poison their whole subtree into one `ManifestMissing`.
4. Root loose files → ignored/loose report, never a unit.
5. Symlinks: follow transparently (no special rules) — `[INFERENCE]`.
6. Manifest presence beats XNB detection; XNB test is relevant-files-only and
   recursive.
7. Every `ModFolder` outcome the scanner can emit: `Smapi`, `ContentPack`,
   `Invalid` + (`EmptyFolder` | `EmptyVortexFolder` | `ManifestInvalid` |
   `ManifestMissing`), `Xnb` + `XnbMod`, `Ignored` + `IgnoredFolder`. Plus the
   SMAPI-installer special case (`install on Linux.sh` / `install on
   macOS.command` / `install on Windows.bat` → `Invalid/ManifestMissing` with
   the "delete after installing" text) — worth mirroring so users don't try to
   "fix" the installer bundle as a broken mod.
8. Open edge for #33 to decide (not SMAPI-determined): traversal-cycle guard
   for link loops; whether `Ignored` records enter the filesystem cache or are
   re-derived each scan.

## Sources

Primary (SMAPI `develop`, fetched live):

- `src/SMAPI.Toolkit/Framework/ModScanning/ModScanner.cs` (ignore tables,
  `GetModFolders`, `IsModSearchFolder`, `TryConsolidate`, `FindManifest`,
  `IsRelevant`, `IsXnbMod`, `IsEmptyVortexFolder`, `RecursivelyGetFiles`)
- `src/SMAPI.Toolkit/Framework/ModScanning/ModFolder.cs` (`ModFolder` shape:
  `DirectoryPath`, `Type`, `Manifest`, `ManifestParseError(Text)`)
- `src/SMAPI.Toolkit/Framework/ModScanning/ModType.cs`
  (`Invalid | Ignored | Smapi | ContentPack | Xnb`)
- `src/SMAPI.Toolkit/Framework/ModScanning/ModParseError.cs`
  (`None | EmptyFolder | EmptyVortexFolder | IgnoredFolder | ManifestInvalid |
  ManifestMissing | XnbMod`)
- `src/SMAPI/Framework/ModLoading/ModResolver.cs` (`ReadManifests`: `Ignored →
  DisabledByDotConvention`; `EmptyFolder/EmptyVortexFolder → EmptyFolder`;
  `XnbMod → XnbMod`; else `InvalidManifest`)
- `src/SMAPI/Framework/SCore.cs` (loose-files error at `Mods` root; ignored-mod
  "folder name starts with a dot" log; `ReadManifests` call site;
  `GetFileLookup` case-insensitivity)
- `src/SMAPI/Framework/Models/SConfig.cs` (`UseCaseInsensitivePaths` default:
  true on Android/Linux)
- `src/SMAPI.Toolkit/ModToolkit.cs` (`GetModFolders` delegation)
- Wiki `Modding:Modder_Guide/APIs/Manifest` (every mod/content pack must have
  `manifest.json` in its folder) and `Modding:Modder_Guide/APIs/Mod_structure`
  (DLL + manifest unit shape)

`[INFERENCE]` marks §5 (symlinks): grounded in the absence of link handling in
SMAPI source plus .NET `DirectoryInfo` follow-by-default semantics, not in a
SMAPI statement. Everything else was read directly in the cited source.
