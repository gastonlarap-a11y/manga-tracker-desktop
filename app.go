package main

import (
	"context"
	"net/http"
	"time"

	"manga-tracker-desktop/internal/backend"
)

// App is the struct bound to the frontend: every exported method on it is
// callable from the window. Wiring only — the logic lives in internal/.
type App struct {
	ctx context.Context
	// One client, reused: discovery opens up to ten connections in a row and a
	// fresh client per call would leak a connection pool each time.
	client *http.Client
}

func NewApp() *App {
	return &App{
		client: &http.Client{
			// Every probe already carries its own deadline; this is the outer
			// bound so a half-open socket cannot hang the window.
			Timeout: 5 * time.Second,
		},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// BackendStatus is what the window needs to decide what to show. It is a
// discriminated result in practice: BaseURL is non-empty exactly when Found is
// true, and empty otherwise.
type BackendStatus struct {
	Found bool `json:"found"`
	// Base URL of the running backend, e.g. http://127.0.0.1:5150.
	BaseURL string `json:"baseUrl"`
	// The window this app searched, so the UI can say where it looked instead
	// of just reporting failure.
	FirstPort int `json:"firstPort"`
	LastPort  int `json:"lastPort"`
}

// FindBackend looks for the manga-tracker-api service on this machine.
//
// Not finding it is a normal answer, not an error: on a machine where nothing
// is installed yet that is precisely the expected outcome, and the window turns
// it into an offer to install rather than into a failure.
func (a *App) FindBackend() BackendStatus {
	baseURL := backend.Discover(a.ctx, a.client)
	return BackendStatus{
		Found:     baseURL != "",
		BaseURL:   baseURL,
		FirstPort: backend.DefaultPort,
		LastPort:  backend.LastPort,
	}
}
