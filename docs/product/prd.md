# ASTRADEW
## Product Requirements Document + Technical Specification + Implementation Prompt

**Product name:** Astradew
**Application type:** Native desktop Stardew Valley mod manager
**Primary technology:** Wails v3 + Go + React + TypeScript
**Primary mod loader:** SMAPI
**Primary target:** Stardew Valley 1.6+
**Platforms:** Windows, macOS, Linux, Steam Deck
**Architecture:** Local-first, offline-capable, transactional, non-destructive

---

# 1. PRODUCT MISSION

Build a dedicated Stardew Valley mod manager that makes installing, maintaining, diagnosing, grouping, updating, and launching SMAPI mod configurations substantially easier without hiding what is happening to the player's files.

The application must not behave like a generic game mod manager with Stardew Valley bolted onto it.

It must understand Stardew-specific concepts:

- SMAPI mods;
- content packs;
- `manifest.json`;
- `UniqueID`;
- `Dependencies`;
- `ContentPackFor`;
- `MinimumApiVersion`;
- `MinimumGameVersion`;
- `UpdateKeys`;
- SMAPI's ignored-folder behavior;
- alternate `--mods-path`;
- SMAPI bundled mods;
- profile-specific mod configuration;
- SMAPI logs;
- Nexus mod IDs and file IDs;
- Stardew mod packages containing multiple independently loadable mods.

The design priority is:

**reliability > reversibility > clarity > speed > convenience > cleverness.**

A manager that can install 300 mods but occasionally eats someone's configuration is a bad manager.

---

# 2. PRODUCT POSITIONING

Astradew should sit between manual SMAPI modding and large generic managers such as Vortex.

It should improve on existing Stardew-specific managers by focusing on:

1. transactional mod installation and updating;
2. transparent dependency health;
3. strong recovery and rollback;
4. isolated profiles;
5. robust handling of malformed or unusual archives;
6. reliable mod provenance/matching;
7. fast management of hundreds or thousands of mods;
8. first-class SMAPI diagnostics;
9. direct understanding of mod packages instead of simple folder-moving;
10. a modern native-feeling desktop interface.

Do not attempt to become a social platform, mod hosting service, community browser, AI assistant, or replacement for Nexus Mods.

---

# 3. PRODUCT PRINCIPLES

## 3.1 Local ownership

All primary state must exist locally.

No Astradew account is required.

The application must remain useful without an Internet connection.

Internet connectivity enhances:

- update checks;
- Nexus metadata;
- Nexus downloads where permitted;
- GitHub update checks;
- mod dataset refreshes;
- application update checks.

It must not be required to:

- launch Stardew Valley;
- switch profiles;
- install a local archive;
- enable or disable mods;
- edit configurations;
- inspect dependencies;
- inspect SMAPI logs.

---

## 3.2 Non-destructive operation

Never modify Stardew Valley's base `Content` files.

Do not manage legacy XNB replacements automatically.

Never silently overwrite user-modified files.

Never automatically rewrite Steam launch options.

Never delete an existing user `Mods` folder during onboarding.

Never overwrite a mod update without creating a recoverable transaction snapshot.

Every destructive operation must either:

- be reversible;
- move data to trash/history;
- or explicitly identify what will be permanently deleted.

---

## 3.3 SMAPI-native profiles

Use SMAPI's alternate mod path support rather than repeatedly renaming the global `Mods` folder.

Each profile receives its own physical mod directory.

Example:

```text
Astradew/
├── database/
│   └── astradew.sqlite
├── library/
│   ├── archives/
│   └── packages/
├── profiles/
│   ├── default/
│   │   └── Mods/
│   ├── multiplayer/
│   │   └── Mods/
│   └── expanded/
│       └── Mods/
├── backups/
├── cache/
├── logs/
└── temp/
```

Launch SMAPI using:

```text
StardewModdingAPI.exe --mods-path "<profile Mods directory>"
```

Use the platform-equivalent executable on macOS/Linux.

This makes profiles independent from the original game `Mods` directory.

---

# 4. TARGET USERS

## Primary

### Casual modded player

Uses 10–50 mods.

Needs:

- drag-and-drop installation;
- updates;
- simple enable/disable;
- automatic dependency warnings;
- safe launch.

### Heavy modded player

Uses 100–500+ mods.

Needs:

- search;
- bulk operations;
- profiles;
- filtering;
- update tracking;
- dependency diagnostics;
- fast startup;
- configuration isolation.

### Multiplayer player

Needs separate mod configurations for:

- single-player;
- specific multiplayer farms;
- vanilla-ish co-op;
- heavily modded saves.

### Mod tester/developer

Needs:

- rapid profile creation;
- duplicate detection;
- log access;
- version inspection;
- manual directories;
- local development mods.

---

# 5. OUT OF SCOPE

Do not implement for MVP:

- console modding;
- iOS modding;
- Android mod management;
- XNB content replacement;
- arbitrary DLL execution;
- arbitrary mod installers;
- FOMOD support;
- automatic modification of game binaries;
- automatic Steam launch-option rewriting;
- cloud synchronization;
- mod ratings/reviews;
- chat;
- social features;
- AI recommendations;
- built-in general-purpose Nexus browsing;
- load-order systems copied from Bethesda modding;
- fake conflict scoring.

Stardew SMAPI mod order and dependency semantics are not equivalent to Skyrim plugin load order.

Do not invent mechanics that Stardew does not have.

---

# 6. CORE TECHNOLOGY STACK

## Backend

Use:

```text
Go 1.25+
Wails v3.0.0-beta.25 pinned explicitly
SQLite
database/sql
modernc.org/sqlite
log/slog
fsnotify
net/http
os/exec
archive/zip
crypto/sha256
```

Additional libraries should be introduced only when materially justified.

Avoid CGO unless unavoidable.

---

## Frontend

Use:

```text
React
TypeScript
Vite
Tailwind CSS
Radix primitives or equivalent accessible headless components
TanStack Query
Zustand only for ephemeral UI state
TanStack Virtual for long mod lists
React Hook Form where forms are non-trivial
Zod for frontend input/schema validation
```

