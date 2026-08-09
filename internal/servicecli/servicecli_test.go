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
