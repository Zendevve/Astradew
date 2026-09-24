# Astradew

Astradew manages Stardew Valley modded play environments built around SMAPI.
This repository is at Phase 0: the foundation — application shell, storage,
settings, tasks and continuous integration. Nothing here installs a mod yet.

## Prerequisites

| Requirement | Notes |
| --- | --- |
| Go | 1.25.0 or newer. `go.mod` pins the toolchain (`toolchain go1.25.14`), so Go downloads the exact compiler on first use. No system-wide upgrade is needed. |
| Node.js and npm | Node 22 or newer. |
| Wails v3 CLI | `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.25` — the version must match the one in `go.mod`. |
| WebView2 runtime | Windows 10/11 ships it. macOS and Linux use the platform webview. |

The Go module requires Go 1.25.0 and pins the Wails framework at
`v3.0.0-beta.25` exactly; both are declared in `go.mod`, not installed globally.

## Reaching a running application

```sh
git clone https://github.com/Zendevve/Astradew
cd astradew

npm install --prefix frontend   # frontend dependencies
wails3 dev                      # development loop: frontend hot reload, Go rebuilds
```

`wails3 dev` builds the frontend, generates the bindings, and starts the
application. Saving a frontend file reloads the window; saving a Go file
rebuilds and restarts it.

## Everyday commands

| Command | What it does |
| --- | --- |
| `wails3 dev` | Development loop with hot reload. |
| `wails3 build` | Production build; writes `bin/astradew.exe` (`bin/astradew` elsewhere). |
| `wails3 task run` | Runs the last built binary without rebuilding. |
| `wails3 generate bindings -ts -i` | Regenerates `frontend/bindings` after changing a bound Go service. |
| `go test ./internal/...` | Backend tests. They run against the service layer directly, with no framework runtime and no webview, so they work on a fresh clone. `build/ios` stays excluded: it carries a pre-existing main-package issue, so the gate is `go build . ./internal/...` and `go vet . ./internal/...`, never `./...`. |
| `go test ./e2e/ -count=1` | End-to-end harness. Builds `bin/astradew.exe`, launches it with an isolated data home and a remote-debugging port, attaches over CDP, and asserts Go-originated values are rendered: identity, the Health report, and — in `TestProductionAppRestartsWithSettingPersisted` — a setting changed through the interface that survives a real process restart. Windows only with an interactive session and free loopback ports 19311–19313; never runs in CI. |
| `npx tsc --noEmit` (in `frontend/`) | TypeScript check. |
| `npx vitest run` (in `frontend/`) | Frontend tests against stubbed bindings. |

CI runs all of the above except the e2e harness on Windows and Linux for every push and pull request (see `.github/workflows/ci.yml`): gofmt, vet, backend tests, frontend check/tests/production build, and the production application build. The built binary is never executed in CI and the e2e harness never runs there — headless by design, no game installation, no network service, no developer machine.

## Layout

```text
main.go                  entry point; must stay at the root because it embeds frontend/dist
internal/                every other Go package
  app/                   the bound service reporting identity, health, settings, tasks
  apperror/              typed error model with stable codes reaching the UI as data
  approot/               data-root resolution under the OS data directory
  buildinfo/             product name, description and version
  logging/               structured JSON-lines logging with correlation fields
  settings/              typed settings registry over stored key/value rows
  store/                 SQLite open + numbered embedded migrations, failing closed
  tasks/                 durable task records, the source of truth over events
e2e/                     process-level harness: builds and drives the real binary over CDP
.github/workflows/      CI: gofmt, vet, backend tests, frontend, production build
frontend/
  src/                   React + TypeScript interface
  bindings/              generated from the Go services; committed
build/                   Wails build tree and Taskfiles, kept for packaging
Taskfile.yml             task entry points (invoked as `wails3 task ...`)
```

## Version

The version the interface shows comes from Go
(`internal/buildinfo.Version`), not from `package.json`. Release builds override
it at link time:

```sh
go build -ldflags "-X github.com/Zendevve/astradew/internal/buildinfo.Version=1.2.3"
```

A build without a release override reports `0.0.0`.
