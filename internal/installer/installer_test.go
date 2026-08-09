package installer

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"manga-tracker-desktop/internal/payload"
	"manga-tracker-desktop/internal/servicecli"
)

// recorder builds Deps whose every effect is observable and none of which
// touches launchd, the Task Scheduler or a real filesystem.
type recorder struct {
	deps      Deps
	extracted []string
	commands  [][]string
	waited    []int
	// What `status` answers. Zero value: nothing installed yet.
	status servicecli.Reply
	// Whether the tree on disk is already this build's version. Zero value: it
	// is not, which is both a first install and an update.
	upToDate bool
	// A verb the service control refuses, so a test can describe an older
	// payload whose CLI does not know it.
	failVerb string
	// Extractions and commands in one sequence, because the update path is
	// about their order relative to each other.
	steps []string
}

func newRecorder(found string, available bool, reply servicecli.Reply, callErr error) *recorder {
	r := &recorder{}
	r.deps = Deps{
		DataDir:   "/data/MangaTracker",
		Discover:  func(context.Context) string { return found },
		Available: func() bool { return available },
		Extract: func(dir string) error {
			r.extracted = append(r.extracted, dir)
			r.steps = append(r.steps, "extract")
			return nil
		},
		Extracted: func(string) bool { return r.upToDate },
		Call: func(_ context.Context, appDir string, args ...string) (servicecli.Reply, error) {
			r.commands = append(r.commands, append([]string{appDir}, args...))
			verb := ""
			if len(args) > 0 {
				verb = args[0]
				r.steps = append(r.steps, verb)
			}
			if verb == r.failVerb {
				return servicecli.Reply{}, errors.New("unknown command")
			}
			// `status` is asked before installing, to catch a service that is
			// registered but stopped. Answered separately so a test can
			// describe a machine that already has one.
			if verb == "status" {
				return r.status, nil
			}
			return reply, callErr
		},
		WaitHealthy: func(_ context.Context, port int) error {
			r.waited = append(r.waited, port)
			return nil
		},
	}
	return r
}

const okReply = 5153

func installed() servicecli.Reply {
	return servicecli.Reply{OK: true, Port: okReply, UserCanControlIt: true}
}

func TestLookReportsARunningBackend(t *testing.T) {
	r := newRecorder("http://127.0.0.1:5150", true, installed(), nil)

	state := r.deps.Look(context.Background())

	if state.Kind != KindRunning || state.BaseURL != "http://127.0.0.1:5150" {
		t.Errorf("expected a running backend, got %+v", state)
	}
}

func TestLookOffersToInstallWhenNothingAnswers(t *testing.T) {
	r := newRecorder("", true, installed(), nil)

	if kind := r.deps.Look(context.Background()).Kind; kind != KindInstallable {
		t.Errorf("expected installable, got %q", kind)
	}
}

func TestLookSaysADevelopmentBuildCannotInstall(t *testing.T) {
	// Better than showing a button that fails: `wails dev` carries no payload,
	// and that is a build choice, not a fault.
	r := newRecorder("", false, installed(), nil)

	if kind := r.deps.Look(context.Background()).Kind; kind != KindNoPayload {
		t.Errorf("expected noPayload, got %q", kind)
	}
}

func TestInstallRefusesToTouchARunningBackend(t *testing.T) {
	// The whole point of this guard: on the author's machine that backend is a
	// production LaunchAgent pointing at a source checkout.
	r := newRecorder("http://127.0.0.1:5150", true, installed(), nil)

	_, err := r.deps.Install(context.Background())

	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("expected ErrAlreadyRunning, got %v", err)
	}
	if len(r.extracted) != 0 || len(r.commands) != 0 {
		t.Error("nothing may be written or registered when a backend is already up")
	}
}

