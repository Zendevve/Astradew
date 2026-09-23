# Astradew

Astradew manages Stardew Valley modded play environments built around SMAPI. Its domain distinguishes imported mod content from the profiles in which players use it.

## Language

### Mod content

**Archive**:
The original downloaded file containing a mod distribution, before extraction or deployment.
_Avoid_: Package

**Package**:
The contents represented by one archive import or folder import. A Package may contain several independently loadable Mod Units.
_Avoid_: Archive, Mod Unit

**Mod Unit**:
One SMAPI-loadable folder described by its own Manifest. A Mod Unit may be a code mod or a Content Pack.
_Avoid_: Package

**Manifest**:
The JSON file a Mod Unit carries that declares its identity, version, entry point, dependencies, and content-pack host.
_Avoid_: Metadata file, mod info

**Unique ID**:
The identifier a Manifest declares for its Mod Unit, which SMAPI uses to resolve dependencies and content-pack hosts. It is compared without regard to letter case.
_Avoid_: Mod ID, mod name, folder name

**Content Pack**:
A Mod Unit whose content is consumed by another mod, its host or framework.

**System Mod**:
A Mod Unit supplied with SMAPI rather than obtained as an independent third-party mod.

**Source Reference**:
An association between mod content and its distribution origin, such as a provider, mod page, or specific downloadable file.
_Avoid_: Unique ID

### Profiles

**Profile**:
A named play environment with its own collection of Mod Units, enabled states, and configuration.
_Avoid_: Game Installation

**Game Installation**:
An installed copy of Stardew Valley against which a Profile can be launched.
_Avoid_: Profile

**Library**:
The collection of imported mod content and versions available for use in Profiles, distinct from any particular Profile's deployed files.
_Avoid_: Profile, Mods folder

**Deployed Copy**:
The files a Profile physically holds for a Mod Unit, which may differ from the Package they were deployed from.

**Recorded Intent**:
What Astradew believes a Profile should contain, as distinct from what its files currently are.

**Observed Profile State**:
What a Profile's files actually are, which is what SMAPI will load.

**Reconciliation**:
Resolving a disagreement between Recorded Intent and Observed Profile State before a change that depends on it is applied.

### Recovery

**Mod Transaction**:
A recoverable unit of change scoped to one Profile.

**Recovery Snapshot**:
A copy of the affected files taken before a Mod Transaction commits, and kept until that commit succeeds.

**Rollback Target**:
A previous version retained after a successful commit, which a Profile can be restored to.
