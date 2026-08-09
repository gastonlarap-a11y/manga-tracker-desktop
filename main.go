package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

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
		// Matches the dashboard's own background, so resizing does not flash a
		// different colour behind the frame it is shown in.
		BackgroundColour: &options.RGBA{R: 15, G: 17, B: 21, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
