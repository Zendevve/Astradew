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
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Zendevve/astradew/internal/app"
	"github.com/Zendevve/astradew/internal/apperror"
	"github.com/Zendevve/astradew/internal/buildinfo"
	"github.com/Zendevve/astradew/internal/logging"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	astradew := application.New(application.Options{
		Name:        buildinfo.Name,
		Description: buildinfo.Description,
		Services: []application.Service{
			application.NewServiceWithOptions(app.New(buildinfo.Name, buildinfo.Version), application.ServiceOptions{MarshalError: apperror.MarshalError}),
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
	if appLogger, err := logging.New(filepath.Join(application.Path(application.PathDataHome), "Astradew", "logs")); err != nil {
		log.Printf("logging: cannot create app logger: %v", err)
	} else {
		defer func() { _ = appLogger.Close() }()
		ctx := logging.WithOperationID(context.Background(), "startup")
		appLogger.Info(ctx, "starting", "product", buildinfo.Name, "version", buildinfo.Version)
	}

	if err := astradew.Run(); err != nil {
		log.Fatal(err)
	}
}