Backend state remains authoritative.

Do not replicate the entire application database into Zustand.

---

## Credentials

Nexus credentials must never be stored unencrypted in SQLite.

Use the operating-system credential store:

- Windows Credential Manager;
- macOS Keychain;
- Linux Secret Service/KWallet where available.

A Go keyring abstraction may be used.

If no secure Linux credential store is available:

- clearly report that state;
- allow session-only credentials;
- do not silently fall back to plaintext.

---

# 7. WAILS ARCHITECTURE

Organize backend logic into explicit services.

Suggested structure:

```text
/
├── cmd/
├── internal/
│   ├── app/
│   ├── database/
│   ├── game/
│   ├── smapi/
│   ├── mods/
│   │   ├── manifest/
│   │   ├── scanner/
│   │   ├── installer/
│   │   ├── deployment/
│   │   ├── dependency/
│   │   └── updates/
│   ├── profiles/
│   ├── nexus/
│   ├── dataset/
│   ├── diagnostics/
│   ├── downloads/
│   ├── archives/
│   ├── security/
│   ├── platform/
│   └── filesystem/
├── migrations/
├── frontend/
├── fixtures/
└── build/
```

Wails services:

```text
ApplicationService
GameService
LibraryService
InstallService
ProfileService
UpdateService
NexusService
LaunchService
DiagnosticsService
SettingsService
BackupService
```

Services may share internal domain components.

Do not put business logic directly inside Wails-bound methods.

---

# 8. APPLICATION INFORMATION ARCHITECTURE

Main navigation:

```text
Library
Profiles
Updates
Health
Downloads
Logs
Settings
```

Primary top bar:

```text
Current profile selector
Search
Update indicator
Launch button
```

Do not create twelve navigation sections because every feature felt lonely.

---

# 9. FIRST-RUN ONBOARDING

The first-run wizard must perform the following sequence.

## Step 1: Detect Stardew Valley

Search known installation locations for:

### Windows

- Steam libraries;
- GOG;
- Xbox app locations where feasible.

### macOS

- Steam;
- GOG;
- standard application location.

### Linux

- standard Steam libraries;
- alternate Steam paths;
- Flatpak Steam where feasible;
- Steam Deck locations.

Allow manual selection.

Store game installations separately from profiles.

---

## Step 2: Detect SMAPI

Look for the platform-appropriate SMAPI executable.

Detect:

- installed;
- missing;
- invalid path;
- version where possible.

Do not make SMAPI installation mandatory for simply opening Astradew.

Without SMAPI:

- local mod library operations remain usable;
- launch remains disabled;
- relevant health warnings are shown.

SMAPI installer automation can be added later.

---

## Step 3: Detect existing Mods directory

Scan but do not modify it.

Show:

```text
Found 84 valid SMAPI mods
3 content packs
2 invalid folders
1 legacy XNB mod
```

Offer:

```text
Import into a new "Imported" profile
Skip import
```

Import means COPY.

Do not move or delete the existing Mods folder.

---

## Step 4: Import SMAPI bundled mods

Detect:

- Console Commands;
- Save Backup.

Mark them as:

```text
Source: SMAPI
System mod: true
```

Default behavior:

- enable in every newly created profile;
- user may override later if appropriate.

---

# 10. MOD DOMAIN MODEL

Distinguish these concepts.

## Archive

The original downloaded `.zip`, `.7z`, or equivalent file.

Fields:

```text
id
sha256
md5
original_filename
size
source
source_mod_id
source_file_id
downloaded_at
stored_path
```

---

## Package

The extracted contents represented by one archive/import operation.

One package can contain multiple SMAPI mods.

Fields:

```text
id
archive_id
sha256
created_at
source_type
root_layout
```

---

## Mod Unit

One SMAPI-loadable folder identified by `manifest.json`.

Fields:

```text
id
package_id
relative_path
unique_id
name
author
version
description
entry_dll
content_pack_for_unique_id
minimum_api_version
minimum_game_version
manifest_raw
manifest_valid
manifest_error
```

---

## Mod Dependency

```text
mod_unit_id
dependency_unique_id
minimum_version
required
dependency_type
```

Dependency types:

```text
manifest_dependency
content_pack_host
```

---

## Source Reference

Represents the origin of the mod.

```text
mod_unit_id
provider
provider_mod_id
provider_file_id
provider_slug
update_key
source_url
file_md5
matched_by
confidence
```

`matched_by` values:

```text
nxm
nexus_file_id
archive_md5
manifest_update_key
dataset_unique_id
manual
unknown
```

Do not use fuzzy matching to silently establish authoritative provenance.

Fuzzy results must require user confirmation.

---

# 11. MANIFEST PARSER

Implement a resilient Stardew `manifest.json` parser.

Recognize:

```text
Name
Author
Version
Description
UniqueID
EntryDll
ContentPackFor
MinimumApiVersion
MinimumGameVersion
Dependencies
UpdateKeys
```

Unknown fields must be preserved as raw JSON.

A malformed optional field must not crash the archive import.

Example:

If `UpdateKeys` contains an unexpected type:

```text
Import status: warning
Manifest status: partially malformed
```

not:

```text
panic
```

Separate:

```text
JSON parsing
manifest interpretation
manifest validation
```

Do not make one giant `UnmarshalJSON` function responsible for all three.

---

# 12. ARCHIVE INSTALLATION PIPELINE

Installing must be transactional.

Pipeline:

```text
1. acquire
2. hash
3. inspect archive
4. security validation
5. extract to temporary directory
6. scan package
7. detect mod roots
8. parse manifests
9. validate package
10. resolve replacement/update relationships
11. preview installation
12. commit package to library
13. deploy to chosen profile(s)
14. persist transaction
15. clean temporary files
```

Nothing is committed until validation completes.

---

# 13. ARCHIVE SECURITY

Reject archive entries containing:

