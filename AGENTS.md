## Agent skills

### Issue tracker

Issues and specs live in GitHub Issues for `Zendevve/Astradew`. Before reading or publishing tickets, read `docs/agents/issue-tracker.md`.

### Triage labels

Use the five default triage labels. Before triaging issues or applying triage labels, read `docs/agents/triage-labels.md`.

### Domain docs

Single-context: root `CONTEXT.md` and `docs/adr/`. Before exploring the codebase, read `docs/agents/domain.md`.

### Wails v3

- Keep application packages under `internal/` and the embed entry point at the repository root; see ADR-0011.
- `main.go` embeds `frontend/dist`. Keep its tracked `.gitkeep` and preserve the Vite plugin that emits it after builds; Vite clears the output directory.
- The development app must start after Vite is accepting connections. Keep the TCP readiness port in `build/config.yml` synchronized with `Taskfile.yml`, and verify hot reload in the running window when changing this path.
- After changing `info` in `build/config.yml`, run `wails3 task common:update:build-assets` and check the generated platform metadata.
- Pin the Go Wails module and `@wailsio/runtime` to the same exact version. Regenerate and commit `frontend/bindings` when a bound service changes.
