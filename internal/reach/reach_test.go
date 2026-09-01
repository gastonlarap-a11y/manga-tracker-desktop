package reach

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
	"time"
)

// dialsTo answers every dial with one end of an in-memory pipe: enough for a
// connection that was established, without a listener anywhere.
func dialsTo(t *testing.T) DialContext {
	t.Helper()
	return func(context.Context, string, string) (net.Conn, error) {
		ours, theirs := net.Pipe()
		t.Cleanup(func() { _ = theirs.Close() })
		return ours, nil
	}
}

func fails(err error) DialContext {
	return func(context.Context, string, string) (net.Conn, error) { return nil, err }
}

func resolvesTo(addresses ...string) LookupHost {
	return func(context.Context, string) ([]string, error) { return addresses, nil }
}

func lookupFails(err error) LookupHost {
	return func(context.Context, string) ([]string, error) { return nil, err }
}

func TestCheck(t *testing.T) {
	t.Parallel()

	// The shape a dial failure really arrives in: the syscall wrapped twice,
	// which is why the code reaches for errors.Is rather than comparing.
	wrapped := func(errno syscall.Errno) error {
		return &net.OpError{Op: "dial", Net: "tcp", Err: &os0{errno}}
	}

	tests := []struct {
		name     string
		hostPort string
		lookup   LookupHost
		dial     DialContext
		want     Verdict
	}{
		{
			name:     "no host to test is not a finding",
			hostPort: "",
			want:     Unknown,
		},
		{
			name:     "a host without a port cannot be dialled",
			hostPort: "db.example.com",
			want:     Unknown,
		},
		{
			name:     "a name that does not exist",
			hostPort: "db.example.com:10260",
			lookup:   lookupFails(&net.DNSError{Err: "no such host", IsNotFound: true}),
			want:     Unresolved,
		},
		{
			name:     "a resolver that could not be asked is not a missing name",
			hostPort: "db.example.com:10260",
			lookup:   lookupFails(&net.DNSError{Err: "timeout", IsTimeout: true}),
			want:     Unknown,
		},
		{
			name:     "a name that resolves to nothing",
			hostPort: "db.example.com:10260",
			lookup:   resolvesTo(),
			want:     Unresolved,
		},
		{
			name:     "resolves but the connection times out: the allowlist case",
			hostPort: "db.example.com:10260",
			lookup:   resolvesTo("203.0.113.7"),
			dial:     fails(&net.OpError{Op: "dial", Err: context.DeadlineExceeded}),
			want:     Unreachable,
		},
		{
			name:     "resolves but the port is closed",
			hostPort: "db.example.com:10260",
			lookup:   resolvesTo("203.0.113.7"),
			dial:     fails(wrapped(syscall.ECONNREFUSED)),
			want:     Unreachable,
		},
		{
			name:     "this machine has no network, which blames nobody",
			hostPort: "db.example.com:10260",
			lookup:   resolvesTo("203.0.113.7"),
			dial:     fails(wrapped(syscall.ENETUNREACH)),
			want:     Unknown,
		},
		{
			name:     "the host itself is unreachable, which blames nobody",
			hostPort: "db.example.com:10260",
			lookup:   resolvesTo("203.0.113.7"),
			dial:     fails(wrapped(syscall.EHOSTUNREACH)),
			want:     Unknown,
		},
		{
			name:     "a cancelled probe found nothing out",
			hostPort: "db.example.com:10260",
			lookup:   resolvesTo("203.0.113.7"),
			dial:     fails(context.Canceled),
			want:     Unknown,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			lookup := test.lookup
			if lookup == nil {
				lookup = resolvesTo("203.0.113.7")
			}
			dial := test.dial
			if dial == nil {
				dial = dialsTo(t)
			}
			if got := Check(context.Background(), test.hostPort, lookup, dial, time.Second); got != test.want {
				t.Errorf("Check(%q) = %q, want %q", test.hostPort, got, test.want)
			}
		})
	}
}

func TestCheckReportsAnOpenPathAsReachable(t *testing.T) {
	t.Parallel()

	got := Check(context.Background(), "db.example.com:10260", resolvesTo("203.0.113.7"), dialsTo(t), time.Second)
	if got != Reachable {
		t.Errorf("Check() = %q, want %q", got, Reachable)
	}
}

// A numeric address has nothing to resolve, and asking anyway would fail on a
// machine with no resolver — turning a perfectly reachable server into Unknown.
func TestCheckDoesNotLookUpANumericAddress(t *testing.T) {
	t.Parallel()

	asked := false
	lookup := func(context.Context, string) ([]string, error) {
		asked = true
		return nil, errors.New("no resolver on this machine")
	}

	if got := Check(context.Background(), "203.0.113.7:10260", lookup, dialsTo(t), time.Second); got != Reachable {
		t.Errorf("Check() = %q, want %q", got, Reachable)
	}
	if asked {
		t.Error("Check resolved an address that was already numeric")
	}
}

// os0 stands in for *os.SyscallError, which is what a dial failure wraps and
// what errors.Is has to see through.
type os0 struct{ errno syscall.Errno }

func (e *os0) Error() string { return e.errno.Error() }
func (e *os0) Unwrap() error { return e.errno }