```text
absolute paths
../ traversal
Windows drive paths
UNC escape paths
device paths
symlinks escaping extraction root
hard links escaping extraction root
```

Protect against decompression bombs.

Set configurable safety limits for:

```text
expanded byte size
file count
compression ratio
path depth
path length
```

Never execute:

```text
.exe
.dll
.bat
.ps1
.sh
command files
installers
```

during import.

DLL files are content, not programs the manager should execute.

SMAPI will load them later when the user launches the game.

---

# 14. SUPPORTED ARCHIVE TYPES

MVP:

```text
.zip
folder import
```

V1:

```text
.zip
.7z
.rar where technically/licensing feasible
folder import
```

Do not block MVP development for every obscure archive format humans managed to invent.

---

# 15. ARCHIVE ROOT DETECTION

Support all common layouts.

Example A:

```text
Archive/
└── SomeMod/
    ├── manifest.json
    └── SomeMod.dll
```

Example B:

```text
Archive/
├── manifest.json
└── SomeMod.dll
```

Example C:

```text
Archive/
├── ModA/
│   └── manifest.json
├── ModB/
│   └── manifest.json
└── README.md
```

Example D:

```text
Archive/
└── WrapperFolder/
    ├── ModA/
    │   └── manifest.json
    └── ModB/
        └── manifest.json
```

Recursively discover manifests.

Do not assume one archive equals one mod.

---

# 16. XNB DETECTION

If package contains `.xnb` files but no usable SMAPI manifest:

mark:

```text
Legacy XNB Mod
```

Display:

```text
This package replaces Stardew Valley content files directly.
Astradew does not install XNB mods automatically.
```

Do not deploy it.

---

# 17. MOD LIBRARY

The library represents every mod version imported into Astradew.

Display:

```text
Name
Version
Author
Unique ID
Type
Installed profiles
Source
Update status
Health
```

Filters:

```text
Enabled
Disabled
Updates available
Missing dependencies
Content packs
SMAPI mods
Local mods
Nexus-linked
Unlinked
Invalid
System
```

Search across:

```text
name
author
UniqueID
notes
source
```

Large lists must be virtualized.

---

# 18. PROFILE MODEL

Each profile contains:

```text
id
name
description
created_at
updated_at
game_install_id
mods_path
launch_arguments
notes
```

Profile mod record:

```text
profile_id
mod_unit_id
package_version_id
enabled
pinned
ignored_update
installed_at
custom_notes
```

---

# 19. PROFILE FILESYSTEM MODEL

Each profile receives:

```text
Profiles/<profile-id>/Mods/
```

Enabled mod:

```text
Mods/LookupAnything/
```

Disabled mod:

```text
Mods/.disabled/LookupAnything/
```

SMAPI ignores folders beneath dot-prefixed parent folders.

This lets disabling preserve:

- generated configuration;
- caches;
- user edits;
- mod-created files.

Enable/disable is a filesystem move inside the same profile.

Use atomic rename where possible.

---

# 20. PROFILE ISOLATION

Do not use symlinks to one shared writable mod directory for MVP.

Each profile receives an independent deployed copy.

Reason:

Mods may write:

```text
config.json
cache files
generated assets
state files
custom data
```

A shared linked directory would allow one profile to mutate another.

Disk space is cheaper than corrupted state.

Possible future optimization:

```text
reflinks
copy-on-write clones
block cloning
content deduplication
```

Do not implement that before correctness is established.

---

# 21. PROFILE CREATION

Support:

```text
Empty profile
Clone current profile
Import existing Mods folder
Import Astradew profile
```

Cloning should preserve:

- exact mod versions;
- enabled state;
- configuration;
- notes;
- update pins.

---

# 22. PROFILE EXPORT FORMAT

Use a versioned JSON format.

Extension:

```text
.astradew-profile
```

Do not include actual mod binaries by default.

Example logical schema:

```json
{
  "formatVersion": 1,
  "name": "Multiplayer",
  "mods": [
    {
      "uniqueId": "Pathoschild.LookupAnything",
      "version": "1.46.3",
      "source": {
        "provider": "Nexus",
        "modId": 541,
        "fileId": 12345
      },
      "enabled": true
    }
  ]
}
```

Optionally export configs separately.

Import should resolve each mod and report:

```text
installed
missing
wrong version
unavailable
manual download required
```

Never silently substitute a materially different file.

---

# 23. CONFIGURATION MANAGEMENT

Profile isolation naturally creates profile-specific configs.

Add an in-app config editor only for valid JSON configuration files.

Recognize:

```text
config.json
```

Editor modes:

```text
structured tree
raw JSON
```

Requirements:

- JSON syntax validation;
- preserve unknown keys;
- create automatic backup before save;
- do not save while the game process is actively writing unless explicitly safe.

Generic Mod Config Menu remains the preferred configuration interface where appropriate.

The manager is not expected to understand every mod's arbitrary schema.

---

# 24. DEPENDENCY ENGINE

Build a dependency graph from:

```text
Dependencies[]
ContentPackFor
```

Evaluate:

```text
dependency exists
dependency enabled
minimum version satisfied
content-pack framework exists
required vs optional
```

Statuses:

```text
Healthy
Missing required dependency
Dependency disabled
Dependency version too old
Optional dependency absent
Duplicate UniqueID
Invalid manifest
Requires newer SMAPI
Requires newer Stardew Valley
```

Do not compress these into an arbitrary numerical health score.

Show concrete facts.

---

# 25. DUPLICATE DETECTION

Within a profile, detect duplicate `UniqueID` values.

This is a launch blocker.

Display both filesystem paths and versions.

Example:

```text
Pathoschild.ContentPatcher

1.30.4
Mods/ContentPatcher/

1.31.0
Mods/OldMods/ContentPatcher/
```

Provide:

```text
Disable older
Open folders
Cancel
```

---

# 26. COMPATIBILITY PREFLIGHT

Before launch, validate:

```text
SMAPI exists
Stardew executable exists
profile directory exists
manifest parsing
duplicate UniqueIDs
required dependencies
MinimumApiVersion
MinimumGameVersion
```

