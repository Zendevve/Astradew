// Command astradew is the Astradew desktop application.
//
// It lives at the repository root because the frontend assets are embedded from
// frontend/dist and a //go:embed directive cannot traverse upward; every other
// package lives under internal/.
package main

import (
	"context"
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Zendevve/astradew/internal/app"
	"github.com/Zendevve/astradew/internal/apperror"
	"github.com/Zendevve/astradew/internal/approot"
	"github.com/Zendevve/astradew/internal/buildinfo"
	"github.com/Zendevve/astradew/internal/logging"
	"github.com/Zendevve/astradew/internal/store"
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
