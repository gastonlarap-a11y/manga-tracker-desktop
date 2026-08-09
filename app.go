package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"manga-tracker-desktop/internal/backend"
	"manga-tracker-desktop/internal/browsers"
	"manga-tracker-desktop/internal/installer"
	"manga-tracker-desktop/internal/payload"
	"manga-tracker-desktop/internal/servicecli"
	"manga-tracker-desktop/internal/syncurl"
)

// StoreURL is where the extension lives once Google approves it.
//
// Configuration rather than compiled-in behaviour: while the review is pending
// the settings screen shows the manual path beside it, and the day it is
// approved nothing here has to change for the one-click button to work.
const StoreURL = "https://chromewebstore.google.com/detail/acopmmaenbjdpcjcaiadcpdniomkikbd"

// App is the struct bound to the frontend: every exported method on it is
// callable from the window. Wiring only — the logic lives in internal/.
type App struct {
	ctx  context.Context
	deps installer.Deps
	// Resolved once at startup, so failing to even find a data directory is
	// reported through the window instead of crashing on the first click.
	setupErr string
	client   *http.Client
}

func NewApp() *App {
	app := &App{client: &http.Client{Timeout: 5 * time.Second}}
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

// InstallOutcome is what the window gets back.
//
// A refusal is not a failure and is not reported as one: "there is already an
// installation here" is the guard doing its job, and an error dialog would
// describe it as something going wrong. Errors stay for things that actually
// broke.
type InstallOutcome struct {
	installer.Result
	// Refused is "" when the install went through, otherwise a code the window
	// turns into a sentence: "running" or "installed".
	Refused string `json:"refused"`
}

// Install writes the bundled backend out and registers it with the system, so
// it starts on its own at every login from then on.
//
// It refuses when this machine already has Manga Tracker — running or merely
// registered. Overwriting a working service definition, or the data directory
// beside it, is not something a button should do by accident.
func (a *App) Install() (InstallOutcome, error) {
	result, err := a.deps.Install(a.ctx)
	switch {
	case errors.Is(err, installer.ErrAlreadyRunning):
		return InstallOutcome{Result: result, Refused: "running"}, nil
	case errors.Is(err, installer.ErrAlreadyInstalled):
		return InstallOutcome{Result: result, Refused: "installed"}, nil
	case err != nil:
		return InstallOutcome{}, err
	default:
		return InstallOutcome{Result: result}, nil
	}
}

// Settings is everything the configuration screen shows at once.
type Settings struct {
	// HasPayload is false in a development build, which carries no server. The
	// screen says so in its own words instead of showing the exec error that
	// asking a program that is not there produces.
	HasPayload     bool               `json:"hasPayload"`
	Installed      bool               `json:"installed"`
	Port           int                `json:"port"`
	DataDir        string             `json:"dataDir"`
	ExtensionDir   string             `json:"extensionDir"`
	Version        string             `json:"version"`
	SyncConfigured bool               `json:"syncConfigured"`
	Browsers       []browsers.Browser `json:"browsers"`
	StoreURL       string             `json:"storeUrl"`
	// Set when the service is there and still could not be asked — a real
	// fault, shown as technical detail under a sentence the window writes.
	Problem string `json:"problem"`
}

// Settings gathers the state of this installation.
func (a *App) Settings() Settings {
	settings := Settings{
		HasPayload:   payload.Available(),
		DataDir:      a.deps.DataDir,
		ExtensionDir: a.deps.ExtensionDir(),
		Version:      payload.Version(),
		Browsers:     browsers.Detect(),
		StoreURL:     StoreURL,
	}
	// Asking a service control that was never written out only produces an
	// exec error, which says nothing anyone can act on.
	if !settings.HasPayload {
		return settings
	}
	reply, err := a.service("status")
	if err != nil {
		settings.Problem = err.Error()
		return settings
	}
	settings.Installed = reply.Installed
	settings.Port = reply.Port
	settings.SyncConfigured = reply.SyncConfigured
	return settings
}

// SyncOutcome is what the screen reports after saving credentials.
type SyncOutcome struct {
	// Problem is a code the window turns into a sentence, so every message a
	// person reads is written in one place: "empty", "srv", "notMongo".
	Problem   string `json:"problem"`
	Connected bool   `json:"connected"`
	// LastError is the backend's own words about why it could not connect.
	LastError string `json:"lastError"`
}

// SetSync stores the user's own credentials and restarts the backend with them.
//
// Configuring this is optional and always was: with nothing set the library
// lives in the local SQLite file, there is no automatic sync and no button to
// trigger one.
func (a *App) SetSync(url string, database string) (SyncOutcome, error) {
	if problem := syncurl.Validate(url); problem != syncurl.None {
		return SyncOutcome{Problem: string(problem)}, nil
	}
	if _, err := a.service("set-sync", "--url", url, "--db", syncurl.Database(database)); err != nil {
		return SyncOutcome{}, err
	}

	// Restarting is asynchronous, so asking once would read the state of the
	// process on its way out.
	state := a.deps.Look(a.ctx)
	if state.BaseURL == "" {
		return SyncOutcome{}, nil
	}
	status, err := backend.WaitForSync(a.ctx, a.client, state.BaseURL, 20*time.Second)
	if err != nil {
		return SyncOutcome{}, err
	}
	return SyncOutcome{Connected: status.Connected, LastError: status.LastError}, nil
}

// ClearSync turns synchronising off and goes back to local-only.
func (a *App) ClearSync() error {
	_, err := a.service("clear-sync")
	return err
}

// OpenInBrowser opens the store listing in one specific browser — not the
// default one, which may not be the browser the extension is wanted in.
func (a *App) OpenInBrowser(id string) error {
	return browsers.Open(id, StoreURL)
}

// RevealExtension opens the folder to point "Load unpacked" at, for while the
// store review is pending.
func (a *App) RevealExtension() error {
	return browsers.Reveal(a.deps.ExtensionDir())
}

func (a *App) service(args ...string) (servicecli.Reply, error) {
	return a.deps.Call(a.ctx, a.deps.AppDir(), args...)
}
