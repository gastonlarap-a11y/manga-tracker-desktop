package main

import (
	"context"

	"manga-tracker-desktop/internal/installer"
)

// App is the struct bound to the frontend: every exported method on it is
// callable from the window. Wiring only — the logic lives in internal/.
type App struct {
	ctx context.Context
	// Resolved once at startup so a failure to even find a data directory is
	// reported through the window rather than crashing on the first click.
	deps     installer.Deps
	setupErr string
}

func NewApp() *App {
	app := &App{}
	dataDir, err := installer.DefaultDataDir()
	if err != nil {
		app.setupErr = err.Error()
		return app
	}
	app.deps = installer.Production(dataDir)
	return app
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// Look reports what the window should show: a backend it can display, an offer
// to install one, or a development build that carries none.
//
// Not finding a backend is a normal answer rather than an error — on a machine
// where nothing is installed yet it is the expected one.
func (a *App) Look() installer.State {
	if a.setupErr != "" {
		return installer.State{Kind: installer.KindNoPayload, Version: a.setupErr}
	}
	return a.deps.Look(a.ctx)
}

// Install writes the bundled backend out and registers it with the system, so
// it starts on its own at every login from then on.
//
// It refuses when a backend is already running: on a machine that already has
// one, overwriting its service definition is not something a button should do
// by accident.
func (a *App) Install() (installer.Result, error) {
	return a.deps.Install(a.ctx)
}

// ExtensionDir is the folder to point the browser's "Load unpacked" at while
// the Web Store review is pending.
func (a *App) ExtensionDir() string {
	return a.deps.ExtensionDir()
}
