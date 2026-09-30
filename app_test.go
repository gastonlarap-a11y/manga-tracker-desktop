package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
		lookupHost: func(context.Context, string) ([]string, error) { return []string{"203.0.113.7"}, nil },
		// No name here has an SRV record, so a test never reaches real DNS.
		lookupSRV: func(context.Context, string, string, string) (string, []*net.SRV, error) {
			return "", nil, &net.DNSError{Err: "no such host", IsNotFound: true}
		},
		dialContext: dial,
	}
}

// atlasSeed is what the service control reports as the sync host of a
// resolved mongodb+srv:// address — the shape every Atlas cluster has.
const atlasSeed = "ac-a-shard-00-00.x.mongodb.net:27017,ac-a-shard-00-01.x.mongodb.net:27017,ac-a-shard-00-02.x.mongodb.net:27017"

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

// On an Atlas cluster the fallback never ran at all: its seed list read as an
// unparsable host, so the probe answered Unknown and a Windows service that
// could not read its keystore never got the credential it needed.
func TestAwaitSyncOrFallBackPinsForAReachableAtlasCluster(t *testing.T) {
	t.Parallel()

	calls := &serviceCalls{reply: servicecli.Reply{
		OK: true, Installed: true, SyncConfigured: true,
		SyncHost:       atlasSeed,
		SecretInConfig: true,
	}}
	app := appFor(t, backendReporting(t, false, "Authentication failed"), calls, dialSucceeds(t))

	outcome, err := app.awaitSyncOrFallBack()
	if err != nil {
		t.Fatalf("awaitSyncOrFallBack() error = %v", err)
	}
	if outcome.Reach != string(reach.Reachable) {
		t.Errorf("Reach = %q, want %q", outcome.Reach, reach.Reachable)
	}
	if !calls.ran("pin-config-secret") {
		t.Error("the fallback was not attempted for a cluster whose path is open")
	}
}

