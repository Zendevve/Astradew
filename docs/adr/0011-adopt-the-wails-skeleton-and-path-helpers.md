# Adopt the framework's skeleton and its path helpers

The entry point stays at the repository root because `//go:embed all:frontend/dist` cannot traverse upward, so the PRD's `cmd/` directory is not used for `main.go`; every other package lives under `internal/`. Generated bindings are written to `frontend/bindings` where the Wails Vite plugin reads them, and the CLI-generated `build/` tree with its Taskfiles is kept for packaging.

Application data resolves through `application.Path(application.PathDataHome)` instead of hand-rolled OS logic: `%LOCALAPPDATA%` on Windows, `~/Library/Application Support` on macOS, `$XDG_DATA_HOME` or `~/.local/share` on Linux. Everything Astradew owns — database, library, profiles, backups, cache, logs — sits under a single `Astradew/` root there, because DataHome and ConfigHome resolve to the same directory on Windows and macOS, and splitting them would only add a second convention.

## Consequences

On Windows the root is Local, never Roaming: a Library can be many gigabytes, and roaming profiles would try to synchronise it.
