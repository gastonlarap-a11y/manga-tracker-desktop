package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
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
	// The service control ships inside the payload, so it has to be on disk
	// before anything asks it a question — including the settings screen, which
	// otherwise reports every answer as "not installed". This is also where an
	// app installed over an older one moves its backend to the new version.
	if err := a.deps.Prepare(ctx); err != nil && a.setupErr == "" {
		a.setupErr = err.Error()
	}
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
	HasPayload bool `json:"hasPayload"`
	// Asked reports whether the service control answered at all. Without it,
	// "there is no service" and "I could not ask" both arrived as
	// Installed:false, and a transient failure looked like a fresh machine.
	Asked          bool   `json:"asked"`
	Installed      bool   `json:"installed"`
	Port           int    `json:"port"`
	DataDir        string `json:"dataDir"`
	ExtensionDir   string `json:"extensionDir"`
	Version        string `json:"version"`
	SyncConfigured bool   `json:"syncConfigured"`
	// HasStoredCredential lets the screen offer to carry over the sync this
	// machine already had, instead of presenting an empty form to someone who
	// configured it long ago.
	HasStoredCredential bool `json:"hasStoredCredential"`
	// Where sync points and how it is doing, so the screen can say "connected,
	// against this server" instead of showing an empty form to someone whose
	// sync has been running for weeks. Host and database only — the credential
	// is parsed out on the service control's side and never travels here.
	SyncHost string             `json:"syncHost"`
	SyncDb   string             `json:"syncDb"`
	SyncLive SyncLive           `json:"syncLive"`
	Browsers []browsers.Browser `json:"browsers"`
	StoreURL string             `json:"storeUrl"`
	// Set when the service is there and still could not be asked — a real
	// fault, shown as technical detail under a sentence the window writes.
	Problem string `json:"problem"`
}

// SyncLive is how the sync is actually doing right now, as opposed to how it
// was configured.
//
// Its own type with its own Asked field, for the reason stated in AGENTS.md:
// "not connected" and "the backend did not answer" must not arrive as the same
// false. The backend being unreachable says nothing about the credential.
type SyncLive struct {
	Asked      bool   `json:"asked"`
	Connected  bool   `json:"connected"`
	LastSyncAt string `json:"lastSyncAt"`
	LastError  string `json:"lastError"`
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
	settings.Asked = true
	settings.Installed = reply.Installed
	settings.Port = reply.Port
	settings.SyncConfigured = reply.SyncConfigured
	settings.HasStoredCredential = reply.HasStoredCredential
	settings.SyncHost = reply.SyncHost
	settings.SyncDb = reply.SyncDb

	// Only worth asking when there is something to report on: with no sync
	// configured the answer is always the same, and it would cost every open of
	// this screen an HTTP round trip to hear it.
	if settings.SyncConfigured {
		settings.SyncLive = a.liveSync()
	}
	return settings
}

func (a *App) liveSync() SyncLive {
	state := a.deps.Look(a.ctx)
	if state.BaseURL == "" {
		return SyncLive{}
	}
	status, err := backend.FetchSyncStatus(a.ctx, a.client, state.BaseURL)
	if err != nil {
		return SyncLive{}
	}
	return SyncLive{
		Asked:      true,
		Connected:  status.Connected,
		LastSyncAt: status.LastSyncAt,
		LastError:  status.LastError,
	}
}

// SyncOutcome is what the screen reports after saving credentials.
type SyncOutcome struct {
	// Problem is a code the window turns into a sentence, so every message a
	// person reads is written in one place: "empty", "srv", "notMongo".
	Problem string `json:"problem"`
	// Settled says the backend was asked and answered. Without it, "it did not
	// connect" and "it was still restarting when I looked" arrived as the same
	// Connected:false — and the second one is by far the more common, because
	// saving is what restarts it.
	Settled   bool `json:"settled"`
	Connected bool `json:"connected"`
	// LastError is the backend's own words about why it could not connect.
	LastError string `json:"lastError"`
	// UsesSrv warns that the credential in use is a mongodb+srv:// URL, which
	// works here and never connects on Windows.
	UsesSrv bool `json:"usesSrv"`
	// Converted says the address stored is not the one that was pasted: a
	// mongodb+srv:// was resolved into its direct form.
	Converted bool `json:"converted"`
	// Host is the server it ended up pointing at — no user, no password.
	Host string `json:"host"`
}

