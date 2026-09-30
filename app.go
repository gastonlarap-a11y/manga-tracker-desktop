package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"manga-tracker-desktop/internal/backend"
	"manga-tracker-desktop/internal/browsers"
	"manga-tracker-desktop/internal/installer"
	"manga-tracker-desktop/internal/payload"
	"manga-tracker-desktop/internal/prefs"
	"manga-tracker-desktop/internal/publicip"
	"manga-tracker-desktop/internal/reach"
	"manga-tracker-desktop/internal/servicecli"
	"manga-tracker-desktop/internal/syncurl"
)

// reachTimeout bounds the probe that asks why sync is down: long enough for a
// handshake over a slow link, short enough that a settings screen opened on a
// broken connection still answers.
const reachTimeout = 8 * time.Second

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
	// Resolved once, in NewApp. When there is no data directory at all, deps
	// was never wired, and every method that would reach into it answers with
	// this instead.
	dataDirErr error
	// prepared is closed once startup has finished, and every bound method
	// that touches the backend waits on it first. See awaitStartup.
	prepared chan struct{}
	// prepareMu serialises Prepare: startup runs it, and so does a Look that
	// finds the previous attempt failed.
	prepareMu  sync.Mutex
	prepareErr error
	client     *http.Client
	// The network probe's dependencies, as fields so a test can answer them
	// without a resolver or a socket. Nil means the real ones.
	lookupHost  reach.LookupHost
	lookupSRV   reach.LookupSRV
	dialContext reach.DialContext
}

func NewApp() *App {
	app := &App{
		client:   &http.Client{Timeout: 5 * time.Second},
		prepared: make(chan struct{}),
	}
	dataDir, err := installer.DefaultDataDir()
	if err != nil {
		app.dataDirErr = err
		return app
	}
	app.deps = installer.Production(dataDir)
	return app
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Closed on every path, including the one with no data directory: a
	// window waiting on it forever would be a hang in place of the error.
	defer a.markStarted()
	if a.dataDirErr != nil {
		return
	}
	// The service control ships inside the payload, so it has to be on disk
	// before anything asks it a question — including the settings screen, which
	// otherwise reports every answer as "not installed". This is also where an
	// app installed over an older one moves its backend to the new version.
	//
	// The error is not dropped: prepare keeps it, and the window's first Look
	// reports it.
	_ = a.prepare()
}

func (a *App) markStarted() {
	if a.prepared != nil {
		close(a.prepared)
	}
}

// awaitStartup blocks until startup has run.
//
// Wails 2.12 calls OnStartup on a goroutine of its own, concurrently with
// loading the window — not before it. So the window's first Look used to run
// while Prepare was still stopping, extracting and re-registering the backend,
// and it described that half-done moment as the machine's state: "unknown" on
// a first launch whose tree was half written, "stopped" during every update,
// with a button that started a second `repair` alongside the one in progress.
//
// A nil channel counts as started, so a test can build an App literal without
// going through startup.
func (a *App) awaitStartup() {
	if a.prepared != nil {
		<-a.prepared
	}
}

// begin is the first call of every bound method that reaches into deps: it
// waits for startup, and refuses when there is no data directory to work in.
func (a *App) begin() error {
	a.awaitStartup()
	return a.dataDirErr
}

// prepare runs Prepare and remembers how it went.
func (a *App) prepare() error {
	a.prepareMu.Lock()
	defer a.prepareMu.Unlock()
	a.prepareErr = a.deps.Prepare(a.ctx)
	return a.prepareErr
}

// prepareAgainIfFailed retries a Prepare that failed, and is a no-op after one
// that succeeded. Prepare is idempotent, so asking again is safe — and it is
// what "Buscar de nuevo" is for once whatever stopped it has been fixed.
func (a *App) prepareAgainIfFailed() error {
	a.prepareMu.Lock()
	failed := a.prepareErr != nil
	a.prepareMu.Unlock()
	if !failed {
		return nil
	}
	return a.prepare()
}

