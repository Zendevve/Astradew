# Assembly version read feasibility (Phase 1 detector research)

Research-only notes for the SMAPI detector. Question (issue #20): does Go stdlib
`debug/pe` reliably yield the SMAPI assembly/semantic version from
`StardewModdingAPI.dll`, which exact PE/CLR field carries the `4.x.y` value, and
which on-disk file/field carries the GAME version. Empirical part verified
against a real install on this machine (`D:\Games\Stardew Valley`, SMAPI 4.5.2,
game 1.6.15 build 24356) — no `[UNVERIFIED-EMPIRICAL]` items.

Decisions this builds on: #17 (nullable versions = unknown, precedence
assembly → manifests → log-header) and #18 (best-effort `debug/pe` with
`""`-on-failure; `Versions` struct with `Assembly`/`Manifest`/`LastRun`/
`Resolved`/`Conflict`).

## 1. Three different "versions" live in one binary — do not conflate them

A .NET SDK-style build from `<Version>4.5.2</Version>` (SMAPI's
`build/common.targets`) generates three distinct version records:

| # | Name | Carries for SMAPI 4.5.2 | Lives in | Loses prerelease suffix? |
|---|------|------------------------|----------|--------------------------|
| A | Assembly Version (`AssemblyName.Version`) | `4.5.2.0` (4-part padded) | CLI metadata, `Assembly` table `MajorVersion/MinorVersion/BuildNumber/RevisionNumber` columns (ECMA-335) | YES — `AssemblyVersion` defaults to `$(Version)` **without the suffix** (`1.2.3-beta.4` → `1.2.3`) |
| B | File version (`FileVersion` / `VS_FIXEDFILEINFO.dwFileVersion*`) | `4.5.2.0` | Win32 version resource (`VS_VERSIONINFO`): binary `FILEVERSION` + `StringFileInfo` `"FileVersion"` string | YES — same suffix-stripping rule as AssemblyVersion |
| C | Product / informational version (`ProductVersion` / `AssemblyInformationalVersion`) | `4.5.2+821167e5c511bf3a2d98f604e5e838561c469219` | Win32 version resource: `PRODUCTVERSION` + `StringFileInfo` `"ProductVersion"` string; CLI custom attribute `AssemblyInformationalVersionAttribute` | NO — `InformationalVersion` defaults to the full `$(Version)`, prerelease suffix and all |

Consequence for the detector: field C (`"ProductVersion"` string) is the ONLY
PE field holding the full semantic version. Fields A/B give `major.minor.patch`
only. For release builds (`4.5.2`) A/B/C agree after padding; for dev builds
(`4.0.0-alpha.<date>`) A/B read `4.0.0.0` while only C keeps the `-alpha` tag.
SMAPI's own startup self-check (`Program.AssertSmapiVersions`) compares
`AssemblyName.Version` major.minor.patch and explicitly notes "we can't get
[the prerelease suffix] from the assembly versions" — the same limitation
applies to any A/B-based read.

## 2. What Go stdlib `debug/pe` can and cannot reach (no third-party deps)

Verified against local Go 1.25.14 (`go doc debug/pe`): `pe.File` exposes COFF
headers, the 16 `DataDirectory` entries, `Sections`, and raw section bytes via
`Section.Data()`. It has NO version-resource parser, NO resource-directory-tree
parser, and NO CLI-metadata parser. `debug/gosym` is Go-binary symbol data and
irrelevant here; `debug/buildinfo` reads Go build stamps only, not .NET
assemblies.

| Field | Reachable via stdlib alone? | How |
|-------|-----------------------------|-----|
| `"FileVersion"` / `"ProductVersion"` strings (`StringFileInfo`) | YES, best-effort (manual parse) | `.rsrc` section bytes via `Section(".rsrc").Data()`, then hand-rolled UTF-16LE scan for the `VS_VERSIONINFO` string table. Demonstrated working by the throwaway probe (§4). No stdlib helper — ~40 lines of `encoding/binary` + `unicode/utf16`. |
| `VS_FIXEDFILEINFO.dwFileVersionMS/LS` | YES, best-effort (manual parse) | Same `.rsrc` bytes; scan for the `0xFEEF04BD` signature, read `dwFileVersionMS` at +8 and `dwFileVersionLS` at +12 (`MS=HI/LO major/minor`, `LS=HI/LO build/revision`). Demonstrated working (§4). Redundant with the `"FileVersion"` string; prefer the string. |
| Assembly Version A (CLI `Assembly` table) | NOT reachable via stdlib — needs manual ECMA-335 parsing | `debug/pe` stops at raw section bytes. Reaching the `Assembly` table means hand-parsing the COM-descriptor CLI header (`DataDirectory[14]`) → metadata root → stream headers (`#~`, `#Strings`) → metadata tables. Possible with stdlib `encoding/binary` alone, but it is a hand-rolled CLI parser (fragile across heap-index sizes), not a stdlib feature. Not recommended; use field C instead. |
| `AssemblyInformationalVersionAttribute` (CLI custom-attribute blob) | Same as A — NOT practically reachable | Same metadata-table walk plus blob decoding. The identical string already sits in `"ProductVersion"`, so never parse the blob. |
| Unix extensionless `StardewModdingAPI` apphost binary | NOT a PE file at all | On Linux/macOS the extensionless launcher is a native apphost (ELF/Mach-O), not PE — `debug/pe` cannot open it. Irrelevant in practice: the sibling `StardewModdingAPI.dll` (managed assemblies are always PE-format, every OS) sits beside it with identical version fields. Read the `.dll`. |