func TestInstallRefusesWhenTheServiceIsRegisteredButStopped(t *testing.T) {
	// The case ErrAlreadyRunning misses. Stopping the service is exactly what
	// someone does to try the installer, and at that moment the machine still
	// has a definition pointing at their real setup.
	r := newRecorder("", true, installed(), nil)
	r.status = servicecli.Reply{OK: true, Installed: true, Port: 5150}

	_, err := r.deps.Install(context.Background())

	if !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("expected ErrAlreadyInstalled, got %v", err)
	}
	for _, command := range r.commands {
		if strings.Contains(strings.Join(command, " "), "install") {
			t.Error("the service definition must not be overwritten")
		}
	}
}

func TestInstallExtractsIntoItsOwnDirectory(t *testing.T) {
	// Extraction deletes its destination. Aiming it at the data directory
	// would take the library and its backups with it.
	r := newRecorder("", true, installed(), nil)

	if _, err := r.deps.Install(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasSuffix(r.deps.AppDir(), filepath.Join(payload.RuntimeSubdir, payload.AppSubdir)) {
		t.Errorf("the backend must live under runtime/, got %s", r.deps.AppDir())
	}
}

func TestInstallExtractsThenRegisters(t *testing.T) {
	r := newRecorder("", true, installed(), nil)

	result, err := r.deps.Install(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(r.extracted) != 1 || r.extracted[0] != "/data/MangaTracker" {
		t.Errorf("expected one extraction into the data dir, got %v", r.extracted)
	}
	// status first, then install.
	if len(r.commands) != 2 {
		t.Fatalf("expected status then install, got %v", r.commands)
	}
	command := strings.Join(r.commands[1], " ")
	// The CLI is run from the extracted tree and told where both directories are.
	for _, want := range []string{"install", "--app-dir", "--data-dir", payload.AppSubdir} {
		if !strings.Contains(command, want) {
			t.Errorf("expected %q in %q", want, command)
		}
	}
	if result.Port != okReply || result.BaseURL != "http://127.0.0.1:5153" {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestInstallWaitsUntilItActuallyAnswers(t *testing.T) {
	// launchd and Task Scheduler both return before the process is listening,
	// and a window opening on a dead port looks exactly like a broken install.
	r := newRecorder("", true, installed(), nil)

	if _, err := r.deps.Install(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(r.waited) != 1 || r.waited[0] != okReply {
		t.Errorf("expected to wait on port %d, got %v", okReply, r.waited)
	}
}

func TestInstallSurfacesTheServiceReason(t *testing.T) {
	r := newRecorder("", true, servicecli.Reply{}, errors.New("no free port between 5150 and 5159"))

	_, err := r.deps.Install(context.Background())

	if err == nil || !strings.Contains(err.Error(), "no free port") {
		t.Errorf("expected the service's own reason, got %v", err)
	}
}

func TestInstallRejectsARegistrationWithNoPort(t *testing.T) {
	// Without a port there is nothing to open, and nothing to wait for.
	r := newRecorder("", true, servicecli.Reply{OK: true}, nil)

	_, err := r.deps.Install(context.Background())

	if err == nil || !strings.Contains(err.Error(), "no port") {
		t.Errorf("expected a missing-port error, got %v", err)
	}
	if len(r.waited) != 0 {
		t.Error("nothing should be waited on when no port was reported")
	}
}

func TestInstallCarriesTheWindowsControlWarning(t *testing.T) {
	// A task registered from an elevated shell can end up unstartable by the
	// account that owns it. The install still worked; the app has to say so.
	r := newRecorder("", true, servicecli.Reply{OK: true, Port: okReply}, nil)

	result, err := r.deps.Install(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.UserCanControlIt {
		t.Error("expected the warning to survive to the caller")
	}
}

func TestInstallDoesNothingWithoutAPayload(t *testing.T) {
	r := newRecorder("", false, installed(), nil)

	_, err := r.deps.Install(context.Background())

	if !errors.Is(err, payload.ErrNoPayload) {
		t.Fatalf("expected ErrNoPayload, got %v", err)
	}
	if len(r.extracted) != 0 {
		t.Error("nothing may be extracted when there is no payload")
	}
}

func TestPrepareDoesNothingWhenTheTreeIsAlreadyThisVersion(t *testing.T) {
	// Every launch calls this. Rewriting tens of thousands of files each time,
	// or bouncing a healthy service, would be a strange thing to do at startup.
	r := newRecorder("", true, installed(), nil)
	r.upToDate = true
	r.status = servicecli.Reply{OK: true, Installed: true, Port: 5150}

	if err := r.deps.Prepare(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.steps) != 0 {
		t.Errorf("expected an untouched machine, got %v", r.steps)
	}
}

func TestPrepareOnAFirstInstallOnlyExtracts(t *testing.T) {
	// No service yet: there is nothing to stop, and starting one is Install's
	// job — the person has not asked for it.
	r := newRecorder("", true, installed(), nil)

	if err := r.deps.Prepare(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := []string{"status", "extract"}; strings.Join(r.steps, ",") != strings.Join(want, ",") {
		t.Errorf("expected %v, got %v", want, r.steps)
	}
}

func TestPrepareStopsTheServiceBeforeReplacingItsFiles(t *testing.T) {
	// The service runs out of the tree being replaced. On Windows its open
	// files block the replacement outright; on both systems it would otherwise
	// go on executing the old code.
	r := newRecorder("", true, installed(), nil)
	r.status = servicecli.Reply{OK: true, Installed: true, Port: 5150}

	if err := r.deps.Prepare(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The whole point is the order: stopped, then written, then started.
	// `repair` rather than `restart`: an update is also when a machine
	// installed before the launcher existed gets its service definition
	// rewritten to point at it.
	want := []string{"status", "stop", "extract", "repair"}
	if strings.Join(r.steps, ",") != strings.Join(want, ",") {
		t.Fatalf("expected %v, got %v", want, r.steps)
	}
}

func TestPrepareUpdatesEvenWhenTheOldVersionCannotStopItself(t *testing.T) {
	// A payload shipped before `stop` existed answers "unknown command".
	// Refusing to update then would strand exactly the people an update is for,
	// and the extraction is written to cope with a tree still in use.
	r := newRecorder("", true, installed(), nil)
	r.status = servicecli.Reply{OK: true, Installed: true, Port: 5150}
	r.failVerb = "stop"

	if err := r.deps.Prepare(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.extracted) != 1 {
		t.Errorf("expected the update to go through anyway, got %v", r.extracted)
	}
}

func TestPrepareSaysSoWhenTheBackendDoesNotComeBack(t *testing.T) {
	// Written but not running is the one outcome nobody can see for themselves:
	// the app opens on a library that is simply not there.
	r := newRecorder("", true, installed(), nil)
	r.status = servicecli.Reply{OK: true, Installed: true, Port: 5150}
	r.failVerb = "repair"

	err := r.deps.Prepare(context.Background())

	if err == nil || !strings.Contains(err.Error(), "did not come back") {
		t.Fatalf("expected the restart failure to be reported, got %v", err)
	}
}

func TestPrepareDoesNothingWithoutAPayload(t *testing.T) {
	// A development build carries no backend. Not a fault, and not something to
	// report at startup.
	r := newRecorder("", false, installed(), nil)

	if err := r.deps.Prepare(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(r.steps) != 0 {
		t.Errorf("expected nothing to happen, got %v", r.steps)
	}
}

func TestDirectoriesHangOffTheDataDir(t *testing.T) {
	d := Deps{DataDir: "/data/MangaTracker"}

	if !strings.HasSuffix(d.AppDir(), payload.AppSubdir) {
		t.Errorf("unexpected app dir: %s", d.AppDir())
	}
	if !strings.HasSuffix(d.ExtensionDir(), payload.ExtensionSubdir) {
		t.Errorf("unexpected extension dir: %s", d.ExtensionDir())
	}
}
