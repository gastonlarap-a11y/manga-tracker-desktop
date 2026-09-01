package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"manga-tracker-desktop/internal/installer"
	"manga-tracker-desktop/internal/reach"
	"manga-tracker-desktop/internal/servicecli"
)

// backendReporting stands in for the running server: it answers the one
// endpoint the sync screen reads, with a connection that is up or down.
func backendReporting(t *testing.T, connected bool, lastError string) string {
	t.Helper()
	body := `{"enabled":true,"connected":true,"lastSyncAt":"2026-08-21T02:16:55.226Z","lastError":null}`
	if !connected {
		body = `{"enabled":true,"connected":false,"lastSyncAt":null,"lastError":{"message":"` + lastError + `"}}`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sync/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// serviceCalls records every command sent to the service control, which is how
// these tests assert that a credential was — or was not — moved.
type serviceCalls struct {
	commands []string
	reply    servicecli.Reply
}

func (s *serviceCalls) call(_ context.Context, _ string, args ...string) (servicecli.Reply, error) {
	s.commands = append(s.commands, strings.Join(args, " "))
	return s.reply, nil
}

func (s *serviceCalls) ran(command string) bool {
	for _, got := range s.commands {
		if got == command || strings.HasPrefix(got, command+" ") {
			return true
		}
	}
	return false
}

// appFor wires an App whose backend, service control and network are all
// answered by the test.
func appFor(t *testing.T, baseURL string, calls *serviceCalls, dial reach.DialContext) *App {
	t.Helper()
	return &App{
		ctx:    context.Background(),
		client: &http.Client{Timeout: 2 * time.Second},
		deps: installer.Deps{
			DataDir:   t.TempDir(),
			Discover:  func(context.Context) string { return baseURL },
			Available: func() bool { return true },
			Call:      calls.call,
		},
		lookupHost:  func(context.Context, string) ([]string, error) { return []string{"203.0.113.7"}, nil },
		dialContext: dial,
	}
}

func dialTimesOut(context.Context, string, string) (net.Conn, error) {
	return nil, &net.OpError{Op: "dial", Net: "tcp", Err: context.DeadlineExceeded}
}

func dialSucceeds(t *testing.T) reach.DialContext {
	t.Helper()
	return func(context.Context, string, string) (net.Conn, error) {
		ours, theirs := net.Pipe()
		t.Cleanup(func() { _ = theirs.Close() })
		return ours, nil
	}
}

// The regression this whole change exists for.
//
// A cluster whose allowlist no longer held this machine's address produced a
// sync that would not come up, and the fallback read that as a keystore it
// could not be blamed on: it took a working credential out of the Keychain and
// wrote it in plaintext into the service's configuration, on a machine whose
// keystore was fine. Nothing about where the password lives could have fixed a
// connection that never reached the server.
func TestAwaitSyncOrFallBackKeepsTheCredentialWhenTheStoreIsUnreachable(t *testing.T) {
	t.Parallel()

	calls := &serviceCalls{reply: servicecli.Reply{
		OK: true, Installed: true, SyncConfigured: true,
		SyncHost: "db.example.com:10260",
	}}
	baseURL := backendReporting(t, false, "Server selection timed out after 15000 ms")
	app := appFor(t, baseURL, calls, dialTimesOut)

	outcome, err := app.awaitSyncOrFallBack()
	if err != nil {
		t.Fatalf("awaitSyncOrFallBack() error = %v", err)
	}

	if calls.ran("pin-config-secret") {
		t.Error("the credential was written into the service configuration over a network failure")
	}
	if outcome.Reach != string(reach.Unreachable) {
		t.Errorf("Reach = %q, want %q", outcome.Reach, reach.Unreachable)
	}
	if !outcome.Settled || outcome.Connected {
		t.Errorf("outcome = %+v, want a settled, unconnected answer", outcome)
	}
	if outcome.SecretInConfig {
		t.Error("SecretInConfig = true, want the credential left in the keystore")
	}
}

// The case the fallback was written for still has to work: the path is open, so
// a credential the service cannot read is a real explanation.
func TestAwaitSyncOrFallBackStillPinsWhenTheNetworkIsFine(t *testing.T) {
	t.Parallel()

	calls := &serviceCalls{reply: servicecli.Reply{
		OK: true, Installed: true, SyncConfigured: true,
		SyncHost:       "db.example.com:10260",
		SecretInConfig: true,
	}}
	baseURL := backendReporting(t, false, "Authentication failed")
	app := appFor(t, baseURL, calls, dialSucceeds(t))

	outcome, err := app.awaitSyncOrFallBack()
	if err != nil {
		t.Fatalf("awaitSyncOrFallBack() error = %v", err)
	}

	if !calls.ran("pin-config-secret") {
		t.Error("the fallback was not attempted on a machine that could reach its store")
	}
	if outcome.Reach != string(reach.Reachable) {
		t.Errorf("Reach = %q, want %q", outcome.Reach, reach.Reachable)
	}
	if !outcome.SecretInConfig {
		t.Error("SecretInConfig = false, want the fallback reported")
	}
}

// A probe that could not be carried out is not permission to weaken anything.
func TestAwaitSyncOrFallBackKeepsTheCredentialWhenTheProbeFoundNothingOut(t *testing.T) {
	t.Parallel()

	calls := &serviceCalls{reply: servicecli.Reply{OK: true, Installed: true, SyncHost: "db.example.com:10260"}}
	baseURL := backendReporting(t, false, "Server selection timed out after 15000 ms")
	app := appFor(t, baseURL, calls, func(context.Context, string, string) (net.Conn, error) {
		return nil, &net.OpError{Op: "dial", Err: syscall.ENETUNREACH}
	})

	outcome, err := app.awaitSyncOrFallBack()
	if err != nil {
		t.Fatalf("awaitSyncOrFallBack() error = %v", err)
	}
	if calls.ran("pin-config-secret") {
		t.Error("the credential was moved on a machine that had no network at all")
	}
	if outcome.Reach != string(reach.Unknown) {
		t.Errorf("Reach = %q, want %q", outcome.Reach, reach.Unknown)
	}
}

// A sync that connects is never a reason to touch the credential.
func TestAwaitSyncOrFallBackLeavesAWorkingSyncAlone(t *testing.T) {
	t.Parallel()

	calls := &serviceCalls{reply: servicecli.Reply{OK: true, Installed: true}}
	app := appFor(t, backendReporting(t, true, ""), calls, dialTimesOut)

	outcome, err := app.awaitSyncOrFallBack()
	if err != nil {
		t.Fatalf("awaitSyncOrFallBack() error = %v", err)
	}
	if !outcome.Connected {
		t.Errorf("outcome = %+v, want a connected answer", outcome)
	}
	if calls.ran("pin-config-secret") {
		t.Error("the credential was moved on a sync that had connected")
	}
}

// Retrying must never be a reason to move a password somewhere weaker: someone
// pressing it is asking to try again, not to change where anything is stored.
func TestRetrySyncNeverPinsTheCredential(t *testing.T) {
	t.Parallel()

	calls := &serviceCalls{reply: servicecli.Reply{OK: true, Installed: true, SyncHost: "db.example.com:10260"}}
	baseURL := backendReporting(t, false, "Authentication failed")
	app := appFor(t, baseURL, calls, dialSucceeds(t))

	outcome, err := app.RetrySync()
	if err != nil {
		t.Fatalf("RetrySync() error = %v", err)
	}
	if !calls.ran("restart") {
		t.Errorf("commands = %v, want a restart among them", calls.commands)
	}
	if calls.ran("pin-config-secret") {
		t.Error("a retry moved the credential into the service configuration")
	}
	if outcome.Reach != string(reach.Reachable) {
		t.Errorf("Reach = %q, want the diagnosis carried back", outcome.Reach)
	}
}

func TestRetrySyncReportsAConnectionThatCameBack(t *testing.T) {
	t.Parallel()

	calls := &serviceCalls{reply: servicecli.Reply{OK: true, Installed: true}}
	app := appFor(t, backendReporting(t, true, ""), calls, dialTimesOut)

	outcome, err := app.RetrySync()
	if err != nil {
		t.Fatalf("RetrySync() error = %v", err)
	}
	if !outcome.Connected {
		t.Errorf("outcome = %+v, want a connected answer", outcome)
	}
	// Nothing to diagnose on a connection that works, and the probe would have
	// timed out on this dialler if it had been run.
	if outcome.Reach != "" {
		t.Errorf("Reach = %q, want it left empty", outcome.Reach)
	}
}
