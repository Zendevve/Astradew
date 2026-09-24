-- 0001_init creates the foundation tables every later migration builds on:
-- settings (key/value application settings) and tasks (durable records for
-- long operations; events are live hints, these rows are the source of truth
-- per ADR 0006).
--
-- Statements run inside one transaction; keep them plain CREATE TABLE so a
-- half-applied file fails loudly instead of masking drift.

CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    operation TEXT NOT NULL,
    status TEXT NOT NULL,
    current INTEGER NOT NULL DEFAULT 0,
    total INTEGER NOT NULL DEFAULT 0,
    message TEXT NOT NULL DEFAULT '',
    outcome TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
