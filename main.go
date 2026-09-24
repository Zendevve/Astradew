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

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Zendevve/astradew/internal/app"
	"github.com/Zendevve/astradew/internal/apperror"
	"github.com/Zendevve/astradew/internal/approot"
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
	astradew := application.New(application.Options{
		Name:        buildinfo.Name,
		Description: buildinfo.Description,
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

	astradew.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            buildinfo.Name,
		Width:            1000,
		Height:           618,
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/",
	})
	if appLogger, err := logging.New(paths.Logs); err != nil {
		log.Printf("logging: cannot create app logger: %v", err)
	} else {
		svc.NoteLoggerCreated()
		defer func() { _ = appLogger.Close() }()
		ctx := logging.WithOperationID(context.Background(), "startup")
		appLogger.Info(ctx, "starting", "product", buildinfo.Name, "version", buildinfo.Version, "schema_version", db.Version(), "database", db.Path())
	}

	if err := astradew.Run(); err != nil {
		log.Fatal(err)
	}
}
