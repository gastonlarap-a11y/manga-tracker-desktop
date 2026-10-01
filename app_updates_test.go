package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"manga-tracker-desktop/internal/installer"
	"manga-tracker-desktop/internal/prefs"
)

// appWithReleases wires an App whose newest published release is `latest`,
// counting how many times GitHub's stand-in was asked.
func appWithReleases(t *testing.T, running, latest string) (*App, *atomic.Int32) {
	t.Helper()
	asked := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Add(1)
		_, _ = w.Write([]byte(`{"tag_name":"` + latest + `"}`))
	}))
	t.Cleanup(server.Close)
	return &App{
		ctx:            context.Background(),
		deps:           installer.Deps{DataDir: t.TempDir()},
		updateEndpoint: server.URL,
		buildVersion:   func() string { return running },
	}, asked
}

func TestCheckForUpdateAsksOncePerLaunch(t *testing.T) {
	t.Parallel()
	app, asked := appWithReleases(t, "v0.1.19", "v0.1.20")

	first := app.CheckForUpdate(false)
	second := app.CheckForUpdate(false)

	if first.State != "available" || first.Latest != "v0.1.20" || first.Current != "v0.1.19" {
		t.Fatalf("CheckForUpdate() = %+v", first)
	}
	if second != first {
		t.Errorf("second CheckForUpdate() = %+v, want the remembered %+v", second, first)
	}
	if asked.Load() != 1 {
		t.Errorf("GitHub asked %d times, want once per launch", asked.Load())
	}

	app.CheckForUpdate(true)
	if asked.Load() != 2 {
		t.Errorf("\"Buscar de nuevo\" did not ask again (%d requests)", asked.Load())
	}
}

func TestCheckForUpdateAsksNothingWhenTurnedOff(t *testing.T) {
	t.Parallel()
	app, asked := appWithReleases(t, "v0.1.19", "v0.1.20")
	if err := app.SetUpdateChecks(false); err != nil {
		t.Fatal(err)
	}

	got := app.CheckForUpdate(true)

	if got.State != UpdateOff {
		t.Errorf("State = %q, want %q", got.State, UpdateOff)
	}
	if asked.Load() != 0 {
		t.Error("GitHub was asked with the check turned off")
	}

	if err := app.SetUpdateChecks(true); err != nil {
		t.Fatal(err)
	}
	if got := app.CheckForUpdate(true); got.State != "available" {
		t.Errorf("turned back on, State = %q", got.State)
	}
}

// Unreadable preferences say nothing about whether the user allowed the
// check, so asking anyway would be guessing yes.
func TestCheckForUpdateAsksNothingWhenThePreferencesCannotBeRead(t *testing.T) {
	t.Parallel()
	app, asked := appWithReleases(t, "v0.1.19", "v0.1.20")
	if err := os.WriteFile(filepath.Join(app.deps.DataDir, "preferences.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := app.CheckForUpdate(false)

	if got.State != "unknown" || got.Problem == "" {
		t.Errorf("CheckForUpdate() = %+v, want unknown with a reason", got)
	}
	if asked.Load() != 0 {
		t.Error("GitHub was asked without knowing whether the user allowed it")
	}
}

func TestDismissUpdateRemembersTheReleaseItClosed(t *testing.T) {
	t.Parallel()
	app, _ := appWithReleases(t, "v0.1.19", "v0.1.20")

	if err := app.DismissUpdate("v0.1.20"); err != nil {
		t.Fatal(err)
	}
	if err := app.DismissUpdate("../../etc"); err == nil {
		t.Error("DismissUpdate accepted something that is not a version")
	}

	got := app.CheckForUpdate(false)
	if got.Dismissed != "v0.1.20" {
		t.Errorf("Dismissed = %q, want v0.1.20", got.Dismissed)
	}
	// Remembered beside the other preferences, not instead of them.
	stored, err := prefs.Load(app.deps.DataDir)
	if err != nil || stored.DismissedUpdate != "v0.1.20" {
		t.Errorf("stored = %+v, %v", stored, err)
	}
}

func TestOpenUpdatePageRefusesWithoutANewerRelease(t *testing.T) {
	t.Parallel()
	app, _ := appWithReleases(t, "v0.1.20", "v0.1.20")

	if err := app.OpenUpdatePage(); err == nil {
		t.Error("OpenUpdatePage() opened something before any check")
	}
	app.CheckForUpdate(false)
	if err := app.OpenUpdatePage(); err == nil {
		t.Error("OpenUpdatePage() opened something while up to date")
	}
}