Do not block launch for minor warnings.

Separate:

```text
Blockers
Warnings
Information
```

---

# 27. GAME LAUNCHING

Primary launch button:

```text
Play <profile name>
```

Execute SMAPI directly using the profile path.

Use `exec.Cmd`.

Capture:

```text
PID
stdout
stderr
exit status
launch timestamp
```

Use appropriate terminal behavior on each platform.

Do not hide the SMAPI console permanently.

Provide launch setting:

```text
SMAPI console:
[Visible]
[Embedded]
[Hidden]
```

Default:

```text
Visible
```

If embedded console support is technically unreliable on a platform, open a real terminal instead.

---

# 28. GAME PROCESS LOCKING

While Stardew/SMAPI is running:

block destructive operations affecting the active profile.

Block:

```text
install
update
delete
bulk enable/disable
profile replacement
```

Allow:

```text
browse
search
notes
logs
non-active profiles
```

Automatically unlock after process termination.

Do not rely only on window existence.

Track process PID.

---

# 29. SMAPI LOG INTEGRATION

Detect the latest SMAPI log.

Create a parser that extracts useful structured events.

Categories:

```text
game version
SMAPI version
loaded mods
skipped mods
update notifications
errors
warnings
duplicate mods
dependency problems
compatibility failures
malicious/blocked mod notices
```

Health page should translate this into clear language while retaining raw log access.

Example:

```text
Automate 2.3.1 failed to load
Reason: requires SpaceCore 1.20.0 or later
Installed: SpaceCore 1.19.2
```

Provide:

```text
Open raw log
Copy diagnostic bundle
Open SMAPI log parser
```

---

# 30. DIAGNOSTIC BUNDLE

Generate a ZIP containing only non-sensitive debugging information.

Potential files:

```text
astradew.log
sanitized settings.json
profile manifest
mod list
dependency status
SMAPI log
environment summary
```

Do not include:

```text
Nexus API keys
OS credentials
save files
full home directory paths where avoidable
private tokens
```

Redact username path components where possible.

---

# 31. UPDATE ENGINE

Updates must be source-aware.

Do not simply compare installed version against the largest version number found on a Nexus page.

A Nexus page may contain:

- main files;
- optional files;
- compatibility branches;
- old files;
- alternate editions.

Store exact provenance when available.

Preferred matching order:

```text
1. exact NXM provenance
2. exact Nexus file ID
3. archive MD5 lookup
4. manifest Nexus UpdateKey
5. open Stardew mod dataset UniqueID mapping
6. manual source association
7. unresolved
```

---

# 32. VERSION UPDATE RULES

A mod update entry must preserve:

```text
current package
current file ID
current version
candidate file
candidate version
candidate category
source
```

When multiple plausible update targets exist:

show the choices.

Do not guess.

---

# 33. UPDATE TRANSACTION

Updating a mod:

```text
1. download/import candidate
2. hash
3. scan
4. verify UniqueID relationship
5. create rollback snapshot
6. compare old package with deployed instance
7. preserve relevant user-generated configuration
8. stage new version
9. validate dependencies
10. atomically replace instance
11. record history
12. keep rollback target
```

Rollback should restore:

```text
old package
old configuration
enabled state
source metadata
```

---

# 34. LOCAL MODIFICATION DETECTION

Every deployment should record baseline hashes.

Before:

```text
update
uninstall
delete
```

detect whether files differ from the originally deployed package.

Classify:

```text
unchanged
new file
modified file
missing file
```

If modified files would be destroyed:

display them.

Never silently delete externally modified files.

---

# 35. CONFIG PRESERVATION DURING UPDATES

At minimum, automatically preserve root-level:

```text
config.json
```

Longer-term implement package delta logic.

If the upstream package contains a changed `config.json` and the local file is also modified:

retain both:

```text
config.json
config.astradew-upstream.json
```

and inform the user.

Data preservation beats clever merging.

---

# 36. VERSION HISTORY

Each mod detail page should show:

```text
Current 1.4.2
Previous 1.4.1
Previous 1.3.9
```

Actions:

```text
Restore
Delete cached version
View source
```

Keep at least the immediately previous version after automatic update unless the user disables rollback retention.

---

# 37. NEXUS MODS INTEGRATION

Nexus is an optional adapter.

The application must work without Nexus credentials.

Development phase:

support manually entered Personal API Key.

Public release:

register the application with Nexus Mods.

After registration:

support official SSO if applicable.

Never ship the developer's personal API key.

---

# 38. NEXUS CAPABILITIES

Supported:

```text
account validation
mod metadata
file metadata
update checks
archive matching
endorsement where allowed
NXM protocol handling
direct downloads for eligible accounts
open exact Nexus page
```

Avoid building the product around:

```text
full Nexus browser
community activity feeds
ratings feeds
generic discovery
content mirroring
```

Keep Nexus the authority for hosted content.

---

# 39. FREE USER DOWNLOAD FLOW

For users without API-authorized direct downloads:

```text
Check update
→ show exact target
→ Open Files Page / Download with Manager
→ website handles authorization
→ nxm:// returns to Astradew where supported
→ Astradew downloads/imports
```

Do not bypass Nexus download restrictions.

---

# 40. PREMIUM/ELIGIBLE DOWNLOAD FLOW

Where Nexus permits direct programmatic downloads:

```text
Download
→ Downloads queue
→ temporary .part file
→ progress
→ retry
→ verification
→ archive library
→ optional auto-install
```

Never install incomplete downloads.

---

# 41. NXM PROTOCOL

Implement URI parsing for:

```text
nxm://
```

Store:

```text
game domain
mod ID
file ID
key
expiry
additional fields
```

Validate:

```text
game == stardewvalley
required IDs exist
expiry where applicable
```

On Windows, provide explicit protocol registration.

macOS/Linux registration may be added through platform packaging after tested.

Never seize the NXM association without confirmation.

---

# 42. DOWNLOAD MANAGER

States:

```text
queued
resolving
downloading
paused
verifying
completed
failed
cancelled
```

