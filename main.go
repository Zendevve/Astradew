// Command astradew is the Astradew desktop application.
//
// It lives at the repository root because the frontend assets are embedded from
// frontend/dist and a //go:embed directive cannot traverse upward; every other
// package lives under internal/.
package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Zendevve/astradew/internal/app"
	"github.com/Zendevve/astradew/internal/apperror"
	"github.com/Zendevve/astradew/internal/approot"
	"github.com/Zendevve/astradew/internal/archive"
	"github.com/Zendevve/astradew/internal/buildinfo"
	"github.com/Zendevve/astradew/internal/logging"
	"github.com/Zendevve/astradew/internal/store"
	"github.com/Zendevve/astradew/internal/tasks"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// The data root comes before everything else: nothing may open (logs,
	// windows, database) until the root is resolved and proven writable.
	// On failure refuse startup loudly with the typed error — never
	// continue as if it succeeded, never silently repair.
	paths, err := approot.Resolve()
	if err != nil {
		log.Fatalf("APPROOT_UNUSABLE: refusing startup: %v", err)
	}

	// The database opens right after the root: no window, logger, or
	// service may run against an unmigrated or corrupt schema. On failure
	// refuse startup loudly — the typed error already names the failing
	// version, the database path, and both repair actions — never continue
	// as if it succeeded, never silently repair.
	db, err := store.Open(paths)
	if err != nil {
		log.Fatalf("refusing startup: %v", err)
	}
	defer func() { _ = db.Close() }()

	// The durable startup record starts at Open: task rows cannot exist
	// before migration, so anything earlier (root resolution) is log-only
	// via the logging package and never fabricated as a row. This
	// Create→Succeed pair records the visible first run — root resolved
	// plus schema version — as the source of truth per ADR 0006; Wails
	// events are live hints only and nothing here emits one. A write
	// failure is logged, never fatal: startup already refused loudly
	// above when the database itself was unusable.
	startupTasks := tasks.New(db.DB())
	startupCtx := logging.WithOperationID(context.Background(), "startup")
	if startupTask, err := startupTasks.Create(startupCtx, "startup"); err != nil {
		log.Printf("tasks: cannot record startup task: %v", err)
	} else {
		startupCtx = logging.ContextWithTask(startupCtx, startupTask.ID)
		if err := startupTasks.UpdateProgress(startupCtx, startupTask.ID, 1, 2, "root resolved at "+paths.Root); err != nil {
			log.Printf("tasks: cannot record startup progress: %v", err)
		}
		if err := startupTasks.Succeed(startupCtx, startupTask.ID, fmt.Sprintf("startup complete at schema version %d", db.Version())); err != nil {
			log.Printf("tasks: cannot complete startup task: %v", err)
		}
	}

	svc := app.NewWithPathsAndStore(buildinfo.Name, buildinfo.Version, paths, db)
	// Remote debugging is strictly opt-in through the environment and exists
	// for the end-to-end harness only: ordinary launches leave the debugger
	// port closed. WebView2 honours additional browser arguments passed here
	// in code, while Wails clears the WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS
	// environment override at startup — so there is no way to enable this
	// without this explicit block. The value is restricted to a bare port
	// number so the variable cannot inject further browser arguments.
	windows := application.WindowsOptions{}
	if arg := os.Getenv("ASTRADEW_REMOTE_DEBUGGING_PORT"); arg != "" {
		if port, err := strconv.Atoi(arg); err == nil && port > 0 && port < 65536 {
			windows.AdditionalBrowserArgs = []string{"--remote-debugging-port=" + strconv.Itoa(port)}
		} else {
			log.Printf("ignoring invalid ASTRADEW_REMOTE_DEBUGGING_PORT %q: want a TCP port number", arg)
		}
	}
	astradew := application.New(application.Options{
		Name:        buildinfo.Name,
		Description: buildinfo.Description,
		Windows:     windows,
		Services: []application.Service{
			application.NewServiceWithOptions(svc, application.ServiceOptions{MarshalError: apperror.MarshalError}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	// Task events travel through this sink: Wails live hints only, never a
	// correctness path (ADR 0006) — the durable task rows carry the same
	// state and the interface re-reads them when it mounts. The sink exists
	// only once the application does, so it is injected here rather than at
	// construction.
	svc.SetEventSink(astradew.Event)
	astradew.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            buildinfo.Name,
		Width:            1000,
		Height:           618,
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/",
	})
	// The app logger is created while nothing has run yet: the window object
	// exists but opens at Run, so every startup step below is recorded. A nil
	// appLogger is the honest "it could not be created" of this block, and the
	// sweep below still reports through the standard logger.
	appLogger, loggerErr := logging.New(paths.Logs)
	if loggerErr != nil {
		log.Printf("logging: cannot create app logger: %v", loggerErr)
	} else {
		svc.NoteLoggerCreated()
		defer func() { _ = appLogger.Close() }()
		ctx := logging.WithOperationID(context.Background(), "startup")
		appLogger.Info(ctx, "starting", "product", buildinfo.Name, "version", buildinfo.Version, "schema_version", db.Version(), "database", db.Path())
	}

	// A crash leaves its inspection stage behind, and the stage layout has
	// exactly one owner (internal/archive). Sweeping before the window opens
	// returns the space and means a leftover can never be mistaken for this
	// run's stage.
	sweepStages(paths.Temp, appLogger)

	if err := astradew.Run(); err != nil {
		log.Fatal(err)
	}
}

// sweepStages removes the inspection stages a previous run left behind: the
// layout has exactly one owner (internal/archive), and a crash is the one
// exit that cannot clean up after itself. It runs before the window opens, so
// a leftover can never be mistaken for this run's stage, and it reports the
// count through both the standard logger and — when it exists at this point —
// the application logger. A sweep that fails is logged, never fatal: a stuck
// stage is worth less than a startup that refuses to run.
func sweepStages(tempRoot string, appLogger *logging.Logger) {
	removed, err := archive.SweepStages(tempRoot)
	if err != nil {
		log.Printf("archive: swept %d leftover inspection stage(s), with errors: %v", removed, err)
	} else {
		log.Printf("archive: swept %d leftover inspection stage(s)", removed)
	}
	if appLogger == nil {
		return
	}
	ctx := logging.WithOperationID(context.Background(), "startup")
	if err != nil {
		appLogger.Error(ctx, "inspection stage sweep incomplete", "stages_removed", removed, "error", err.Error())
		return
	}
	appLogger.Info(ctx, "swept inspection stages", "stages_removed", removed)
}
