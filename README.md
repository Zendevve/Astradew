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
| `go test ./...` | Backend tests. They run against the service layer directly, with no framework runtime and no webview, so they work on a fresh clone. |

## Layout

```text
main.go                  entry point; must stay at the root because it embeds frontend/dist
internal/                every other Go package
  app/                   the bound service reporting application identity
  buildinfo/             product name, description and version
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