Features:

```text
progress
transferred bytes
total size
speed
retry
cancel
open file
install
```

Persist enough information to recover abandoned downloads where server semantics permit.

---

# 43. OPEN STARDEW MOD DATASET

Implement an optional dataset adapter.

Use it for:

```text
UniqueID → mod page lookup
source suggestions
package provenance hints
dependency metadata enrichment
unknown-mod identification
```

Cache indexes locally.

Do not download a 100GB mod dump.

Only fetch the structured indexes/data needed by the manager.

Store:

```text
dataset version
dataset updated_at
last_refresh
```

If dataset schema is pre-1.0:

isolate parsing behind an adapter so schema changes do not infect the application domain model.

---

# 44. SOURCE ADAPTER INTERFACE

Define:

```go
type ModSource interface {
    Resolve(ctx context.Context, ref SourceRef) (*ResolvedMod, error)
    CheckUpdates(ctx context.Context, mod InstalledMod) ([]UpdateCandidate, error)
    MatchArchive(ctx context.Context, archive ArchiveInfo) ([]MatchCandidate, error)
}
```

Adapters:

```text
Nexus
GitHub
StardewDataset
Manual
```

Do not put Nexus-specific fields all over core mod structs.

---

# 45. GITHUB UPDATE KEYS

Recognize manifest keys such as:

```text
GitHub:Owner/Repository
```

Use GitHub release metadata conservatively.

Respect API limits.

Cache responses.

Do not treat arbitrary GitHub commits as release updates.

---

# 46. DATABASE

Use SQLite with migrations.

Enable:

```text
foreign_keys = ON
WAL mode
busy_timeout
```

Suggested tables:

```text
schema_migrations
settings
game_installs
profiles
archives
packages
package_files
mod_units
mod_dependencies
source_refs
profile_mods
profile_instances
deployment_files
mod_versions
update_candidates
downloads
transactions
transaction_files
diagnostic_runs
app_events
```

Indexes:

```text
mod_units(unique_id)
profile_mods(profile_id)
source_refs(provider, provider_mod_id)
archives(sha256)
archives(md5)
downloads(status)
```

---

# 47. DATABASE MIGRATION POLICY

Every schema change requires a numbered migration.

Never mutate the SQLite schema ad hoc during service startup.

Before risky migrations:

create a database backup.

Migrations must be tested using:

```text
fresh DB
previous production DB fixture
rollback/recovery scenario
```

---

# 48. TRANSACTION MODEL

Every destructive mod operation receives a transaction ID.

Transaction types:

```text
install
update
uninstall
enable
disable
profile_import
profile_clone
profile_delete
```

Record:

```text
started_at
completed_at
status
affected_paths
backup_paths
error
```

Possible statuses:

```text
pending
committed
rolled_back
failed
```

On application startup:

detect incomplete transactions.

Provide automatic recovery where deterministic.

---

# 49. FILESYSTEM WRITES

Use:

```text
temp file
fsync where important
atomic rename
```

for:

```text
profile metadata
database backups
configuration writes
transaction records
```

Never write directly over critical files if an atomic replacement is possible.

---

# 50. FILE WATCHING

Use filesystem watchers for profile mod directories.

Detect manual changes while Astradew is open.

Events:

```text
mod added externally
mod removed externally
manifest changed
config changed
folder renamed
```

Debounce bursts.

Do not rescan the entire library for every filesystem event.

---

# 51. MANUAL MOD EDITS

Users remain free to edit profile folders.

When unknown content appears:

show:

```text
External changes detected
```

Actions:

```text
Import changes
Ignore
Rescan profile
```

Never immediately delete an unknown manually added folder because the database did not create it.

---

# 52. PERFORMANCE REQUIREMENTS

Frontend shell:

```text
visible < 1 second on typical SSD after process start
```

Library view:

```text
render first cached results < 500 ms
```

Scanning:

```text
must occur asynchronously
must report progress
must be cancellable
```

Target:

```text
1000 installed mod units:
full metadata scan <= 5 seconds on typical modern SSD
```

Hashing large archives may exceed this and should run independently.

UI must remain responsive while:

```text
hashing
extracting
checking updates
scanning
copying profiles
```

---

# 53. CONCURRENCY

Use bounded worker pools.

Do not launch one goroutine per file across a 50,000-file archive.

Use contexts for cancellation.

Suggested limits:

```text
metadata scanning: CPU-aware bounded pool
filesystem copies: low bounded concurrency
HTTP update checks: provider-specific rate limit
hashing: bounded by disk throughput
```

Protect shared Wails services with appropriate mutexes.

---

# 54. CACHING

Cache:

```text
parsed manifests
archive hashes
file hashes
Nexus metadata
GitHub release metadata
dataset indexes
SMAPI/game version detection
```

Cache keys must incorporate enough source state to invalidate correctly.

Filesystem metadata cache may use:

```text
path
size
mtime
```

before recalculating expensive hashes.

---

# 55. UI: LIBRARY

Default presentation should be compact.

Columns:

```text
State
Name
Version
Author
Profile
Source
Health
Update
```

Allow optional columns:

```text
UniqueID
Install date
Modified date
Notes
Framework
```

Views:

```text
List
Compact cards
```

List is default for large libraries.

---

# 56. UI: MOD DETAIL DRAWER

Selecting a mod opens a side panel instead of navigating away.

Sections:

```text
Overview
Dependencies
Versions
Files
Configuration
Source
Diagnostics
Notes
```

Actions:

```text
Enable/Disable
Check update
Update
Open folder
Open source page
Rollback
Remove from profile
Delete from library
```

---

# 57. UI: PROFILES

Profile page shows:

```text
profile name
enabled mods
disabled mods
health
last played
game version
SMAPI version
```

Actions:

```text
Play
Clone
Rename
Export
Compare
Delete
```

---

# 58. PROFILE COMPARISON

Given profile A and profile B show:

```text
Only in A
Only in B
Different version
Different enabled state
Config differs
Same
```