// Look reports what the window should show: a backend it can display, an offer
// to install one, or a development build that carries none.
//
// Not finding a backend is a normal answer rather than an error — on a machine
// where nothing is installed yet it is the expected one.
//
// A preparation that failed is its own answer, never "a development build":
// that is what it used to arrive as, with the error tucked into the version
// field the window does not show. The one exception is a backend that answers
// anyway — the previous version, still running — which is worth showing
// rather than hiding behind the failure.
func (a *App) Look() installer.State {
	if err := a.begin(); err != nil {
		return installer.State{Kind: installer.KindSetupFailed, Detail: err.Error(), Version: payload.Version()}
	}
	prepareErr := a.prepareAgainIfFailed()
	state := a.deps.Look(a.ctx)
	if prepareErr != nil && state.Kind != installer.KindRunning {
		return installer.State{Kind: installer.KindSetupFailed, Detail: prepareErr.Error(), Version: payload.Version()}
	}
	return state
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
	if err := a.begin(); err != nil {
		return InstallOutcome{}, err
	}
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
	SyncHost string   `json:"syncHost"`
	SyncDb   string   `json:"syncDb"`
	SyncLive SyncLive `json:"syncLive"`
	// SecretInConfig says the credential is in the service's configuration
	// rather than only in the system keystore — the fallback for a machine
	// whose service cannot read the keystore at startup. Shown, because where a
	// password lives is not a detail to keep from whoever owns it.
	SecretInConfig bool               `json:"secretInConfig"`
	Browsers       []browsers.Browser `json:"browsers"`
	StoreURL       string             `json:"storeUrl"`
	// ChapterBrowser is the browser a chapter link opens in, as an id from
	// Browsers; empty means the system default.
	//
	// ChapterBrowserKnown separates that empty from a preferences file that
	// could not be read — the same rule as Asked and SyncLive.Asked. Presenting
	// "the system default" to someone who did choose would be the screen
	// stating something untrue.
	ChapterBrowser      string `json:"chapterBrowser"`
	ChapterBrowserKnown bool   `json:"chapterBrowserKnown"`
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
	if err := a.begin(); err != nil {
		// No data directory: nothing below has anywhere to read from.
		settings.Problem = err.Error()
		return settings
	}
	// Read before the payload check below: which browser opens a chapter is a
	// choice a development build can make too.
	if stored, err := prefs.Load(a.deps.DataDir); err == nil {
		settings.ChapterBrowser = stored.BrowserID
		settings.ChapterBrowserKnown = true
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
	settings.SecretInConfig = reply.SecretInConfig

	// Only worth asking when there is something to report on: with no sync
	// configured the answer is always the same, and it would cost every open of
	// this screen an HTTP round trip to hear it.
	if settings.SyncConfigured {
		settings.SyncLive = a.liveSync()
	}
	return settings
}

// Diagnosis is why sync cannot reach its store, and the address an allowlist
// would have to contain for it to.
type Diagnosis struct {
	// Reach is a code from internal/reach, which the window turns into a
	// sentence — including "unknown", which is its own answer.
	Reach string `json:"reach"`
	// Address is this machine's public address, filled in only when the path is
	// closed and it could be found out. Best effort: an empty one means the
	// screen says nothing about it rather than showing a guess.
	Address string `json:"address"`
}

// DiagnoseSync works out why a configured sync is not connecting.
//
// Its own call rather than part of Settings, because it dials a socket and may
// ask a service on the internet: folded into Settings it would hold the
// configuration dialog shut for as long as the probe took, and the probe is
// slowest in exactly the case someone is opening the dialog to look at.
//
// The window calls it after it has rendered, and only for a sync that is
// configured and reporting that it is down.
func (a *App) DiagnoseSync() Diagnosis {
	if err := a.begin(); err != nil {
		return Diagnosis{Reach: string(reach.Unknown)}
	}
	verdict := a.reachOfSync("")
	diagnosis := Diagnosis{Reach: string(verdict)}
	if verdict == reach.Unreachable {
		if address, err := a.PublicAddress(); err == nil {
			diagnosis.Address = address
		}
	}
	return diagnosis
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
	// SecretInConfig is true when the credential had to be written into the
	// service's configuration because this machine's service could not read it
	// from the keystore at startup. Surfaced rather than hidden: it is a real
	// difference in where a password lives.
	SecretInConfig bool `json:"secretInConfig"`
	// Reach is why the network path to the store is or is not open, as a code
	// from internal/reach. Set only when it was worth asking — a connection
	// that came up needs no diagnosis — and "unknown" when the probe itself
	// could not be carried out.
	Reach string `json:"reach"`
}

// SetSync stores the user's own credentials and restarts the backend with them.
//
// Configuring this is optional and always was: with nothing set the library
// lives in the local SQLite file, there is no automatic sync and no button to
// trigger one.
func (a *App) SetSync(pasted string, database string) (SyncOutcome, error) {
	if err := a.begin(); err != nil {
		return SyncOutcome{}, err
	}
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
	if err := a.begin(); err != nil {
		return SyncOutcome{}, err
	}
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
	return a.awaitSyncOrFallBack()
}

// awaitSyncOrFallBack waits for the new configuration to connect, and puts the
// credential back in the service's configuration if it did not.
//
// `set-sync` leaves the credential in the system keystore and writes only a
// marker into the service's configuration, so the launcher reads it at startup
// and no plaintext copy exists. Whether a given machine's service *can* read
// its own keystore is not knowable in advance — it depends on how the session
// was created, and a Windows task running as S4U has no password behind it, so
// unwrapping a DPAPI blob may simply fail there.
//
// Rather than guess, this tries the good path and watches. A sync that does not
// come up is worse than a credential in a file locked to the account, which is
// what every install did until now — so on failure it falls back, says so, and
// leaves the machine working.
//
// But only when a keystore that could not be read is a possible explanation at
// all. "It did not connect" has more than one cause, and this used to treat
// every one of them as that cause: a cluster whose allowlist no longer had this
// machine's address took a working credential out of the Keychain and wrote it
// in plaintext into the service's configuration, on a Mac whose Keychain was
// fine. So the network path is tested first, and the fallback is reached only
// when the path is open and the credential is therefore a candidate.
func (a *App) awaitSyncOrFallBack() (SyncOutcome, error) {
	outcome, err := a.awaitSync()
	if err != nil || !outcome.Settled || outcome.Connected {
		return outcome, err
	}

	outcome.Reach = string(a.reachOfSync(""))
	if outcome.Reach != string(reach.Reachable) {
		// Nothing about where the credential lives would change this, and
		// moving a password to weaken it is not a step taken on a maybe.
		return outcome, nil
	}

	reply, pinErr := a.service("pin-config-secret")
	if pinErr != nil {
		// The fallback itself failed. The original outcome is the honest
		// answer: it did not connect, and here is what the backend said.
		return outcome, nil
	}

	pinned, err := a.awaitSync()
	pinned.SecretInConfig = reply.SecretInConfig
	// Carried over rather than probed again: the path was open a moment ago,
	// and it is what the screen needs to explain a fallback that still failed.
	pinned.Reach = outcome.Reach
	return pinned, err
}

// reachOfSync tests the network path to wherever sync points.
//
// Empty hosts asks the service control where that is, which is the only
// component that knows: the address lives beside the credential, and the
// credential never travels here. What it reports may be a whole seed list.
func (a *App) reachOfSync(hosts string) reach.Verdict {
	if hosts == "" {
		reply, err := a.service("status")
		if err != nil {
			return reach.Unknown
		}
		hosts = reply.SyncHost
	}
	network := reach.Network{
		LookupHost: a.lookupHost,
		LookupSRV:  a.lookupSRV,
		Dial:       a.dialContext,
	}
	if network.LookupHost == nil {
		network.LookupHost = net.DefaultResolver.LookupHost
	}
	if network.LookupSRV == nil {
		network.LookupSRV = net.DefaultResolver.LookupSRV
	}
	if network.Dial == nil {
		network.Dial = (&net.Dialer{}).DialContext
	}
	return reach.Check(a.ctx, hosts, network, reachTimeout)
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
	if err := a.begin(); err != nil {
		return SyncOutcome{}, err
	}
	reply, err := a.service("use-stored-sync", "--db", syncurl.Database(database))
	if err != nil {
		return SyncOutcome{}, err
	}

	// UsesSrv is carried only here. SetSync and SetSyncFields cannot produce
	// one: syncurl refuses an srv URL before it is ever stored. It survives on
	// this path because the credential predates that rule — it was put in the
	// keystore by hand, and it works on this machine.
	outcome, err := a.awaitSyncOrFallBack()
	outcome.UsesSrv = reply.UsesSrv
	return outcome, err
}

// ClearSync turns synchronising off and goes back to local-only.
func (a *App) ClearSync() error {
	if err := a.begin(); err != nil {
		return err
	}
	_, err := a.service("clear-sync")
	return err
}

// RetrySync asks the backend to connect to the store again, now.
//
// The header's "Reconectar" never did this: it looks for a backend on this
// machine, which answers perfectly well while its connection to the store is
// down. There was no way to retry the part that had actually failed short of
// retyping the credential — so someone who fixed the real problem had to sit
// and wait for the periodic attempt, with a screen still saying it was broken.
//
// `restart` rather than `repair`: the service definition is not in question
// here, only the process reading it. And deliberately not the fallback path —
// a retry must never be a reason to move a credential somewhere weaker.
//
// No network probe here: the window reloads its settings after a retry, and a
// sync still down gets DiagnoseSync, address included. Probing here as well
// cost up to reachTimeout more on "Reintentando…" for an answer the window
// then threw away.
func (a *App) RetrySync() (SyncOutcome, error) {
	if err := a.begin(); err != nil {
		return SyncOutcome{}, err
	}
	if _, err := a.service("restart"); err != nil {
		return SyncOutcome{}, err
	}
	return a.awaitSync()
}

// PublicAddress is the address this machine appears to come from, which is what
// an allowlist is written in terms of.
//
// Its own method rather than part of Settings: it is the one thing here that
// asks a service nobody in this project runs, and the settings screen should
// not pay for that on every open. The window calls it only when it has
// something to say about an address.
func (a *App) PublicAddress() (string, error) {
	a.awaitStartup()
	return publicip.Find(a.ctx, publicip.Client(reachTimeout), publicip.Endpoints)
}

// OpenInBrowser opens the store listing in one specific browser — not the
// default one, which may not be the browser the extension is wanted in.
func (a *App) OpenInBrowser(id string) error {
	return browsers.Open(id, StoreURL)
}

// RevealExtension opens the folder to point "Load unpacked" at, for a
// development build or a copy loaded by hand.
func (a *App) RevealExtension() error {
	if err := a.begin(); err != nil {
		return err
	}
	return browsers.Reveal(a.deps.ExtensionDir())
}

// StartService brings back a backend that is installed but not answering.
//
// The action the "stopped" screen offers, because looking again is no use when
// the service really is down. `repair` rather than `restart` for the reason
// stated in AGENTS.md: restarting only reloads what is already registered,
// while repair also re-registers a definition written by an older version. It
// keeps the port and the sync settings it finds.
func (a *App) StartService() error {
	if err := a.begin(); err != nil {
		return err
	}
	_, err := a.deps.Call(
		a.ctx, a.deps.AppDir(), "repair", "--app-dir", a.deps.AppDir(), "--data-dir", a.deps.DataDir,
	)
	return err
}

// SetChapterBrowser remembers which browser opens a chapter link. An empty id
// means the system default.
//
// Refuses an id no longer among the installed browsers rather than storing it:
// a preference pointing at a browser that is not there would silently do
// nothing every time it was used.
// Update rather than Save: the file holds more than this one setting now, and
// writing a Prefs built from the single value in hand would blank the rest.
func (a *App) SetChapterBrowser(id string) error {
	if err := a.begin(); err != nil {
		return err
	}
	if id != "" && !browsers.Installed(id) {
		return errors.New("unknown-browser")
	}
	return prefs.Update(a.deps.DataDir, func(p *prefs.Prefs) { p.BrowserID = id })
}

// OpenChapter opens a chapter link from the embedded dashboard in a real
// browser.
//
// The one route by which a URL from the iframe reaches this machine, so the
// scheme is checked here and nowhere else. The frame is served by the backend,
// but a page it renders can still carry any href, and `javascript:` or `file:`
// must not be handed to the system.
//
// Preference first, because the extension lives in one specific browser and a
// chapter opened anywhere else records nothing — which is the entire point of
// the app. The system default is the fallback rather than a failure: not
// opening at all is worse than opening in the wrong place, and the settings
// screen already lists the browsers actually found.
func (a *App) OpenChapter(link string) error {
	if !browsers.IsWebURL(link) {
		return errors.New("bad-url")
	}
	// Without a data directory there is no preference to read, and the system
	// default is still better than not opening the chapter at all.
	if err := a.begin(); err == nil {
		stored, err := prefs.Load(a.deps.DataDir)
		if err == nil && stored.BrowserID != "" {
			if err := browsers.Open(stored.BrowserID, link); err == nil {
				return nil
			}
		}
	}
	runtime.BrowserOpenURL(a.ctx, link)
	return nil
}

func (a *App) service(args ...string) (servicecli.Reply, error) {
	return a.deps.Call(a.ctx, a.deps.AppDir(), args...)
}
