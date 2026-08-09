// Package servicecli talks to the backend's service control.
//
// Registering a launchd agent or a Windows scheduled task correctly is not
// obvious — the macOS reload has to wait for the old job to disappear before
// bootstrapping, and the Windows task needs an S4U logon type plus an icacls
// grant afterwards. That is already written and tested in manga-tracker-api
// (deploy/lib), and it ships beside the server as a bundled `service.js`.
//
// So this package does not reimplement any of it. It spawns that program,
// reads the single JSON object it prints, and turns a failure into a Go error.
package servicecli

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Reply is the union of every field the CLI prints. A command that does not
// produce one leaves it at its zero value, which is what the callers expect —
// `Installed` is only meaningful after `Status`, `Port` after `Status` or
// `Install`.
type Reply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`

	Port       int    `json:"port"`
	DataDir    string `json:"dataDir"`
	Installed  bool   `json:"installed"`
	ConfigPath string `json:"configPath"`
	Service    string `json:"service"`
	// False when the service registered but this account cannot start or stop
	// it — the Windows ACL case, which the app has to surface rather than hide.
	UserCanControlIt bool `json:"userCanControlIt"`
	SyncConfigured   bool `json:"syncConfigured"`
	// Where sync points — host and database only. The CLI parses the
	// connection string on its side precisely so the credential never has to
	// travel here to be displayed.
	SyncHost string `json:"syncHost"`
	SyncDb   string `json:"syncDb"`
	// SecretInConfig is true while the credential is still sitting in the
	// service's own configuration in plaintext, which the launcher exists to
	// end — and which remains the fallback for a machine whose service cannot
	// read its keystore at startup.
	SecretInConfig bool `json:"secretInConfig"`
	// HasStoredCredential says the system keystore on this machine holds one,
	// even when the configuration does not — which is what happens after a
	// fresh install replaces the service definition. Whether it exists, never
	// what it is.
	HasStoredCredential bool `json:"hasStoredCredential"`
	// UsesSrv flags a stored mongodb+srv:// URL: it works here and never
	// connects on Windows.
	UsesSrv bool `json:"usesSrv"`
}

// Command runs a program and returns its stdout. Injected so the tests never
// spawn anything, the same way manga-tracker-api pins its own commands.
type Command func(ctx context.Context, name string, args ...string) ([]byte, error)

// Exec is the real one.
func Exec(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// CommandWithInput runs a program that is handed something on stdin.
//
// Separate from Command rather than an extra parameter on it: the difference is
// not a detail of how the process is spawned, it is which values are allowed to
// travel there. Everything that goes through this one is a credential.
type CommandWithInput func(ctx context.Context, stdin string, name string, args ...string) ([]byte, error)

// ExecWithInput is the real one. The value reaches the child through a pipe,
// which `ps` and Task Manager do not show — unlike an argument, which every
// process on the machine can read.
func ExecWithInput(ctx context.Context, stdin string, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = strings.NewReader(stdin)
	return command.Output()
}

// Client knows where the bundled interpreter and the CLI live.
type Client struct {
	BunPath      string
	ScriptPath   string
	Run          Command
	RunWithInput CommandWithInput
}

// Call runs one command and returns its reply.
func (c Client) Call(ctx context.Context, args ...string) (Reply, error) {
	run := c.Run
	if run == nil {
		run = Exec
	}
	stdout, err := run(ctx, c.BunPath, c.argsFor(args)...)
	return parseReply(stdout, err)
}

// CallWithSecret is Call for the one command that carries a credential.
//
// The value goes to the CLI's stdin rather than into its arguments. That is the
// rule manga-tracker-api already holds for `az` — "never --value, which ps
// would expose" — and this hop was the exception: the connection string a user
// types is a cluster password, and it was readable by every process on the
// machine for as long as the command ran.
func (c Client) CallWithSecret(ctx context.Context, secret string, args ...string) (Reply, error) {
	run := c.RunWithInput
	if run == nil {
		run = ExecWithInput
	}
	stdout, err := run(ctx, secret, c.BunPath, c.argsFor(args)...)
	return parseReply(stdout, err)
}

func (c Client) argsFor(args []string) []string {
	return append([]string{"run", c.ScriptPath}, args...)
}

// parseReply turns the CLI's output into a reply or an error.
//
// Two different failures are kept apart on purpose: the program not running at
// all, and the program running and reporting that it could not do the job. The
// second one carries a message worth showing to whoever is looking at the
// screen; the first one usually means the payload was not extracted.
func parseReply(stdout []byte, err error) (Reply, error) {
	// The CLI prints its JSON and then exits non-zero on failure, so the output
	// is worth parsing even when the process failed.
	var reply Reply
	if parseErr := json.Unmarshal(trimToJSON(stdout), &reply); parseErr != nil {
		if err != nil {
			return Reply{}, fmt.Errorf("service control did not run: %w", err)
		}
		return Reply{}, fmt.Errorf("service control returned no usable answer: %q", string(stdout))
	}
	if !reply.OK {
		if reply.Error == "" {
			return reply, fmt.Errorf("service control failed without saying why")
		}
		return reply, fmt.Errorf("%s", reply.Error)
	}
	return reply, nil
}

// Bun may print a warning before the JSON; the object is the last line that
// looks like one.
func trimToJSON(stdout []byte) []byte {
	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}") {
			return []byte(line)
		}
	}
	return stdout
}