Two stdlib caveats worth recording: (1) `debug/pe` is documented as not
hardened against adversarial inputs — fine for best-effort reads of local game
files, but failures MUST degrade to `""`, never propagate as detection errors
(per #18). (2) Even locating `.rsrc` *precisely* (resource directory tree,
RVA→file-offset mapping) is manual work; the probe's pragmatic approach —
`Section.Data()` + signature/string scan — is sufficient precisely because the
read is best-effort with `""`-on-failure.

## 3. GAME version source: `Stardew Valley.dll`, same field mechanics

The game version is read by SMAPI itself as `Game1.version`
(`Constants.GameVersion = new GameVersion(Game1.version)`, surfaced in the log
header via `Game1.GetVersionString()`). On disk the carrier is
**`Stardew Valley.dll`** (the validity-rule file from smapi-layout §1.1), whose
PE fields mirror the SMAPI layout exactly:

- Observed: `FileVersion = 1.6.15.24356`, `AssemblyName.Version = 1.6.15.24356`
  (here FileVersion == AssemblyVersion — the game stamps all four parts).
- `Stardew Valley.exe` (native apphost: `AssemblyName.GetAssemblyName` fails on
  it, COM descriptor zero) embeds an identical copy of the same version
  resource, so either file works; prefer the `.dll` (it is also the managed
  assembly, present on every OS).
- Game `ProductVersion` string is oddly formatted (`1.6.15, , 24356, `), so for
  the GAME the recommendation is inverted vs SMAPI: prefer the `FileVersion`
  string / `AssemblyName.Version`, not `ProductVersion`.

Legacy-branch note (smapi-layout §4.2–4.3): pre-1.5.5 games have no
`Stardew Valley.dll`; the version then lives in the old exe's PE fields only.
That path inherits the same best-effort `""`-on-failure rule.

## 4. Empirical results (this machine, verified — NOT unverified)

Throwaway probe (`go run` reading real files; .rsrc scan + FixedFileInfo
signature + COM-descriptor check), cross-checked with
`FileVersionInfo` + `AssemblyName.GetAssemblyName` ground truth:

| File | `.rsrc` | COM descriptor [14] | `dwFileVersionMS/LS` | `"FileVersion"` | `"ProductVersion"` | Ground truth |
|------|---------|---------------------|----------------------|-----------------|--------------------|--------------|
| `StardewModdingAPI.dll` (1.0 MB) | present (17408 B) | nonzero (managed CLI assembly) | `4.5.2.0` | `4.5.2.0` | `4.5.2+821167e...` | AssemblyName `4.5.2.0` ✔ |
| `StardewModdingAPI.exe` (163 KB) | present (17408 B) | **zero** (native apphost, not managed) | `4.5.2.0` | `4.5.2.0` | `4.5.2+821167e...` (identical copy) | `GetAssemblyName` fails (no manifest) ✔ as predicted |
| `smapi-internal/SMAPI.Toolkit.dll` | present | nonzero | `4.5.2.0` | `4.5.2.0` | `4.5.2+821167e...` | AssemblyName `4.5.2.0` ✔ |
| `Stardew Valley.dll` | present (8704 B) | nonzero | `1.6.15.24356` | `1.6.15.24356` | `1.6.15, , 24356, ` | AssemblyName `1.6.15.24356` ✔ |
| `Stardew Valley.exe` | present | zero (apphost) | `1.6.15.24356` | `1.6.15.24356` | `1.6.15, , 24356, ` (identical copy) | `GetAssemblyName` fails ✔ as predicted |
| Bundled manifests | — | — | — | `Version = MinimumApiVersion = 4.5.2`, UniqueIDs `SMAPI.ConsoleCommands` / `SMAPI.SaveBackup` | — | agree with assembly fields ✔ |

