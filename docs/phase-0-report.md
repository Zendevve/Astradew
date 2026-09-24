# Phase 0 report — foundation acceptance (#13)

Ticket #13 closes Phase 0 (#1) with the acceptance criterion demonstrated by
machine and the development documentation made accurate. This report records
what was implemented per ticket, what was verified, what was not, known
limitations, the parent-spec acceptance mapping, and the placeholder sweep.

## What was implemented (#2–#13, one line each)

- #2 Walking skeleton — pinned Go 1.25 toolchain + Wails v3.0.0-beta.25, root
  `main.go` (asset-embed constraint), bound identity service, runnable window.
  Key files: `go.mod`, `main.go`, `internal/app/service.go`,
  `internal/buildinfo/`, `frontend/src/App.tsx`, `frontend/bindings/`.
- #3 Typed errors — `apperror` codes declared once in Go, generated into the
  frontend, marshalled via the per-service `MarshalError` hook; the UI branches
  on codes, and the Diagnostics panel demonstrates the failure path with the
  `ProbeFailure` probe. Key files: `internal/apperror/`, `frontend/src/errors.*`,
  `frontend/src/App.tsx` (probe), `internal/app/service.go` (`ProbeFailure`).
- #4 Data root — single `Astradew/` root resolved through the framework path
  helper (Windows Local, never Roaming), full product layout created on demand,
  writability probed, refusal via typed `APPROOT_UNUSABLE`. Key files:
  `internal/approot/`, `internal/app/service.go` (`Health`, `Paths`).
- #5 Structured logging — JSON-lines rotating file under the data root plus
  readable console output; correlation fields travel through
  `context.Context`; secrets never emitted. Key files: `internal/logging/`,
  `main.go` (logger wiring, startup operation id).
- #6 Database — `astradew.db` opened with FK/WAL/busy-timeout, numbered
  embedded SQL migrations applied in order inside transactions, recorded in
  `schema_migrations`, pre-migration backup, rollback + typed refusal with two
  repair actions on failure. Key files: `internal/store/`
  (`store.go`, `migrations/0001_*.sql`).
- #7 Settings — typed registry (`theme`, `ui-scale`, `check-updates-on-start`)
  over key/value JSON-scalar rows; defaults on never-written keys; unknown keys
  preserved byte-untouched; invalid values refused with `SETTING_INVALID`
  leaving the stored value. Key files: `internal/settings/`,
  `internal/app/service.go` (`Settings`/`GetSetting`/`SetSetting`),
  `frontend/src/pages/SettingsSection.tsx`.
- #8 Durable tasks — task rows (status, operation, progress, message,
  timestamps, outcome) as the source of truth; events are hints only; UI
  re-reads on mount; startup initialisation recorded as the first durable run.
  Key files: `internal/tasks/`, `main.go` (startup Create→Succeed),
  `frontend/src/pages/StartupTaskSection.tsx`.
- #9 Application shell — hash-routed nav (Dashboard, Library, Profiles,
  Updates, Health, Downloads, Logs, Settings), honest empty states, accessible
  primitives, visible focus, no colour-only status, disabled-with-reason top
  controls, bindings-stubbed frontend tests. Key files: `frontend/src/App.tsx`,
  `frontend/src/pages/routes.tsx`, `frontend/src/ui/`, `frontend/src/test/`.
- #10 Health view — backend-observed version, data root, per-directory
  writability, live database state + schema version, startup steps, concrete
  findings with actions; unobservable capabilities listed as unavailable, never
  passing. Key files: `internal/app/service.go` (`Health`),
  `frontend/src/pages/HealthSection.tsx`.
- #11 E2E harness — ATTACH verdict (proven green, not re-litigated here): built
  binary launched with isolated data home + remote-debugging port, CDP attach,
  DOM assertions of Go-originated values (identity, Health report). Key files:
  `e2e/e2e_test.go`, `main.go` (env-gated debugging block).
