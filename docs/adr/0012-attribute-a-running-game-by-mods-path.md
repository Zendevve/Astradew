# Attribute a running game by mods path, not by process name

Astradew decides a Profile is in use by reading the running `StardewModdingAPI` process's command line and environment — `--mods-path` and `SMAPI_MODS_PATH` — and matching the resolved mods directory against its Profiles, checked on demand rather than by polling. The process name alone proves nothing: a SMAPI session can belong to another installation or a hand-made folder, so locking on the name would block work for no reason, while not locking at all would let an update overwrite a running game.

## Consequences

When the mods path cannot be read but the executable sits inside a known Game Installation, that installation's Profiles are locked with an explicit override and an explanation. SMAPI's exclusively-held `SMAPI-latest.txt` only ever indicates that some session is live — its mods path is anonymised and the file is shared per user, so it never attributes a Profile. Astradew's own launches pass both carriers (argument and environment) so its games are always attributable, and it holds the process handle for authoritative exit detection.
