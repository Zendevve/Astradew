# Manifest corpus and malformation survey (Phase 2 wayfinding research)

Research-only notes for the SMAPI manifest parser. Question (issue #30, child of map #29):
what real-world `manifest.json` shapes must the resilient parser accept?

Ticket: Zendevve/Astradew#30. Blocks the parser-contract ticket #32.
Branch: `research/manifest-corpus`. Do NOT close #30 from here; charting session owns structure.

## Sources (all primary)

SMAPI `develop` branch source, read verbatim:

- `src/SMAPI.Toolkit/Serialization/Models/Manifest.cs` — the manifest model, `[JsonConstructor]`, `NormalizeField`, `[JsonExtensionData] ExtraFields`.
- `src/SMAPI.Toolkit/Serialization/Models/ManifestContentPackFor.cs` — `{ UniqueID, MinimumVersion? }`.
- `src/SMAPI.Toolkit/Serialization/Models/ManifestDependency.cs` — `{ UniqueID, MinimumVersion?, IsRequired = true }`.
- `src/SMAPI.Toolkit/Serialization/Converters/ManifestContentPackForConverter.cs` — object-only deserialization.
- `src/SMAPI.Toolkit/Serialization/Converters/ManifestDependencyArrayConverter.cs` — case-insensitive dependency parsing.
- `src/SMAPI.Toolkit/Serialization/Converters/SemanticVersionConverter.cs` — string-or-object version parsing.
- `src/SMAPI.Toolkit/Serialization/JsonHelper.cs` — Newtonsoft settings, curly-quote retry.
- `src/SMAPI.Toolkit/Serialization/InternalExtensions.cs` — `ValueIgnoreCase<T>` helper.
- `src/SMAPI.Toolkit/Framework/ManifestValidator.cs` — `TryValidateFields` with exact error strings.
- `src/SMAPI.Toolkit/Framework/ModScanning/ModScanner.cs` — folder scan, parse-error mapping, mod-type classification.
- `src/SMAPI.Toolkit/Framework/ModScanning/ModFolder.cs` — `GetUpdateKeys` whitespace filter.
- `src/SMAPI.Toolkit/Framework/ModScanning/ModParseError.cs` — parse-error enum.
- `src/SMAPI.Toolkit/Framework/UpdateData/UpdateKey.cs` and `ModSiteKey.cs` — update-key grammar.
- `src/SMAPI/Framework/ModLoading/ModResolver.cs` (lines 1–300 read) — validation pipeline order.
- `src/SMAPI.Toolkit/Utilities/PathUtilities.cs` — `IsSlug` rule.
- JSON schema: `https://smapi.io/schemas/manifest.json` (editor/CI validation; NOT the runtime).
- Docs: `Modding:Modder Guide/APIs/Manifest` and `.../APIs/Update checks` (Stardew Valley Wiki).

Real-world manifests read verbatim:

- `Pathoschild/StardewMods` `LookupAnything/manifest.json` (canonical code mod).
- `Pathoschild/StardewMods` `ContentPatcher/manifest.json` (canonical framework; `%ProjectVersion%`).
- `FlashShifter/StardewValleyExpanded` `[CP] Stardew Valley Expanded/manifest.json` (large content pack, mixed deps, `Nexus:???` placeholder).

## 1. Canonical shapes

Code mod (Lookup Anything, abridged — full file has only these keys):

```json
{
    "Name": "Lookup Anything",
    "Author": "Pathoschild",
    "Version": "%ProjectVersion%",
    "MinimumApiVersion": "4.3.1",
    "MinimumGameVersion": "1.6.15",
    "Description": "View metadata about anything by pressing a button.",
    "UniqueID": "Pathoschild.LookupAnything",
    "EntryDll": "LookupAnything.dll",
    "UpdateKeys": [ "Nexus:541" ]
}
```

Content pack (Stardew Valley Expanded, abridged):

```json
{
    "Name": "Stardew Valley Expanded",
    "Author": "FlashShifter",
    "Version": "1.15.11",
    "Description": "An expansive fanmade mod for ConcernedApe's Stardew Valley.",
    "UniqueID": "FlashShifter.StardewValleyExpandedCP",
    "MinimumApiVersion": "4.1.7",
    "UpdateKeys": [ "Nexus:???" ],
    "ContentPackFor": { "UniqueID": "Pathoschild.ContentPatcher" },
    "Dependencies": [
        { "UniqueID": "FlashShifter.SVE-FTM", "IsRequired": true },
        { "UniqueID": "MoreFish", "IsRequired": false }
    ]
}
```

Rule: exactly one of `EntryDll` / `ContentPackFor`. Both or neither ⇒ invalid (see §8).

## 2. Required vs optional — schema and runtime disagree

| Field | Schema (`smapi.io`) | Runtime validator (`ManifestValidator`) | Verdict for parser |
|---|---|---|---|
| `Name` | required | required (blank ⇒ missing) | required |
| `Author` | required | **not checked** | required-by-schema, tolerated-by-runtime |
| `Version` | required | required; `null` **or `"0.0.0"`** ⇒ missing | required; treat `0.0.0` as missing |
| `Description` | required | **not checked** | required-by-schema, tolerated-by-runtime |
| `UniqueID` | required | required (blank ⇒ missing) + slug check | required |
| `EntryDll` / `ContentPackFor` | `oneOf` (exactly one) | exactly one non-blank; mutual exclusion | exactly one |
| `MinimumApiVersion` | optional | optional (gate only, §8) | optional |
| `MinimumGameVersion` | optional | optional (gate only) | optional |
| `Dependencies` | optional | optional; each entry checked if present | optional, default `[]` |
| `UpdateKeys` | optional | optional; blank entries filtered downstream | optional, default `[]` |
| `ContentPackFor.MinimumVersion` | optional | optional | optional |
| `Dependencies[].MinimumVersion` | optional | optional | optional |
| `Dependencies[].IsRequired` | optional | n/a (parsed, default `true`) | optional, default `true` |

Notes:

- The schema requires `Author` and `Description`, but `TryValidateFields` only flags `Name`, `Version`, `UniqueID`. A manifest missing `Author`/`Description` deserializes fine (`null` → normalized to `null`) and passes runtime validation. The resilient parser MUST accept those as absent (SMAPI itself loads such mods).
- `Version: "0.0.0"` is explicitly treated as missing: `manifest.Version == null || manifest.Version.ToString() == "0.0.0"`. Quasi-empty versions are a real malformation class.
- Missing `Dependencies`/`UpdateKeys` (JSON `null` or absent) become `[]` in the `[JsonConstructor]` (`dependencies ?? []`, `updateKeys ?? []`). Missing `MinimumApiVersion`/`MinimumGameVersion`/`EntryDll` become `null`.

## 3. `ContentPackFor`: string vs object

- **Current format is object-only**: `{ "UniqueID": "<host id>", "MinimumVersion": "1.0.0" }` (`MinimumVersion` optional). Source: `ManifestContentPackFor`, schema (`"type": "object"`, `required: ["UniqueID"]`).
- **The legacy string form (`"ContentPackFor": "Pathoschild.ContentPatcher"`) is dead at runtime.** `ManifestContentPackForConverter.ReadJson` calls `serializer.Deserialize<ManifestContentPackFor>(reader)` directly — a JSON string token cannot deserialize into that object type, so the whole manifest fails with `ManifestInvalid` ("parsing its manifest failed: …"). There is no string-fallback branch in the converter. (Confirmed by reading the converter; the legacy form survives only in old forum/wiki examples.)
- Parser-contract input for #32: SMAPI **rejects** the string form. Our parser must decide whether to match SMAPI (reject) or be lenient (coerce `string s` → `{ UniqueID: s }`). Recommendation: coerce-and-warn, since pre-2020 packs in the wild still carry the string form and the mapping is unambiguous. But record the deviation from SMAPI behavior explicitly.
- `ContentPackFor` with a blank/missing `UniqueID` fails validation with its own message (`manifest declares ContentPackFor without its required UniqueID field.`), distinct from the neither/both messages.

## 4. `Dependencies` entries

Shape per entry: `{ "UniqueID": "<id>" (required), "MinimumVersion": "<semver>" (optional), "IsRequired": <bool> (optional, default `true`) }`.

Parser-relevant behaviors from `ManifestDependencyArrayConverter`:

- Field lookup is **case-insensitive** (`ValueIgnoreCase<T>` over `JObject.GetValue(name, OrdinalIgnoreCase)`). `uniqueid`, `minimumversion`, `isrequired` all parse.
- `IsRequired` absent ⇒ `true`. Explicit `false` marks load-order-only/optional deps (common in real packs — SVE lists five optional IDs).
- `MinimumVersion` absent/blank ⇒ `null` (no floor). Present ⇒ parsed via `new SemanticVersion(string)`; an unparsable string throws ⇒ whole-manifest `ManifestInvalid`.
- A `null` `UniqueID` does **not** throw at parse time (`uniqueId!` with "will be validated separately" comment); it fails later in `TryValidateFields` (`manifest has a Dependencies entry with no UniqueID field.`). So missing dep IDs are field errors, not parse errors.
- Non-object items inside the array: `JArray.Load(reader).Children<JObject>()` filters to `JObject` children (Newtonsoft `Children<T>` = `OfType<T>` semantics), so a stray string/number entry is **silently dropped** [INFERENCE on Newtonsoft semantics — converter source is primary, flag for a unit test in #32]. A non-array `Dependencies` value (single object, string) throws at `JArray.Load` ⇒ whole-manifest `ManifestInvalid`.
- Dependency IDs get the same slug check as the mod ID (§8).

## 5. `UpdateKeys` host variants

Valid sites (enum `ModSiteKey` + wiki + schema regex):

| Key form | Example | Notes |
|---|---|---|
| `Nexus:<numeric id>` | `Nexus:541` | most common |
| `Chucklefish:<numeric id>` | `Chucklefish:4250` | deprecated site, still parsed |
| `CurseForge:<numeric id>` | `CurseForge:309243` | |
| `ModDrop:<numeric id>` | `ModDrop:123338` | |
| `GitHub:<user>/<repo>` | `GitHub:Pathoschild/LookupAnything` | no numeric constraint; monorepos unsupported |
| `UpdateManifest:<url>@<key>` | `UpdateManifest:https://example.org/mods.json@ExampleMod` | custom JSON manifest (wiki "Advanced"); `ModSiteKey.UpdateManifest` exists at runtime |
| `<any of the above>@<subkey>` | `Nexus:2400@GeodeCrusher` | disambiguates multi-mod pages; subkey keeps its `@`, matched against file title/description; ignored-if-no-match fallback |

Runtime grammar (`UpdateKey.Parse`): split on first `:` → site + id; split id on first `@` → id + subkey (delimiter kept in subkey, re-emitted by `GetString`). Site matched **case-insensitively** against the enum; unknown site or blank id ⇒ `LooksValid == false`. Everything is trimmed. Invalid keys are **preserved verbatim** (`RawText`) and compare by raw text — i.e. SMAPI never throws on a bad update key; `GetUpdateUrl` returns `null` and update checks skip it.

**Schema/runtime gap**: the published schema regex only allows the five named sites (`Chucklefish|Nexus|GitHub|ModDrop|CurseForge` + optional `@subkey`) — `UpdateManifest:` keys **fail schema validation** but **parse fine at runtime** (enum member + `VendorModUrls` miss ⇒ `GetUpdateUrl` returns `null`, checks defer to the remote API). The resilient parser MUST accept `UpdateManifest:` keys despite the schema. Real-world evidence of loose keys: SVE ships `"UpdateKeys": [ "Nexus:???" ]` — parses (numeric check is schema-only), only fails to resolve.

`ModFolder.GetUpdateKeys` filters out blank/whitespace entries, so `["", "Nexus:541"]` ≡ `["Nexus:541"]` downstream.

## 6. Version parsing rules

- Accepted: semver-like strings (`1.0.0`, `1.0`, `1.0.0-beta.5`; schema regex requires `major.minor[.patch][-prerelease]`, no leading zeros) **or** a JSON object `{ MajorVersion, MinorVersion, PatchVersion, PrereleaseTag? }` (all case-insensitive) via `SemanticVersionConverter`.
- Empty/whitespace version string ⇒ `null` (→ "missing Version" validation error, not a parse error).
- Unparsable non-empty string ⇒ `SParseException` ("Can't parse semantic version from invalid value '…', should be formatted like 1.2, 1.2.30, or 1.2.30-beta (path: …)") ⇒ whole-manifest `ManifestInvalid`.
- Non-string/non-object version (number, bool, array) ⇒ `SParseException` ("Can't parse ISemanticVersion from … node") ⇒ `ManifestInvalid`.
- `%ProjectVersion%` is a **build-time placeholder** substituted by SMAPI.ModBuildConfig (present in source trees, e.g. ContentPatcher's shipped manifest source; schema allows it as a string const). A parser that reads unbuilt working copies WILL meet this literal; it is not parseable semver. #32 must decide: preserve-as-opaque vs reject. Recommendation: accept as an "unresolved" marker, never treat as `0.0.0`.

## 7. Parser-tolerance behaviors SMAPI guarantees

These are load-bearing for the resilient parser — all confirmed in source:

1. **Unknown fields are preserved, not rejected.** `[JsonExtensionData] ExtraFields` collects every unrecognized property; docs state they are "stored in the `IManifest.ExtraFields` dictionary … ignored by SMAPI, but may be useful for extended metadata". The schema's `"additionalProperties": false` is editor/CI-only. Parser MUST round-trip unknown fields.
2. **Field names are case-insensitive**, twice over: Newtonsoft's default contract matching plus `ValueIgnoreCase` in the hand-written converters (`Dependencies`, versions-as-objects). `uniqueid`/`entrydll`/`contentpackfor` parse.
3. **String normalization** (`Manifest.NormalizeField`): trim; `\r`/`\n` → space; additionally in `Name` only, `[`→`(`, `]`→`)` (log-format safety). `EntryDll`/`UniqueID`/etc. are trimmed; `ContentPackFor.UniqueID` and dependency IDs go through `NormalizeWhitespace` (trim). Whitespace-only ⇒ treated as missing.
4. **JSON-layer leniency** (`JsonHelper`): Newtonsoft-based; on `JsonReaderException` it retries with curly quotes (`“”` → `"`), else rethrows with a hint ("This doesn't seem to be valid JSON." + curly-quote diagnosis). Trailing-comma/comment tolerance is schema-advertised (`allowComments`, `allowTrailingCommas`) for editors; runtime tolerance was not verified against `JsonSettings` — open item for #32, do not assume.
5. **Manifest file lookup is case-sensitive by default** (`manifest.json` exact; case-insensitive only when `useCaseInsensitiveFilePaths`, i.e. Linux builds). A folder without a readable manifest is `ManifestMissing`, not a parse error.
6. **Dot-folders are ignored by convention** (`ModType.Ignored`); empty folders, Vortex empties, and XNB mods each get distinct `ModParseError` values — the parser only owns the `ManifestInvalid` slice.

## 8. Observed malformation catalog

Two severities, in pipeline order (`ModScanner.ReadFolder` → `ModResolver.ValidateManifests`):

**A. Parse-time — whole manifest unreadable (`ModParseError.ManifestInvalid`, "parsing its manifest failed: …"):**

| Malformation | Mechanism |
|---|---|
| Not JSON at all | `JsonReaderException` (with curly-quote hint) |
| `ContentPackFor` as string | converter type mismatch (legacy form, §3) |
| `Dependencies` not an array | `JArray.Load` throws |
| Bad `Version` / `MinimumApiVersion` / `MinimumGameVersion` / `MinimumVersion` string | `SParseException` from version converter/ctor |
| Version as number/bool/array | `SParseException` (wrong node type) |
| Wrong-typed scalar elsewhere (e.g. `UpdateKeys` as string, `Name` as object) | Newtonsoft deserialization throws |
| Missing file / unreadable file | `ManifestMissing` (distinct; "it contains files, but none of them are manifest.json.") |

**B. Validation-time — manifest parses, mod fails (`TryValidateFields` / resolver, exact strings):**

| Malformation | Exact error |
|---|---|
| Both `EntryDll` and `ContentPackFor` | `manifest sets both EntryDll and ContentPackFor, which are mutually exclusive.` |
| Neither present/blank | `manifest has no EntryDll or ContentPackFor field; must specify one.` |
| `EntryDll` with path chars | `manifest has invalid filename '…' for the EntryDll field.` (schema pins this tighter: `^[a-zA-Z0-9_.-]+\.dll$`) |
| `ContentPackFor` without `UniqueID` | `manifest declares ContentPackFor without its required UniqueID field.` |
| Missing `Name` / `Version` (incl. `0.0.0`) / `UniqueID` | `manifest is missing required fields (…).` |
| Non-slug `UniqueID` | `manifest specifies an invalid ID (IDs must only contain letters, numbers, underscores, periods, or hyphens).` (≙ `IsSlug`: Unicode letters + digits + `_` `.` `-`) |
| `Dependencies` null entry / missing ID / non-slug ID | `manifest has a null entry under Dependencies.` / `… entry with no UniqueID field.` / `… entry with an invalid UniqueID field (…slugs…).` |
| Referenced `EntryDll` absent on disk | resolver file-existence check (message form: `its DLL '…' doesn't exist.`) |
| Duplicate `UniqueID` (case-insensitive) across folders | `Duplicate` fail ("you have multiple copies of this mod …") |
| `MinimumApiVersion`/`MinimumGameVersion` newer than current | `Incompatible` fail ("it needs SMAPI … or later …" / "it needs Stardew Valley … or later …") — a gate, not a malformation; parser surfaces, never rejects |

Malformations SMAPI does **not** catch (parser must still represent faithfully): missing `Author`/`Description`; placeholder update keys (`Nexus:???`); unknown extra fields; `%ProjectVersion%` in unbuilt sources.

## 9. Unknown-field preservation expectations

- Runtime keeps every unknown property in `ExtraFields` (`IDictionary<string, object>`, JToken-backed via `[JsonExtensionData]`) and exposes it to other mods through the mod registry (`IManifest.ExtraFields`). Nothing is dropped, nothing warns.
- Therefore the resilient parser MUST: (a) accept any unknown property at any level without error; (b) preserve values verbatim for round-trip; (c) expose them to callers (Astradew-side equivalent of `ExtraFields`). Losing them would silently strip cross-mod metadata.
- Caveat: `ExtraFields` values are Newtonsoft `JToken`s; `JsonHelper.ConvertToSystemTextJsonNode` exists for bridging to `System.Text.Json`. Our Go parser has no such bridge — define the preservation type in #32 (raw bytes vs decoded generic values).

## 10. Sharp inputs for the parser-contract ticket (#32)

1. Accept table = §2 "Verdict" column; `Author`/`Description` optional-in-practice despite schema-required.
2. `EntryDll` pattern: enforce schema regex (`^[a-zA-Z0-9_.-]+\.dll$`) or runtime rule (no `Path.GetInvalidFileNameChars`)? They differ (e.g. `My Mod.dll` with a space passes runtime, fails schema). Recommend runtime rule + schema-level warning.
3. `ContentPackFor` string form: coerce-and-warn (deviation from SMAPI — record it).
4. `UpdateManifest:` keys: accept (schema is wrong here, runtime is right).
5. `%ProjectVersion%`: accept as unresolved-version marker.
6. `0.0.0` ≡ missing `Version`.
7. Unknown fields: preserve verbatim, expose, round-trip.
8. Case-insensitive field names; trim + newline-fold strings; `Name` bracket rewrite (mirror or deliberately drop? — display-only; recommend mirror for log parity).
9. Error taxonomy mirrors SMAPI's two tiers: parse errors (whole file) vs field errors (exact-string compatible where feasible).
10. Verify-before-close: `Children<JObject>()` drop semantics for non-object dependency entries; runtime trailing-comma/comment tolerance of `JsonSettings`; Newtonsoft property-name case-insensitivity as relied upon in §7.2.