// SetSync stores the user's own credentials and restarts the backend with them.
//
// Configuring this is optional and always was: with nothing set the library
// lives in the local SQLite file, there is no automatic sync and no button to
// trigger one.
func (a *App) SetSync(pasted string, database string) (SyncOutcome, error) {
	// What Azure and Atlas hand you is a mongodb+srv:// address, and that is
	// the one form the backend cannot use on Windows. Rather than refuse the
	// only string most people have, the app resolves the record here — Go asks
	// the OS resolver, which answers on both systems — and stores the direct
	// address, which works on either machine. Anything already direct comes
	// back untouched and costs no lookup.
	resolved, problem := syncurl.Resolve(a.ctx, pasted, net.DefaultResolver.LookupSRV)
	if problem == syncurl.None {
		problem = syncurl.Validate(resolved)
	}
	if problem != syncurl.None {
		return SyncOutcome{Problem: string(problem)}, nil
	}

	outcome, err := a.storeSync(resolved, database)
	// Said out loud rather than done quietly: what got stored is not what was
	// typed, and someone comparing the two later deserves to know why.
	outcome.Converted = resolved != strings.TrimSpace(pasted)
	outcome.Host = hostOf(resolved)
	return outcome, err
}

// hostOf is the part of a connection string that is safe to show: never the
// user, never the password.
func hostOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Host
}

// SetSyncFields stores a connection assembled from separately typed fields.
//
// This is the path for someone who was handed a server, a user and a password
// rather than a connection string — which is most people. It exists because a
// MongoDB URI is a URL: a password containing `@`, `:`, `/`, `?`, `#` or `%`
// has to be percent-encoded inside it, and typed into a single address field it
// is not. What comes back is not a helpful error but an authentication failure,
// or a driver reading half the password as a hostname.
func (a *App) SetSyncFields(address, user, password, database string) (SyncOutcome, error) {
	url, problem := syncurl.Build(syncurl.Credentials{
		Address:  address,
		User:     user,
		Password: password,
	})
	if problem != syncurl.None {
		return SyncOutcome{Problem: string(problem)}, nil
	}
	return a.storeSync(url, database)
}

// storeSync hands the credential to the service control and waits to find out
// whether it works.
//
// The credential goes through CallWithSecret, which puts it on the CLI's stdin:
// as an argument it was readable by every process on the machine through `ps`,
// which is exactly what this project's own rule about `az` forbids.
func (a *App) storeSync(url string, database string) (SyncOutcome, error) {
	if _, err := a.deps.CallWithSecret(
		a.ctx, a.deps.AppDir(), url, "set-sync", "--db", syncurl.Database(database),
	); err != nil {
		return SyncOutcome{}, err
	}
	return a.awaitSync()
}

// awaitSync reports whether the configuration that was just written connects.
//
// Saving restarts the service, and for a few seconds afterwards nothing is
// listening: the process that answered a moment ago is on its way out and its
// replacement has not bound the port yet. So this waits for a backend to exist
// before asking it anything.
//
// Without that wait the first look found nothing and returned an empty outcome,
// which the window read as "no pudo conectar" — on a sync that had connected
// perfectly well and was already pushing. Not knowing yet is its own answer,
// and it is reported as one rather than as a failure.
func (a *App) awaitSync() (SyncOutcome, error) {
	baseURL := a.waitForBackend(30 * time.Second)
	if baseURL == "" {
		return SyncOutcome{Settled: false}, nil
	}
	status, err := backend.WaitForSync(a.ctx, a.client, baseURL, 20*time.Second)
	if err != nil {
		return SyncOutcome{Settled: false}, err
	}
	return SyncOutcome{
		Settled:   true,
		Connected: status.Connected,
		LastError: status.LastError,
	}, nil
}

// waitForBackend blocks until something answers on this machine again, or the
// timeout runs out. Returns "" if it never came back.
func (a *App) waitForBackend(timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for {
		if state := a.deps.Look(a.ctx); state.BaseURL != "" {
			return state.BaseURL
		}
		if time.Now().After(deadline) {
			return ""
		}
		select {
		case <-a.ctx.Done():
			return ""
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// UseStoredSync turns synchronising back on with the credential this machine
// already held, rather than making someone find and retype it.
//
// Nothing is sent anywhere and nothing was shipped: the credential has been in
// this machine's keystore all along, and installing simply stopped referring to
// it. Deliberate rather than automatic — turning on a connection to the cloud
// is not something an install should decide.
func (a *App) UseStoredSync(database string) (SyncOutcome, error) {
	reply, err := a.service("use-stored-sync", "--db", syncurl.Database(database))
	if err != nil {
		return SyncOutcome{}, err
	}

	// UsesSrv is carried only here. SetSync and SetSyncFields cannot produce
	// one: syncurl refuses an srv URL before it is ever stored. It survives on
	// this path because the credential predates that rule — it was put in the
	// keystore by hand, and it works on this machine.
	outcome, err := a.awaitSync()
	outcome.UsesSrv = reply.UsesSrv
	return outcome, err
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