This is especially useful for multiplayer debugging.

---

# 59. UI: HEALTH

Health page grouped into:

```text
Launch blockers
Dependency issues
Compatibility warnings
Invalid packages
External file changes
SMAPI errors from last run
Updates requiring attention
```

Every issue needs:

```text
what happened
why it matters
affected mod
recommended concrete action
```

Avoid messages such as:

```text
Something went wrong.
```

That sentence has survived far longer in software than it deserves.

---

# 60. UI: UPDATES

Sections:

```text
Ready
Needs selection
Manual download
Ignored
Unknown source
```

Each entry displays:

```text
installed version
candidate version
source
file name
change source
```

Actions:

```text
Update
Open source
Ignore this version
Ignore all updates
Pin current version
```

---

# 61. UI: DOWNLOADS

Persistent drawer/panel containing:

```text
current transfers
completed downloads
failed downloads
```

Do not use modal dialogs for long-running downloads.

---

# 62. UI: LOGS

Tabs:

```text
Astradew
SMAPI
```

Support:

```text
search
severity filters
copy
save
open external file
```

Streaming Astradew log viewer is optional.

---

# 63. SETTINGS

Categories:

## General

```text
theme
language
startup behavior
confirmation behavior
```

## Stardew Valley

```text
game installation
SMAPI executable
default profile
```

## Mod Library

```text
library location
archive retention
rollback retention
```

## Nexus Mods

```text
connection
account state
protocol handler
download preference
```

## Updates

```text
automatic metadata checks
application update channel
```

## Advanced

```text
diagnostics
database path
cache reset
rescan
open app data
```

---

# 64. THEMING

Default visual language:

```text
clean desktop utility
soft Stardew-inspired accents
high information density
clear status badges
excellent dark mode
```

Do not reproduce Stardew Valley copyrighted UI assets.

Do not turn every panel into parchment, wooden signs, pixel chickens, and beige gradients.

The manager is a tool.

---

# 65. ACCESSIBILITY

Required:

```text
keyboard navigation
visible focus states
screen-reader labels
WCAG-compliant contrast
reduced-motion support
no color-only statuses
scalable UI text
```

---

# 66. ERROR ARCHITECTURE

Define typed backend errors.

Example:

```go
type AppError struct {
    Code        string
    Message     string
    Details     string
    Recoverable bool
}
```

Example codes:

```text
MOD_MANIFEST_INVALID
ARCHIVE_PATH_TRAVERSAL
ARCHIVE_TOO_LARGE
DUPLICATE_MOD_ID
DEPENDENCY_MISSING
SMAPI_NOT_FOUND
GAME_NOT_FOUND
NEXUS_AUTH_REQUIRED
NEXUS_RATE_LIMITED
NEXUS_MANUAL_DOWNLOAD_REQUIRED
TRANSACTION_ROLLBACK_FAILED
PROFILE_LOCKED
```

Frontend behavior should depend on error code, not English substring matching.

---

# 67. LOGGING

Use structured logging.

Fields:

```text
operation
transaction_id
profile_id
mod_unique_id
archive_id
provider
duration_ms
error_code
```

Never log:

```text
API keys
NXM secrets
authorization headers
passwords
credential-store content
```

---

# 68. TEST STRATEGY

## Unit tests

Required for:

```text
manifest parsing
semantic-version comparisons
dependency resolution
archive root detection
source matching
NXM URI parser
profile diff
path security
transaction planning
```

---

## Fixture tests

Maintain real-world-style fixtures:

```text
single mod ZIP
nested wrapper ZIP
multi-mod ZIP
content pack
missing manifest
malformed manifest
malformed UpdateKeys
duplicate UniqueID
legacy XNB
zip traversal
absolute archive path
huge-compression-ratio fixture
```

---

## Fuzz tests

Fuzz:

```text
manifest JSON
NXM URIs
archive entry names
version strings
```

Parser input must never panic.

---

# 69. INTEGRATION TESTS

Build temporary fake Stardew environments.

Test:

```text
import existing Mods
create profile
install archive
enable
disable
update
rollback
launch fake SMAPI
capture arguments
detect exit
```

Fake SMAPI executable should record:

```text
argv
environment
working directory
```

Verify exact `--mods-path`.

---

# 70. DATABASE TESTS

Test:

```text
fresh migration
upgrade migration
partial transaction recovery
foreign-key constraints
duplicate protections
```

---

# 71. NEXUS TESTS

Never run normal unit tests against production Nexus.

Use mock HTTP server fixtures.

Test:

```text
401
403
404
429
500
invalid JSON
timeout
cancelled request
quarantined file
multiple candidate files
manual-download requirement
```

---

# 72. FRONTEND TESTS

Use:

```text
Vitest
React Testing Library
Playwright
```

Critical Playwright flows:

```text
onboarding
local mod install
profile creation
profile switching
dependency warning
update preview
transaction failure recovery
```

---

# 73. SECURITY TESTS

Required regression cases:

```text
../../escape
..\..\escape
C:\escape
\\server\share
absolute Unix path
symlink outside temp root
archive bomb
very long path
reserved Windows filename
mixed slash traversal
Unicode path confusion where applicable
```

---

# 74. RELEASE PACKAGING

Target artifacts:

### Windows

```text
x64 installer
ARM64 later
portable ZIP optional
```

### macOS

```text
universal or architecture-specific .app/.dmg
signed
notarized
```

### Linux

```text
AppImage
.deb
```

Steam Deck:

support through Linux build.

---

# 75. CODE SIGNING

Treat signing as a release requirement, not decoration.

Unsigned mod managers trigger platform security systems because, rather inconveniently, software which moves DLLs around resembles software which moves DLLs around.

Configure:

```text
Windows signing
Apple Developer ID signing
macOS notarization
release checksums
```

---

# 76. APPLICATION UPDATE SYSTEM

Use signed GitHub releases initially.

Update workflow:

```text
check metadata
show changelog
download package
verify checksum/signature
run platform updater
restart
```