- #12 CI — Windows + Linux on push/PR: gofmt, vet, backend tests, frontend
  check/tests/production build, production app build; headless, never runs the
  binary. Key file: `.github/workflows/ci.yml` (owned by #12, untouched here).
- #13 This ticket — restart acceptance test, README accuracy, this report,
  placeholder sweep. Files: `e2e/e2e_test.go`
  (`TestProductionAppRestartsWithSettingPersisted` + `restartPort` 19313 +
  `evaluateInPage`/`assertRenderedPageText`/`startupUpdatedAt`/
  `waitForNoDefaultMarker` helpers + variadic first-launch evidence on
  `launchApp`/`waitForPageTarget`), `README.md`, `docs/phase-0-report.md`.

## What was verified by machine

| Gate | Command | Result (this machine, 2026-09-24) |
| --- | --- | --- |
| gofmt | `gofmt -l main.go internal/` | clean (empty output) |
| gofmt (e2e) | `gofmt -l e2e/` | lists `e2e\e2e_test.go`: one missing blank line after `TestProductionAppRendersHealthReport` — left for the orchestrator's format pass per ticket rules ("No gofmt") |
| go vet | `go vet . ./internal/... ./e2e/` | exit 0 |
| backend tests | `go test ./internal/... -count=1` | 62 `--- PASS`, 0 FAIL; 1 SKIP (`TestResolveRefusesUnwritableBase` — Windows ignores dir write bits, covered by the is-a-file refusal test instead; see limitations) |
| TypeScript | `npx tsc --noEmit` (frontend/) | exit 0 (frontend untouched by #13) |
| frontend tests | `npx vitest run` (frontend/) | 2 files, 22 tests, all passed |
| frontend build | `npm run build` (frontend/) | exit 0 (`dist/` assets rebuilt) |
| production build | `wails3 build` | exit 0 (`bin/astradew.exe`) |
| e2e full suite, run 1 | `go test ./e2e/ -count=1` | all 3 PASS in 9.5s (Identity 1.9s, Health 2.6s, Restart 3.9s) |
| e2e full suite, run 2 | `go test ./e2e/ -count=1` | all 3 PASS in 10.1s (Identity 1.9s, Health 2.5s, Restart 4.4s) |

The new test `TestProductionAppRestartsWithSettingPersisted` additionally
passed solo before the two consecutive full-suite greens. Each step fails via
`t.Fatalf` (non-zero exit); both launches `defer`/`killApp` on all paths; all
second-launch failures carry the first-launch evidence string.

## What was NOT verified

- Linux CI runner outcome — the workflow file is #12's; this machine is
  Windows. The pipeline result on `ubuntu-latest` runners was not observed
  here.
- macOS — out of scope for Phase 0 CI by parent-spec decision (joins at
  packaging/notarisation).
- Game/SMAPI paths — out of scope for Phase 0 (no game detection, mod
  scanning, manifests, archives, transactions, profiles deployment, launch,
  update engine, Nexus, packaging). The Health view's "Not yet available"
  section names them honestly.
- `go test ./...` as a whole — `build/ios` carries a pre-existing
  main-package issue and stays excluded; the documented gate is
  `go build . ./internal/...` / `go vet . ./internal/...` /
  `go test ./internal/...`, never `./...`.
- Fresh-clone setup from zero (clone → install → `wails3 dev`) — documented in
  the README from the existing working setup, not re-exercised end to end on a
  bare machine here.

## Known limitations

- #4 unwritable-parent refusal skips on Windows
  (`TestResolveRefusesUnwritableBase`): Windows ignores the read-only bit on
  directories for write access, so a chmod-based test cannot deny anything. The
  test probes first and skips with that reason; the refusal path stays covered
  by `TestResolveRefusesBaseThatIsAFile`. Not a product gap — the probe still
  refuses genuinely unwritable roots at runtime.
- The e2e harness needs an interactive Windows session, a WebView2 runtime,
  and free loopback ports 19311–19313. It never runs in CI by design.
- `build/ios` pre-existing main-package issue: excluded from build/vet/test
  gates exactly as the repo (and CI) already exclude it.
- `gofmt -l e2e/` flags one blank-line nit in the new test (see gate table);
  `main.go` + `internal/` are clean. Left for the orchestrator's format pass —
  #13 rules forbid running gofmt here.
- The no-remigrate signal is mtime-plus-version-plus-record-growth, by design:
  migrations are idempotent (applied versions recorded in `schema_migrations`;
  reaching the same version twice is a no-op), so a version line alone cannot
  distinguish "reused" from "re-applied". The test asserts version still 1,
  the startup record advanced to a strictly later `updated` timestamp from the
  same store, and the db file mtime did not go backwards.

## Parent-spec (#1) acceptance mapping

Problem statement — resolved: there is an application, a database, persisted
choices, and machine evidence the stack holds (e2e greens above).

- User stories 1–5 (window, identity, nav, honest empty states, Health truth):
  demonstrated — e2e asserts identity + Health DOM; frontend tests assert
  routes render and empty states carry no sample content.
- Story 6 (setting survives restart): demonstrated — the new e2e test changes
  `theme` through the interface and reads `dark` back after a real restart.
- Stories 7–9 (refuse to start, never silently rebuild, two repair actions):
  demonstrated — `store.Open` failure semantics + `APPROOT_UNUSABLE` refusal,
  covered by service/store/approot tests asserting refusal and repair text.
- Stories 10–11 (one predictable root, Local never Roaming on Windows):
  demonstrated — approot tests including `TestDataHomeIsLocalNeverRoaming`;
  Health DOM shows the resolved root (e2e asserts it).
- Stories 12–14 (log file, correlation, no secrets): demonstrated — logging
  tests assert rotation/bounds, operation-id propagation, and secret redaction.
- Stories 15–17 (visible first run, durable outcomes, responsive shell):
  demonstrated — startup task Create→Succeed recorded durably, UI re-reads on
  mount, e2e asserts "Startup task succeeded" rendered on both launches.
- Stories 18–20 (pinned framework/toolchain, root entry layout): demonstrated
  — `go.mod` pins, `main.go` at root, builds green.
- Stories 21–26 (created/migrated db, numbered embedded ordered atomic
  migrations, pre-risk backup, fresh + fixture upgrade paths, pragmas in
  effect, rollback + refuse): demonstrated — store tests cover fresh/upgrade/
  rollback/backup/pragma paths; e2e proves creation + migration on first start.
- Stories 27–28 (declared settings, unknown keys preserved): demonstrated —
  settings tests (defaults, round-trip, foreign-row preservation) plus the e2e
  restart proof.
- Stories 29–30 (typed errors as data, codes declared once + generated):
  demonstrated — apperror marshalling tests + probe surface in the UI.
- Stories 31–32 (task records, events as hints): demonstrated — tasks tests
  including re-read after reopen; UI mounts re-read.
- Story 33 (correlation fields through context): demonstrated — logging tests.
- Stories 34–36 (bindings-only access, routing + primitives once, settings
  writes through service→db): demonstrated — frontend tests stub the bindings
  module; SettingsSection writes via `SetSetting` (e2e drives exactly this).
- Stories 37–38 (dev loop, production build): demonstrated — `wails3 dev`
  documented; `wails3 build` green here.
- Stories 39–40 (service-layer tests on real temp db; frontend tests stub
  bindings): demonstrated — the suites above.
- Story 41 (e2e harness proving start→change→restart→read-back): demonstrated
  — `TestProductionAppRestartsWithSettingPersisted`, 2 consecutive full-suite
  greens.
- Stories 42–44 (CI format/vet/tests/builds on Windows+Linux; frontend in the
  same pipeline; no game dependency): implemented per #12's workflow (this
  ticket does not own `.github/`); local equivalents all green here except the
  unobserved Linux runner outcome (recorded above as not verified).
- Story 45 (no placeholders): demonstrated — sweep below, zero hits.
- Story 46 (only implemented services registered): demonstrated — the bound
  surface is ApplicationService only; later-phase services are absent, never
  stubbed.
- Stories 47–48 (PRD as record, ADRs binding): process constraints honoured —
  `docs/product/`, `docs/adr/` untouched by #13; no deviation introduced.
- Stories 49–50 (setup docs, highest-boundary seams): demonstrated — README
  corrected here; seams are service layer / bindings module / process harness.

Implementation decisions — all hold as specified: exact framework pin,
toolchain pin, root entry point, Local data home on Windows, only Application
+ Settings + diagnostics-source services, registry-not-migration settings with
unknown-key preservation, numbered embedded transactional migrations with
backup and fail-closed refusal, typed errors via the verified marshaller,
tasks-as-truth with replay-less events and startup as the first task,
context-carried correlation fields, bindings-only frontend access, and the four
test seams in order. No deviation.

Out of scope — respected: no game/SMAPI/mod/archive/transaction/profile/
launch/health-engine/update/Nexus/packaging work; no interface beyond
navigation, empty states, Health, and Settings; no portable mode; no macOS or
Linux packaging; no mod installation of any kind.

## Placeholder sweep (2026-09-24)

`TODO|FIXME|placeholder|lorem|sample-data|SampleData|SAMPLE_DATA` over
`internal/` + `frontend/src` + `main.go` + `e2e/`:

- `internal/*/…_test.go`: `context.TODO()` only — the stdlib
  zero-Context constructor, not a placeholder. Not a placeholder (no action).
- `frontend/src/App.tsx` (search input) and `frontend/src/pages/routes.tsx`
  (doc comment): the word `placeholder` appears as the HTML input's
  `placeholder="Search"` attribute on a deliberately disabled control and as a
  comment stating empty states contain no placeholder numbers. Not
  placeholders (no action).
- `internal/app/service.go` `ProbeFailure` ("failing demo probe"): a
  deliberate typed-error probe exercising the failure path end to end per #3's
  acceptance criteria ("one interface surface exercises the failure path"),
  with a typed `PROBE_FAILURE` code — not a placeholder (no action).
- Zero `FIXME`, zero `lorem`, zero `sample-data` hits anywhere in scope.

No placeholder implementation remains in the completed work.