Key empirical confirmations: (a) the `.exe` files are native apphosts carrying
byte-identical version resources to their `.dll`s — reading the `.dll` loses
nothing; (b) all SMAPI components stamp the same version (consistent with the
startup self-check); (c) assembly source and both manifests agree at 4.5.2 on a
healthy install, so the #17 precedence chain resolves cleanly in the common
case.

## 5. Verdict table + precedence recommendation

| Source | Verdict | Recommended field / file |
|--------|---------|--------------------------|
| SMAPI assembly version | **Best-effort, reachable** (StringFileInfo scan of `.rsrc`; `""`-on-failure) | `"ProductVersion"` string of `StardewModdingAPI.dll` (full semver incl. prerelease); fall back to `"FileVersion"` (major.minor.patch). |
| SMAPI CLI `Assembly`-table Version | **Not reachable via stdlib** (needs hand-rolled ECMA-335 parser or third-party dep) — do not attempt | — (covered by the string above) |
| `VS_FIXEDFILEINFO` binary fields | **Reachable but redundant** — same info as `"FileVersion"` string | Parse only as cross-check, or not at all |
| Unix apphost binary | **Not reachable via `debug/pe`** (not PE) — read the sibling `.dll` instead | `StardewModdingAPI.dll` on all OSes |
| GAME version | **Best-effort, reachable** | `"FileVersion"` string of `Stardew Valley.dll` (== its Assembly Version here); `ProductVersion` is malformed for the game, avoid it |
| Bundled manifests / log header | Unchanged (smapi-layout §3.2–3.3, #17/#18) | — |

**Precedence update to smapi-layout §3: none needed.** The assembly → manifests
→ log-header order stands; this ticket only sharpens what "assembly" means in
row 1: read the **`"ProductVersion"` version-resource string** (fallback
`"FileVersion"`), which resolves the old `[UNVERIFIED]` PE-field note in §3 row
1 — the mapping is now verified file-by-file (§4). Everything else (#18's
best-effort `debug/pe`, `""`-on-failure, manifests as the practical source,
`NULL` = unknown per #17) is confirmed, not changed.

## Sources

- https://www.ecma-international.org/publications-and-standards/standards/ecma-335/ (ECMA-335 CLI: Assembly table MajorVersion/MinorVersion/BuildNumber/RevisionNumber; managed assemblies use the PE file format)
- https://learn.microsoft.com/en-us/windows/win32/debug/pe-format (PE/COFF spec: `.rsrc` section, COM-descriptor directory entry 14 = CLI header, VS_FIXEDFILEINFO layout)
- https://learn.microsoft.com/en-us/windows/win32/menurc/versioninfo-resource (VERSIONINFO: FILEVERSION/PRODUCTVERSION binary fields, StringFileInfo FileVersion/ProductVersion strings)
- https://learn.microsoft.com/en-us/dotnet/standard/assembly/versioning (assembly 4-part identity version vs informational version shown as "Product Version" in file properties)
- https://learn.microsoft.com/en-us/dotnet/standard/assembly/set-attributes-project-file (Version→AssemblyVersion/FileVersion suffix-stripping; InformationalVersion keeps full Version)
- https://learn.microsoft.com/en-us/dotnet/api/system.diagnostics.fileversioninfo.fileversion (FileVersionInfo 4×16-bit composition)
- https://pkg.go.dev/debug/pe (Go stdlib: COFF/sections/DataDirectory/Section.Data only — no resource or CLI-metadata parsers) + local `go doc debug/pe` (Go 1.25.14) confirming no resource-tree API
- https://github.com/Pathoschild/SMAPI/blob/develop/build/common.targets (`<Version>4.5.2</Version>` single build version source)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI/SMAPI.csproj (`AssemblyName StardewModdingAPI`, net6.0 apphost build)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI/Constants.cs (`EarlyConstants.RawApiVersion = "4.5.2"`)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI/Program.cs (`AssertSmapiVersions`: assembly-version self-check, prerelease suffix unavailable from assembly versions; exe-is-apphost evidence)
- https://github.com/Pathoschild/SMAPI/blob/develop/src/SMAPI/Framework/Logging/LogManager.cs (`LogIntro`: `SMAPI {ApiVersion} with Stardew Valley {Game1.GetVersionString()} on …`)
- Empirical: local install `D:\Games\Stardew Valley` (SMAPI 4.5.2+821167e, game 1.6.15.24356), throwaway `go run` PE probe + `FileVersionInfo`/`AssemblyName` ground-truth cross-check (probe deleted after use per throwaway rule)
