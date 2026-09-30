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
	"strconv"
	"strings"
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

// DefaultPort is MongoDB's, which a connection string that names no port means.
const DefaultPort = "27017"

// LookupHost, LookupSRV and DialContext are the only things this package needs
// from the network, taken as parameters so the tests never touch one.
//
// They match the methods on net.Resolver and net.Dialer, so production passes
// net.DefaultResolver.LookupHost, net.DefaultResolver.LookupSRV and
// (&net.Dialer{}).DialContext unchanged.
type (
	LookupHost  func(ctx context.Context, host string) ([]string, error)
	LookupSRV   func(ctx context.Context, service, proto, name string) (string, []*net.SRV, error)
	DialContext func(ctx context.Context, network, address string) (net.Conn, error)
)

// Network bundles those dependencies.
type Network struct {
	LookupHost LookupHost
	LookupSRV  LookupSRV
	Dial       DialContext
}

// Check reports whether the network path to wherever sync points is open.
//
// hosts is what the service control reports as its sync host: everything
// between a connection string's `@` and its path. For a single server that is
// "name:port", but a cluster — which is what every resolved mongodb+srv://
// address becomes — is a comma-separated seed list of them, and a host may
// carry no port at all. Reading only the single-server form made every Atlas
// cluster come back Unknown, so the screen blamed the internet connection and
// the keystore fallback never ran.
//
// The seed hosts are probed together, within one timeout, and the answer is
// the most useful one that is also true: one open path makes the whole store
// reachable, because the driver only needs one.
func Check(ctx context.Context, hosts string, network Network, timeout time.Duration) Verdict {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	targets := seedList(ctx, hosts, network.LookupSRV)
	if len(targets) == 0 {
		return Unknown
	}

	verdicts := make(chan Verdict, len(targets))
	for _, target := range targets {
		go func() { verdicts <- checkOne(ctx, target, network) }()
	}
	collected := make([]Verdict, 0, len(targets))
	for range targets {
		verdict := <-verdicts
		if verdict == Reachable {
			// The rest can only add a closed path to an open one. Returning
			// cancels them through the deferred cancel.
			return Reachable
		}
		collected = append(collected, verdict)
	}
	return combine(collected)
}

// combine is the verdict for a store none of whose hosts was reachable.
//
// Unknown wins over everything that is left: a probe that could not be carried
// out says nothing about its host, so a finding drawn from the others would be
// drawn from part of the store. Otherwise any name that resolved and refused
// is the allowlist case, and only a store whose every name is gone is
// Unresolved.
func combine(verdicts []Verdict) Verdict {
	result := Unresolved
	for _, verdict := range verdicts {
		switch verdict {
		case Unknown:
			return Unknown
		case Unreachable:
			result = Unreachable
		}
	}
	return result
}

// seedList turns the reported hosts into "name:port" targets.
//
// A host without a port means one of two things. From a mongodb+srv://
// address, which this machine may still hold from before the app converted
// them, it is the name of an SRV record, which lists the real servers and
// usually has no address of its own — probing it directly would call a
// perfectly good cluster Unresolved. Anything else is MongoDB's default port.
func seedList(ctx context.Context, hosts string, lookupSRV LookupSRV) []string {
	var targets []string
	for _, host := range strings.Split(hosts, ",") {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(host); err == nil {
			targets = append(targets, host)
			continue
		}
		name := strings.Trim(host, "[]")
		if records := srvTargets(ctx, name, lookupSRV); len(records) > 0 {
			targets = append(targets, records...)
			continue
		}
		targets = append(targets, net.JoinHostPort(name, DefaultPort))
	}
	return targets
}

// srvTargets is what an SRV record for a MongoDB cluster lists, or nothing when
// the name has no such record or it could not be read.
func srvTargets(ctx context.Context, name string, lookupSRV LookupSRV) []string {
	if lookupSRV == nil || net.ParseIP(name) != nil {
		return nil
	}
	_, records, err := lookupSRV(ctx, "mongodb", "tcp", name)
	if err != nil {
		return nil
	}
	targets := make([]string, 0, len(records))
	for _, record := range records {
		// DNS names come back fully qualified, with the root dot.
		target := strings.TrimSuffix(record.Target, ".")
		targets = append(targets, net.JoinHostPort(target, strconv.Itoa(int(record.Port))))
	}
	return targets
}

// checkOne is the verdict for a single "name:port".
func checkOne(ctx context.Context, hostPort string, network Network) Verdict {
	host, _, err := net.SplitHostPort(hostPort)
	if err != nil || host == "" {
		return Unknown
	}

	if verdict := resolves(ctx, host, network.LookupHost); verdict != Reachable {
		return verdict
	}

	conn, err := network.Dial(ctx, "tcp", hostPort)
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
