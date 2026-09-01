// Package reach answers why sync is down, by testing the network rather than by
// reading the driver's error text.
//
// The backend reports its last failure in the words of the MongoDB driver, and
// "Server selection timed out after 15000 ms" is the same sentence whether the
// server's allowlist does not have this machine's address, the name no longer
// resolves, or the credential is wrong. Matching on that string would be
// guessing, and it would break the day the driver rewords it.
//
// So this asks the network directly: does the name resolve, and does anything
// accept a connection on that port. Three answers plus the one that matters
// most — Unknown, for when the question could not be put at all. A screen that
// says "the server is refusing this computer" because a lookup failed for some
// unrelated reason is the failure mode this whole package exists to avoid.
package reach

import (
	"context"
	"errors"
	"net"
	"syscall"
	"time"
)

// Verdict is why sync cannot reach its store, as a code the window turns into a
// sentence — never as a sentence itself.
type Verdict string

const (
	// Unknown is "I could not find out": no host to test, or the test itself
	// could not be carried out. Never reported as one of the other three.
	Unknown Verdict = "unknown"
	// Unresolved is a name that does not exist any more. The address was stored
	// when it did, so something changed on the other side.
	Unresolved Verdict = "unresolved"
	// Unreachable is a name that resolves while nothing accepts a connection on
	// its port: the packets leave and no answer comes back. On a managed
	// database this is nearly always an allowlist that no longer has this
	// machine's public address.
	Unreachable Verdict = "unreachable"
	// Reachable means the network path is open, so whatever is wrong is above
	// it — credentials, database name, or the server itself refusing the
	// session. Worth distinguishing: it is the only verdict under which a
	// missing keystore read is a plausible explanation.
	Reachable Verdict = "reachable"
)

// LookupHost and DialContext are the only two things this package needs from
// the network, taken as parameters so the tests never touch one.
//
// Both match the methods on net.Resolver and net.Dialer, so production passes
// net.DefaultResolver.LookupHost and (&net.Dialer{}).DialContext unchanged.
type (
	LookupHost  func(ctx context.Context, host string) ([]string, error)
	DialContext func(ctx context.Context, network, address string) (net.Conn, error)
)

// Check reports whether the network path to hostPort is open.
//
// hostPort is what the service control already reports as its sync host, in
// "name:port" form. Anything it cannot parse is Unknown rather than a guess.
func Check(ctx context.Context, hostPort string, lookup LookupHost, dial DialContext, timeout time.Duration) Verdict {
	host, _, err := net.SplitHostPort(hostPort)
	if err != nil || host == "" {
		return Unknown
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if verdict := resolves(ctx, host, lookup); verdict != Reachable {
		return verdict
	}

	conn, err := dial(ctx, "tcp", hostPort)
	if err != nil {
		return dialVerdict(err)
	}
	// Nothing is sent and nothing is read: that the handshake completed is the
	// whole answer. Closing can only fail on a connection already gone, which
	// does not change what was just learned.
	_ = conn.Close()
	return Reachable
}

// resolves reports Reachable when the name has an address, and otherwise the
// verdict that stands on its own.
func resolves(ctx context.Context, host string, lookup LookupHost) Verdict {
	// An address that is already numeric has nothing to look up, and asking
	// would fail on a machine with no DNS at all.
	if net.ParseIP(host) != nil {
		return Reachable
	}

	addresses, err := lookup(ctx, host)
	if err != nil {
		var dnsErr *net.DNSError
		// IsNotFound is the only DNS failure that says something about the name
		// itself. Everything else — a timeout, a resolver that is not there —
		// is this machine failing to ask, which is not the same finding.
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return Unresolved
		}
		return Unknown
	}
	if len(addresses) == 0 {
		return Unresolved
	}
	return Reachable
}

// dialVerdict separates "this machine has no network" from "this machine has
// one and the far end does not answer".
//
// The difference is the entire point. Blaming the server's allowlist for an
// unplugged cable would send someone to a cloud portal to fix their wifi.
func dialVerdict(err error) Verdict {
	switch {
	case errors.Is(err, syscall.ENETDOWN),
		errors.Is(err, syscall.ENETUNREACH),
		errors.Is(err, syscall.EHOSTUNREACH):
		return Unknown
	case errors.Is(err, context.Canceled):
		return Unknown
	}
	return Unreachable
}
