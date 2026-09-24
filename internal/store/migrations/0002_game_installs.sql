-- 0002_game_installs creates durable storage for detected game
-- installations. One row per installed copy of Stardew Valley (CONTEXT.md
-- Game Installation); Profiles will point at these rows and MUST NEVER
-- embed installation data. The installation directory is recorded durably
-- here for process attribution per ADR-0012.
--
-- Statements run inside one transaction; keep them plain CREATE TABLE so a
-- half-applied file fails loudly instead of masking drift.

CREATE TABLE game_installs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT NOT NULL,
    source TEXT NOT NULL,
    smapi_exe_path TEXT,
    game_version TEXT,
    smapi_version TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (path)
);
