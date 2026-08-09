// Package installer turns a freshly downloaded app into a working tracker.
//
// It writes the embedded backend out, asks the bundled service control to
// register it with launchd or Task Scheduler, and waits until it answers. From
// then on the backend starts on its own at every login and this app is just a
// window onto it.
//
// The first thing it does is check whether a backend is already running, and
// stop there if one is. On the author's own machine that is a production
// LaunchAgent pointing at a source checkout, and a stray click must not
// overwrite it.
package installer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"manga-tracker-desktop/internal/backend"
	"manga-tracker-desktop/internal/payload"
	"manga-tracker-desktop/internal/servicecli"
)

// Kind is what the app shows. A union rather than a pile of booleans: "running
// and not installed" is not a state that should be expressible.
type Kind string

const (
	// KindRunning — a backend answered. Nothing to do.
	KindRunning Kind = "running"
	// KindInstallable — nothing answered, and this build carries a payload.
	KindInstallable Kind = "installable"
	// KindNoPayload — a development build. It cannot install, which is not a
	// fault, and saying so beats offering a button that fails.
	KindNoPayload Kind = "noPayload"
)

type State struct {
	Kind    Kind   `json:"kind"`
	BaseURL string `json:"baseUrl"`
	Version string `json:"version"`
}

// Result of an install that went through.
type Result struct {
	BaseURL string `json:"baseUrl"`
	Port    int    `json:"port"`
	DataDir string `json:"dataDir"`
	// False when the service registered but this account cannot start or stop
	// it — the Windows ACL case, surfaced instead of hidden.
	UserCanControlIt bool `json:"userCanControlIt"`
}

// Deps are the pieces the installer talks to. They are fields rather than
// direct calls so the orchestration — the part where the bugs live — has tests
// that never touch launchd, the Task Scheduler or the filesystem of whoever
// runs them.
type Deps struct {
	// Where the payload is written and where the database lives.
	DataDir string

	Discover  func(ctx context.Context) string
	Available func() bool
	Extract   func(dir string) error
	// Call runs the bundled service CLI from the extracted tree.
	Call func(ctx context.Context, appDir string, args ...string) (servicecli.Reply, error)
	// WaitHealthy blocks until the backend on that port answers, or the context ends.
	WaitHealthy func(ctx context.Context, port int) error
}

// Production wires the real implementations.
func Production(dataDir string) Deps {
	client := &http.Client{Timeout: 5 * time.Second}
	return Deps{
		DataDir:   dataDir,
		Discover:  func(ctx context.Context) string { return backend.Discover(ctx, client) },
		Available: payload.Available,
		Extract:   payload.Extract,
		Call: func(ctx context.Context, appDir string, args ...string) (servicecli.Reply, error) {
			return servicecli.Client{
				BunPath:    filepath.Join(appDir, interpreter()),
				ScriptPath: filepath.Join(appDir, "service.js"),
			}.Call(ctx, args...)
		},
		WaitHealthy: func(ctx context.Context, port int) error {
			return waitHealthy(ctx, client, port)
		},
	}
}

func interpreter() string {
	if runtime.GOOS == "windows" {
		return "bun.exe"
	}
	return "bun"
}

// AppDir is where the backend ends up, and what the service CLI is given.
//
// Under `runtime/`, not directly in the data directory: that is where the
// database and its backups live, and the payload's own directory is the one
// thing extraction is allowed to delete.
func (d Deps) AppDir() string {
	return filepath.Join(payload.RuntimeDir(d.DataDir), payload.AppSubdir)
}

// ExtensionDir is the folder to point "Load unpacked" at while the store
// review is pending.
func (d Deps) ExtensionDir() string {
	return filepath.Join(payload.RuntimeDir(d.DataDir), payload.ExtensionSubdir)
}

// Prepare writes the payload out if this build carries one.
//
// Called at startup rather than only from Install, because the service control
// lives inside the payload: until it is on disk, nothing can ask the machine
// whether a service is installed, and the settings screen showed every question
// as "not installed". Idempotent and quick — 132 ms the first time, microseconds
// afterwards.
func (d Deps) Prepare() error {
	if !d.Available() {
		return nil
	}
	return d.Extract(d.DataDir)
}

// Look reports what the app should offer.
func (d Deps) Look(ctx context.Context) State {
	if baseURL := d.Discover(ctx); baseURL != "" {
		return State{Kind: KindRunning, BaseURL: baseURL, Version: payload.Version()}
	}
	if !d.Available() {
		return State{Kind: KindNoPayload, Version: payload.Version()}
	}
	return State{Kind: KindInstallable, Version: payload.Version()}
}

// ErrAlreadyRunning is returned rather than silently reinstalling.
var ErrAlreadyRunning = errors.New("a backend is already running on this machine")

// ErrAlreadyInstalled covers the case ErrAlreadyRunning misses: a service that
// is registered but stopped.
//
// Looking only for something that answers is not enough. Stopping the service
// is exactly what someone does in order to try the installer, and at that
// moment the machine still has a service definition pointing at their real
// setup — overwriting it is not something a button should do.
var ErrAlreadyInstalled = errors.New("this machine already has Manga Tracker installed")

// Install extracts the payload and registers the service.
func (d Deps) Install(ctx context.Context) (Result, error) {
	// Checked again here, not only in Look: the two are separated by however
	// long the window sat open before someone pressed the button.
	if baseURL := d.Discover(ctx); baseURL != "" {
		return Result{BaseURL: baseURL}, ErrAlreadyRunning
	}
	if !d.Available() {
		return Result{}, payload.ErrNoPayload
	}

	// Safe to do before the check below: extraction only ever writes inside its
	// own runtime/ directory, and the service control it asks next lives there.
	if err := d.Extract(d.DataDir); err != nil {
		return Result{}, err
	}

	// A registered but stopped service is still an installation. Asked after
	// extracting because the program that answers this is part of the payload.
	if status, err := d.Call(ctx, d.AppDir(), "status"); err == nil && status.Installed {
		return Result{Port: status.Port}, ErrAlreadyInstalled
	}

	reply, err := d.Call(ctx, d.AppDir(), "install", "--app-dir", d.AppDir(), "--data-dir", d.DataDir)
	if err != nil {
		return Result{}, err
	}
	if reply.Port == 0 {
		return Result{}, fmt.Errorf("the service was registered but reported no port")
	}

	// Registered is not the same as running: launchd and Task Scheduler both
	// return before the process is listening, and a window that opens on a
	// dead port is indistinguishable from a broken install.
	if err := d.WaitHealthy(ctx, reply.Port); err != nil {
		return Result{}, fmt.Errorf("the service was registered but never answered: %w", err)
	}

	return Result{
		BaseURL:          fmt.Sprintf("http://127.0.0.1:%d", reply.Port),
		Port:             reply.Port,
		DataDir:          d.DataDir,
		UserCanControlIt: reply.UserCanControlIt,
	}, nil
}

func waitHealthy(ctx context.Context, client *http.Client, port int) error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		if found := backend.DiscoverPort(ctx, client, port); found {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("timed out after 30s")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// DefaultDataDir is where an installed copy keeps the backend and its database.
func DefaultDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("could not find a place to install into: %w", err)
	}
	return filepath.Join(base, "MangaTracker"), nil
}
