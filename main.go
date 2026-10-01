package main

import (
	"embed"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

// windowBackground is what shows behind the page while it loads and while the
// window is resized.
//
// On macOS it is fully transparent, so what shows is the window's own material
// (vibrancy, below) — which follows the system's light or dark appearance by
// itself. Elsewhere it is the dashboard's dark background, opaque: Windows
// treats any alpha but 0 as 255, and its window gets no material (Mica was left
// out until it can be checked on a real machine). The frontend moves it to the
// light colour when the system is light (frontend/src/theme.ts).
func windowBackground(goos string) *options.RGBA {
	if goos == "darwin" {
		return &options.RGBA{R: 0, G: 0, B: 0, A: 0}
	}
	return &options.RGBA{R: 17, G: 18, B: 24, A: 255}
}

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "Manga Tracker",
		Width:  1280,
		Height: 860,
		// The dashboard is a real layout with cards and a detail view; below
		// this it stops being usable rather than just cramped.
		MinWidth:  900,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: windowBackground(runtime.GOOS),
		// The page is transparent where it wants the window's material to show:
		// the status screens always, the dashboard once it has been told the
		// window is translucent (the embed greeting, frontend/src/App.tsx).
		Mac: &mac.Options{
			WebviewIsTransparent: true,
			WindowIsTranslucent:  true,
		},
		OnStartup: app.startup,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