// And the allowlist case, which is the one the screen explains with this
// machine's address — the reason the diagnosis exists. Asked through
// reachOfSync rather than DiagnoseSync, which would go on to ask a real
// service on the internet for that address.
func TestReachOfSyncCallsAClosedAtlasClusterUnreachable(t *testing.T) {
	t.Parallel()

	calls := &serviceCalls{reply: servicecli.Reply{
		OK: true, Installed: true, SyncConfigured: true, SyncHost: atlasSeed,
	}}
	app := appFor(t, "", calls, dialTimesOut)

	if verdict := app.reachOfSync(""); verdict != reach.Unreachable {
		t.Errorf("reachOfSync() = %q, want %q", verdict, reach.Unreachable)
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
	// The diagnosis comes from DiagnoseSync once the window reloads; probing
	// here too only held "Reintentando…" open for an answer nobody read.
	if outcome.Reach != "" {
		t.Errorf("Reach = %q, want it left to DiagnoseSync", outcome.Reach)
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

// preparing is a machine whose Prepare runs for real against fakes: a build
// with a payload, a tree that is not this version yet, and no service
// registered. extract answers each extraction in turn.
type preparing struct {
	mu          sync.Mutex
	extractions int
	probed      bool
}

func (p *preparing) deps(found string, extract func(attempt int) error) installer.Deps {
	return installer.Deps{
		DataDir: "/data/MangaTracker",
		Discover: func(context.Context) string {
			p.mu.Lock()
			defer p.mu.Unlock()
			p.probed = true
			return found
		},
		Available: func() bool { return true },
		Extracted: func(string) bool { return false },
		Extract: func(string) error {
			p.mu.Lock()
			p.extractions++
			attempt := p.extractions
			p.mu.Unlock()
			return extract(attempt)
		},
		Call: func(context.Context, string, ...string) (servicecli.Reply, error) {
			return servicecli.Reply{OK: true}, nil
		},
	}
}

func (p *preparing) wasProbed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.probed
}

// Wails runs OnStartup on its own goroutine while the window loads, so the
// window's first Look can arrive while Prepare is still extracting. It has to
// wait: answering then described a half-written tree as the machine's state.
func TestLookWaitsUntilStartupHasPrepared(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	machine := &preparing{}
	app := &App{
		prepared: make(chan struct{}),
		deps: machine.deps("", func(int) error {
			<-release
			return nil
		}),
	}

	go app.startup(context.Background())
	looked := make(chan installer.State, 1)
	go func() { looked <- app.Look() }()

	select {
	case state := <-looked:
		t.Fatalf("Look() = %+v while Prepare was still extracting", state)
	case <-time.After(100 * time.Millisecond):
	}
	if machine.wasProbed() {
		t.Fatal("Look probed for a backend while Prepare was still extracting")
	}

	close(release)
	select {
	case state := <-looked:
		if state.Kind != installer.KindInstallable {
			t.Errorf("Kind = %q, want %q once the tree is on disk", state.Kind, installer.KindInstallable)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Look never answered after Prepare finished")
	}
}

// A release that could not write its server out used to be reported as a
// development build, with the error in a field the window never showed.
func TestLookReportsAFailedPreparationAsSuch(t *testing.T) {
	t.Parallel()

	machine := &preparing{}
	app := &App{
		prepared: make(chan struct{}),
		deps: machine.deps("", func(int) error {
			return errors.New("no space left on device")
		}),
	}
	app.startup(context.Background())

	state := app.Look()
	if state.Kind != installer.KindSetupFailed {
		t.Fatalf("Kind = %q, want %q", state.Kind, installer.KindSetupFailed)
	}
	if !strings.Contains(state.Detail, "no space left on device") {
		t.Errorf("Detail = %q, want the reason it failed", state.Detail)
	}
}

// "Buscar de nuevo" after freeing the disk has to actually try again, and once
// it has worked it must not keep re-extracting on every look.
func TestLookRetriesAFailedPreparationOnce(t *testing.T) {
	t.Parallel()

	machine := &preparing{}
	app := &App{
		prepared: make(chan struct{}),
		deps: machine.deps("", func(attempt int) error {
			if attempt == 1 {
				return errors.New("no space left on device")
			}
			return nil
		}),
	}
	app.startup(context.Background())

	if state := app.Look(); state.Kind != installer.KindInstallable {
		t.Fatalf("Kind = %q, want %q after the retry worked", state.Kind, installer.KindInstallable)
	}
	app.Look()
	if machine.extractions != 2 {
		t.Errorf("extractions = %d, want 2: the failed one and the retry", machine.extractions)
	}
}

// The previous version still answering is worth showing, failure or not:
// hiding a working library behind an update that did not land helps nobody.
func TestLookShowsARunningBackendEvenIfPreparationFailed(t *testing.T) {
	t.Parallel()

	machine := &preparing{}
	app := &App{
		prepared: make(chan struct{}),
		deps: machine.deps("http://127.0.0.1:5150", func(int) error {
			return errors.New("access is denied")
		}),
	}
	app.startup(context.Background())

	if state := app.Look(); state.Kind != installer.KindRunning {
		t.Errorf("Kind = %q, want %q", state.Kind, installer.KindRunning)
	}
}

// With no data directory, deps are never wired. Startup used to call into them
// anyway and panic, so the error meant to be shown never was.
func TestNoDataDirectoryIsReportedRatherThanCrashing(t *testing.T) {
	t.Parallel()

	app := &App{
		prepared:   make(chan struct{}),
		client:     &http.Client{Timeout: time.Second},
		dataDirErr: errors.New("could not find a place to install into"),
	}
	app.startup(context.Background())

	if state := app.Look(); state.Kind != installer.KindSetupFailed {
		t.Errorf("Look().Kind = %q, want %q", state.Kind, installer.KindSetupFailed)
	}
	if _, err := app.Install(); err == nil {
		t.Error("Install() succeeded with no data directory")
	}
	if settings := app.Settings(); settings.Problem == "" {
		t.Error("Settings().Problem is empty with no data directory")
	}
	if _, err := app.SetSync("mongodb://db.example.com:27017", ""); err == nil {
		t.Error("SetSync() succeeded with no data directory")
	}
	if err := app.StartService(); err == nil {
		t.Error("StartService() succeeded with no data directory")
	}
}