Do not replace the running executable directly from arbitrary HTTP response data.

---

# 77. CI/CD

GitHub Actions matrix:

```text
windows-amd64
macos-amd64
macos-arm64
linux-amd64
```

Pipeline:

```text
go fmt check
go vet
go test ./...
frontend lint
frontend test
frontend build
Wails production build
integration tests
security fixture tests
package
sign where secrets available
generate SHA-256 sums
publish release
```

---

# 78. VERSIONING

Use semantic versioning:

```text
0.x development
1.0 stable
```

Database schema version is independent.

Profile export format has independent `formatVersion`.

Do not tie everything to one number.

---

# 79. MVP SCOPE

MVP is complete only when these work reliably:

1. game detection;
2. SMAPI detection;
3. existing Mods scan;
4. ZIP and folder import;
5. manifest parsing;
6. library;
7. install/uninstall;
8. enable/disable;
9. isolated profiles;
10. profile clone;
11. dependency warnings;
12. launch using `--mods-path`;
13. active-process locking;
14. basic SMAPI log detection;
15. rollback-safe filesystem transactions;
16. structured app logs;
17. Windows production build.

Do not add Nexus before these are stable.

---

# 80. V0.2 SCOPE

Add:

```text
mod update provenance
Nexus API
Personal API key
NXM Windows handler
download queue
archive MD5 matching
GitHub UpdateKeys
ignored updates
pinning
```

---

# 81. V0.3 SCOPE

Add:

```text
SMAPI health parser
profile export/import
profile comparison
configuration editor
version history
rollback UI
open Stardew dataset integration
macOS production support
Linux production support
```

---

# 82. V1.0 SCOPE

Required before stable:

```text
Windows
macOS
Linux
Steam Deck
Nexus registered application
robust transactions
auto recovery
mod updates
profile portability
diagnostic bundles
signed/notarized builds
documented backup/recovery procedure
no known data-loss bugs
```

---

# 83. FUTURE FEATURES

Candidates only after 1.0:

```text
Nexus Collections
SMAPI installation/update
save-profile associations
per-save recommended profiles
save backup viewer
automatic multiplayer profile comparison
LAN profile transfer
reflink profile storage
plugin architecture
multiple Stardew installations
portable mode
```

---

# 84. DO NOT IMPLEMENT THESE AS SHORTCUTS

Never:

```text
rename Mods to Mods.disabled as the core profile system
delete then copy without staging
assume archive root depth
assume every ZIP contains one manifest
identify mods by folder name only
identify updates by display name only
store Nexus keys in SQLite
execute downloaded installers
silently fix malformed mods
silently delete externally changed files
silently modify Steam configuration
block the UI while hashing
store all backend state in frontend memory
```

---

# 85. IMPORTANT EDGE CASES

Explicitly handle:

```text
two mods with identical folder names
two mods with identical UniqueID
same mod installed from two Nexus pages
one archive containing multiple mods
content pack without its framework
missing EntryDll
manifest references nonexistent DLL
invalid semantic version
unknown manifest fields
read-only game directory
SMAPI currently running
profile folder manually deleted
library moved to another disk
database references missing package
archive deleted after installation
Nexus file deleted
Nexus mod hidden
Nexus API unavailable
user loses Internet during update
disk fills during deployment
application crashes halfway through update
antivirus removes DLL after installation
Windows Smart App Control blocks DLL
macOS quarantine flags application
case-sensitive vs case-insensitive filesystems
```

---

# 86. PRODUCT ACCEPTANCE CRITERIA

A release candidate must demonstrate:

### Safety

Force-kill Astradew during a mod update.

Restart it.

The previous profile must still be recoverable.

### Isolation

Modify `config.json` in Profile A.

Profile B's configuration must remain unchanged.

### Dependencies

Disable Content Patcher while a required content pack is active.

Health must immediately report the dependency failure.

### Mod parsing

Import an archive with malformed optional manifest fields.

Application must report the issue without crashing.

### Archive security

Import a traversal archive.

No file may be written outside the staging directory.

### Scale

Load 1,000 indexed mod entries.

UI must remain responsive and scrolling must not degrade materially.

### Launch

Launch each profile.

SMAPI must receive that profile's exact Mods path.

### External modification

Edit an installed file manually.

Attempt an update.

Astradew must detect that the local file differs before deleting it.

---

# 87. INITIAL DATABASE DOMAIN TYPES

Create explicit Go domain types rather than passing database rows directly to Wails.

Example:

```go
type Mod struct {
    ID          string
    UniqueID    string
    Name        string
    Author      string
    Version     Version
    Description string
    Type        ModType
    MinimumSMAPI *Version
    MinimumGame  *Version
    Dependencies []Dependency
    Sources      []SourceReference
}

type Profile struct {
    ID       string
    Name     string
    ModsPath string
    Mods     []ProfileMod
}

type ProfileMod struct {
    ModID         string
    Enabled       bool
    Pinned        bool
    IgnoreUpdates bool
    Notes         string
}

type Dependency struct {
    UniqueID       string
    MinimumVersion *Version
    Required       bool
}

type HealthIssue struct {
    Code       string
    Severity   Severity
    ModID      string
    Message    string
    Resolution string
}
```

---

# 88. WAILS BACKEND API SHAPE

Expose coarse operations.

Good:

```go
LibraryService.ListMods(...)
LibraryService.GetMod(...)
InstallService.InspectArchive(...)
InstallService.Install(...)
ProfileService.List(...)
ProfileService.Create(...)
ProfileService.SetModEnabled(...)
ProfileService.Compare(...)
UpdateService.Check(...)
UpdateService.Apply(...)
LaunchService.Preflight(...)
LaunchService.Launch(...)
DiagnosticsService.GetHealth(...)
```

Bad:

```go
DeleteFile(path)
CopyFile(a, b)
RunCommand(cmd)
ExecuteSQL(query)
```

Never expose arbitrary filesystem/process execution to the frontend.

---

# 89. ASYNC TASK MODEL

Long operations return task IDs.

Example:

```text
InstallArchive()
→ task_id
```

Backend sends events:

```text
task.started
task.progress
task.warning
task.completed
task.failed
```

Task payload:

```text
task_id
operation
current
total
message
```

Frontend renders progress without polling every 100 ms like a web application from 2009.

---

# 90. IMPLEMENTATION PHASE 0 — FOUNDATION

Deliver:

```text
Wails app
React frontend
routing
SQLite
migrations
structured logger
application directories
settings service
error model
task/event infrastructure
CI
```

Acceptance:

```text
wails3 dev works
wails3 build works
tests run
database initializes
frontend receives Go binding calls
```

---

# 91. IMPLEMENTATION PHASE 1 — GAME + SMAPI DISCOVERY

Build:

```text
platform install discovery
manual game location
SMAPI detection
version detection
Mods folder detection
```

No mod mutation yet.

Write integration tests with fake game directories.

---

# 92. IMPLEMENTATION PHASE 2 — MANIFEST + SCANNER

Build:

```text
manifest parser
mod scanner
dependency extraction
content-pack detection
XNB detection
duplicate detection
filesystem cache
```

Add fixture library.

This phase must be extremely well tested because nearly everything depends on it.

---

# 93. IMPLEMENTATION PHASE 3 — ARCHIVE INSPECTION

Build:

```text
ZIP import
secure extractor
archive hashing
root detection
multi-mod package support
install preview
```

Still do not deploy until transaction layer exists.

---

# 94. IMPLEMENTATION PHASE 4 — TRANSACTION ENGINE

Build:

```text
staging
transaction journal
atomic commit
rollback
startup recovery
backup paths
file modification detection
```

Only after this phase may actual installs become enabled.

---

# 95. IMPLEMENTATION PHASE 5 — LIBRARY + PROFILE DEPLOYMENT

Build:

```text
library storage
default profile
profile creation
profile cloning
copy deployment
enable
disable
uninstall
profile isolation
```

Use `.disabled` parent directory.

---

# 96. IMPLEMENTATION PHASE 6 — LAUNCH

Build:

```text
preflight
SMAPI process
--mods-path
process tracking
profile locking
console strategy
exit detection
```

Do not touch Steam options automatically.

---

# 97. IMPLEMENTATION PHASE 7 — HEALTH

Build:

```text
dependency graph
duplicate warnings
SMAPI/game version validation
manifest warnings
Health page
```

Then parse SMAPI logs.

---

# 98. IMPLEMENTATION PHASE 8 — UPDATE ENGINE

Build source-neutral update model first.

Then:

```text
manifest UpdateKeys
GitHub adapter
dataset adapter
Nexus adapter
```

Do not start by hardcoding Nexus calls into mod rows.

---

# 99. IMPLEMENTATION PHASE 9 — NEXUS

Development:

```text
personal API key
secure credential store
user validation
mod/file metadata
MD5 matching
update lookup
manual website flow
```

Then:

```text
NXM Windows association
download queue
eligible direct downloads
```

Before public release:

register application properly.

---

# 100. IMPLEMENTATION PHASE 10 — HARDENING

Focus solely on:

```text
data-loss testing
cross-platform paths
crash recovery
read-only directories
low disk space
antivirus removal
large mod libraries
network errors
API throttling
filesystem races
```

No shiny new features during hardening.

---

# 101. ENGINEERING RULES FOR THE CODING AGENT

You are implementing production software.

For every phase:

1. inspect the existing repository before modifying it;
2. preserve established architecture unless there is a concrete reason to change it;
3. implement the smallest complete vertical slice;
4. add tests with the implementation;
5. run backend tests;
6. run frontend tests;
7. run static checks;
8. build the application;
9. fix all introduced warnings;
10. update documentation.

Do not leave placeholder implementations such as:

```text
TODO implement later
return nil
mock data
hardcoded sample mod
```

inside completed features.

---

# 102. CODING QUALITY RULES

Prefer:

```text
small packages
explicit interfaces
dependency injection
typed domain objects
context cancellation
structured errors
pure functions for parsing
transaction boundaries
table-driven tests
```

Avoid:

```text
god objects
global mutable state
frontend business logic
panic for user input
stringly typed status values
filesystem code embedded in UI bindings
1000-line service files
```

---

# 103. SAFE FAILURE RULE

When uncertain whether an operation could destroy user data:

fail without modifying the source.

Return enough information for recovery.

Never choose convenience over preservation.

---

# 104. DEVELOPMENT OUTPUT FORMAT

After completing each phase, report:

```text
Implemented
Architecture decisions
Files added/changed
Tests added
Commands run
Test results
Known limitations
Next phase
```

Do not claim success unless the build and relevant tests actually run successfully.

---

# 105. FIRST IMPLEMENTATION TASK

Start with Phase 0 only.

Create the production-ready Wails v3 skeleton for Astradew.

Requirements:

```text
Wails v3.0.0-beta.25 pinned
Go module
React + TypeScript frontend
Tailwind
frontend router
SQLite initialization
migration system
structured slog logger
cross-platform app data paths
typed AppError model
task manager
Wails event bridge
SettingsService
Health placeholder route with real backend connectivity
unit-test setup
frontend-test setup
GitHub Actions
README development instructions
```

Do not implement mod installation yet.

Proposed initial routes:

```text
/
/library
/profiles
/updates
/health
/downloads
/logs
/settings
```

Create empty-state interfaces rather than fake data.

Application must successfully:

```text
start
initialize SQLite
apply migration 001
load settings
invoke Go service from React
persist a setting
restart
read persisted setting
build in production mode
```

Add tests proving settings persistence and database migration.

Only proceed to Phase 1 once this foundation is stable.

---

# 106. DEFINITION OF THE PRODUCT

The finished application is not merely:

> a GUI that moves folders around.

Astradew is a transactional Stardew Valley environment manager built around SMAPI's real mod model.

The architecture must make these operations boring and predictable:

```text
install
disable
switch
update
rollback
diagnose
launch
```

That is the standard every implementation decision should serve.
