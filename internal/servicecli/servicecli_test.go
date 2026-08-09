package servicecli

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// answers builds a Command that returns a fixed stdout and error, and records
// what it was asked to run.
func answers(stdout string, err error, seen *[]string) Command {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		*seen = append(*seen, strings.Join(append([]string{name}, args...), " "))
		return []byte(stdout), err
	}
}

func client(stdout string, err error, seen *[]string) Client {
	return Client{
		BunPath:    "/opt/app/bun",
		ScriptPath: "/opt/app/service.js",
		Run:        answers(stdout, err, seen),
	}
}

// withInput is answers' counterpart: it records the command line and, apart
// from it, whatever was handed to the process on stdin.
func withInput(stdout string, err error, seen *[]string, stdin *string) CommandWithInput {
	return func(_ context.Context, input string, name string, args ...string) ([]byte, error) {
		*seen = append(*seen, strings.Join(append([]string{name}, args...), " "))
		*stdin = input
		return []byte(stdout), err
	}
}

// The assertion this whole change exists for. A credential in the argument
// list is readable by every process on the machine — `ps` on macOS, Task
// Manager on Windows — for as long as the command runs. manga-tracker-api holds
// the same rule for `az` ("never --value, which ps would expose") and this hop
// was the exception.
func TestCallWithSecretKeepsTheCredentialOutOfTheCommandLine(t *testing.T) {
	const credential = "mongodb://reader:hunter2@cluster.example.com:10260/?tls=true"
	var seen []string
	var stdin string
	c := Client{
		BunPath:      "/opt/app/bun",
		ScriptPath:   "/opt/app/service.js",
		RunWithInput: withInput(`{"ok":true,"syncConfigured":true}`, nil, &seen, &stdin),
	}

	if _, err := c.CallWithSecret(context.Background(), credential, "set-sync", "--db", "mangas"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(seen) != 1 {
		t.Fatalf("expected one command, got %v", seen)
	}
	// Not just the whole string: any recognisable piece of it leaking is the
	// same failure. "hunter2" alone is enough to be someone's password.
	for _, fragment := range []string{credential, "hunter2", "reader"} {
		if strings.Contains(seen[0], fragment) {
			t.Errorf("%q reached the command line: %q", fragment, seen[0])
		}
	}
	if stdin != credential {
		t.Errorf("the credential did not reach stdin: %q", stdin)
	}
	// The rest of the command still travels normally — the database name is
	// not a secret, and hiding it would only make this harder to debug.
	want := "/opt/app/bun run /opt/app/service.js set-sync --db mangas"
	if seen[0] != want {
		t.Errorf("expected to run %q, got %q", want, seen[0])
	}
}

func TestCallWithSecretSurfacesTheReportedReason(t *testing.T) {
	// Both entry points share one reply parser; this is what keeps that
	// refactor from quietly swallowing failures on the newer path.
	var seen []string
	var stdin string
	c := Client{
		RunWithInput: withInput(
			`{"ok":false,"error":"no connection string was provided on stdin"}`,
			errors.New("exit status 1"), &seen, &stdin,
		),
	}

	_, err := c.CallWithSecret(context.Background(), "", "set-sync")

	if err == nil || !strings.Contains(err.Error(), "no connection string") {
		t.Fatalf("expected the CLI's own reason, got %v", err)
	}
}

func TestCallRunsTheBundledInterpreter(t *testing.T) {
	var seen []string
	c := client(`{"ok":true,"port":5153}`, nil, &seen)

	if _, err := c.Call(context.Background(), "install", "--port", "5153"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "/opt/app/bun run /opt/app/service.js install --port 5153"
	if len(seen) != 1 || seen[0] != want {
		t.Errorf("expected to run %q, got %v", want, seen)
	}
}

func TestCallReturnsTheReply(t *testing.T) {
	var seen []string
	c := client(`{"ok":true,"installed":true,"port":5153,"syncConfigured":true}`, nil, &seen)

	reply, err := c.Call(context.Background(), "status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply.Port != 5153 || !reply.Installed || !reply.SyncConfigured {
		t.Errorf("reply not parsed: %+v", reply)
	}
}

func TestCallSurfacesTheReportedReason(t *testing.T) {
	// The CLI prints its JSON and then exits non-zero, so the message is worth
	// parsing even though the process failed — it is the only thing that says
	// what went wrong in words a person can act on.
	var seen []string
	c := client(`{"ok":false,"error":"no free port between 5150 and 5159"}`, errors.New("exit status 1"), &seen)

	_, err := c.Call(context.Background(), "install")

	if err == nil || !strings.Contains(err.Error(), "no free port") {
		t.Errorf("expected the CLI's own reason, got %v", err)
	}
}

func TestCallDistinguishesNotRunningFromFailing(t *testing.T) {
	// Nothing on stdout and a failed process usually means the payload was
	// never extracted — a different problem, and a different fix.
	var seen []string
	c := client("", errors.New("no such file or directory"), &seen)

	_, err := c.Call(context.Background(), "status")

	if err == nil || !strings.Contains(err.Error(), "did not run") {
		t.Errorf("expected a did-not-run error, got %v", err)
	}
}

func TestCallRejectsOutputThatIsNotJSON(t *testing.T) {
	var seen []string
	c := client("Segmentation fault", nil, &seen)

	_, err := c.Call(context.Background(), "status")

	if err == nil || !strings.Contains(err.Error(), "no usable answer") {
		t.Errorf("expected an unusable-answer error, got %v", err)
	}
}

func TestCallIgnoresNoiseBeforeTheJSON(t *testing.T) {
	// Bun occasionally prints a warning first; the object is still the answer.
	var seen []string
	c := client("warn: something\n{\"ok\":true,\"port\":5155}", nil, &seen)

	reply, err := c.Call(context.Background(), "status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply.Port != 5155 {
		t.Errorf("expected port 5155, got %d", reply.Port)
	}
}

func TestCallDoesNotClaimSuccessWithoutAReason(t *testing.T) {
	var seen []string
	c := client(`{"ok":false}`, nil, &seen)

	if _, err := c.Call(context.Background(), "status"); err == nil {
		t.Error("expected an error when the CLI reports failure")
	}
}
