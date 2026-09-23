# Fail closed at startup instead of repairing silently

When the database is corrupt, a migration fails, or the app data directory is unwritable, Astradew reports a specific error with a next step and refuses to run rather than recreating or discarding state on the user's behalf. Repair is offered as an explicit action — restore the newest database backup, or start fresh while preserving the old file — because a mod manager that silently rebuilds its own state is one that silently loses a library.

## Consequences

A failed migration rolls back and keeps its pre-migration backup. An incomplete Mod Transaction whose resolution is deterministic is recovered automatically; one that is not leaves its Profile with mutations blocked until the user reconciles it.
