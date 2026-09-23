# Persist task records instead of trusting events

Long operations expose a task ID and report progress through Wails events, which are in-memory only: they are never replayed and do not survive a frontend reload or an application restart. Task records therefore live in SQLite as the source of truth, events are live hints, and the frontend re-reads task state when it mounts.

## Consequences

Startup recovery finds operations that never finished by reading the same records, rather than depending on a notification that may never have been delivered.
