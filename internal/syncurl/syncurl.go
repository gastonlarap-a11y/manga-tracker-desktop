// Package syncurl checks a connection string before it is stored.
//
// Synchronising to a database of your own is optional — with nothing
// configured the library lives in the local SQLite file and there is no sync at
// all. But when someone does configure it, a string that cannot work should be
// refused at the point of typing rather than accepted and left to fail later
// with nothing on screen to explain it.
//
// The reasons are returned as codes, not sentences: the window owns the wording
// so every message the user reads is written in one place, in one language.
package syncurl

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// srvScheme is the form every hosted MongoDB hands out, and the one that has to
// be converted before it is stored.
const srvScheme = "mongodb+srv://"

// Problem is why a connection string was refused. The empty value means it was
// not refused.
type Problem string

const (
	None Problem = ""
	// Empty — nothing was typed.
	Empty Problem = "empty"
	// SRV — a mongodb+srv:// URL.
	//
	// It resolves fine on macOS and never connects on Windows: Bun there does
	// not read the system DNS servers (dns.getServers() answers 127.0.0.1), so
	// every SRV lookup fails while the OS resolver answers normally. Storing
	// one guarantees a sync that silently never runs on that machine, so it is
	// refused rather than warned about.
	SRV Problem = "srv"
	// NotMongo — not a MongoDB connection string at all.
	NotMongo Problem = "notMongo"
	// NoHost — an address with no server in it.
	NoHost Problem = "noHost"
	// CredentialsInAddress — the address already carries a `user:pass@` and the
	// separate fields were filled in too. Answered rather than guessed: picking
	// one silently is how someone ends up staring at an authentication failure
	// for a password they are sure they typed.
	CredentialsInAddress Problem = "credentialsInAddress"
	// NoUser — a password with nobody to go with it.
	NoUser Problem = "noUser"
	// SrvUnresolved — a mongodb+srv:// address whose DNS record could not be
	// read, so there is nothing to convert it into.
	SrvUnresolved Problem = "srvUnresolved"
)

// Validate reports whether the string can be stored.
func Validate(raw string) Problem {
	value := strings.TrimSpace(raw)
	switch {
	case value == "":
		return Empty
	case strings.HasPrefix(value, srvScheme):
		return SRV
	case !strings.HasPrefix(value, "mongodb://"):
		return NotMongo
	default:
		return None
	}
}

// LookupSRV has the signature of (*net.Resolver).LookupSRV — the context-aware
// one; the package-level net.LookupSRV takes none and cannot be cancelled.
// Injected for the same reason every other outside call in this project is: a
// test that resolved real DNS would fail on a train.
type LookupSRV func(ctx context.Context, service, proto, name string) (string, []*net.SRV, error)

// Resolve turns a `mongodb+srv://` address into the direct form, and leaves
// anything else exactly as it was.
//
// This is the address Azure, Atlas and every other hosted MongoDB hands out, so
// refusing it means telling people to go and resolve a DNS record by hand. It
// is refused for a real reason — on Windows Bun does not read the system DNS
// servers, so an srv URL there produces a sync that silently never runs — but
// **Go resolves it fine on both systems**, because it asks the OS rather than
// reading resolv.conf. So the app looks the record up once, at the moment
// someone pastes it, and stores an address that works on either machine.
//
// The TXT record, which the connection-string spec also allows for options, is
// deliberately ignored: no cluster this has met publishes one, and reading it
// would be code with no case behind it.
func Resolve(ctx context.Context, raw string, lookup LookupSRV) (string, Problem) {
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(value, srvScheme) {
		return value, None
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", NotMongo
	}
	if parsed.Hostname() == "" {
		return "", NoHost
	}

	_, records, err := lookup(ctx, "mongodb", "tcp", parsed.Hostname())
	if err != nil || len(records) == 0 {
		return "", SrvUnresolved
	}

	// Every target, not only the first: a real replica set publishes several,
	// and a seed list is what lets the driver survive one of them being down.
	hosts := make([]string, 0, len(records))
	for _, record := range records {
		// DNS names come back fully qualified, with the root dot.
		hosts = append(hosts, fmt.Sprintf("%s:%d", strings.TrimSuffix(record.Target, "."), record.Port))
	}

	parsed.Scheme = "mongodb"
	parsed.Host = strings.Join(hosts, ",")

	// `mongodb+srv` implies TLS; plain `mongodb` does not. Dropping the scheme
	// without saying so would turn an encrypted connection into a refused one,
	// and the error a cluster gives for that names neither TLS nor this change.
	query := parsed.Query()
	if !query.Has("tls") && !query.Has("ssl") {
		query.Set("tls", "true")
		parsed.RawQuery = query.Encode()
	}

	return parsed.String(), None
}

// Credentials are what someone types when they do not have a whole connection
// string to paste — which is most people, most of the time.
//
// Address may be a bare `host:port` or a full `mongodb://…` with the options
// already on it; either way it carries no user and no password.
type Credentials struct {
	Address  string
	User     string
	Password string
}

// Build assembles a connection string from the pieces.
//
// This exists for one reason: a MongoDB URI is a URL, so a password containing
// `@`, `:`, `/`, `?`, `#` or `%` has to be percent-encoded inside it. Typed
// straight into a single address field it is not, and the result is not a
// helpful error — it is an authentication failure, or a driver parsing the
// password as a hostname. Passwords out of a manager contain exactly those
// characters.
//
// The escaping is `net/url`'s, never hand-written: it already knows that
// userinfo escapes `@ : / ? #` and leaves the sub-delimiters alone, and a
// second implementation of that table would be wrong in some corner nobody
// tests.
func Build(c Credentials) (string, Problem) {
	address := strings.TrimSpace(c.Address)
	user := strings.TrimSpace(c.User)
	// Deliberately not trimmed: a leading or trailing space can be part of a
	// password, and silently dropping it produces a failure nobody can see.
	password := c.Password

	switch {
	case address == "":
		return "", Empty
	case user == "" && password != "":
		return "", NoUser
	}

	parsed, problem := parseAddress(address)
	if problem != None {
		return "", problem
	}
	if parsed.User != nil && (user != "" || password != "") {
		return "", CredentialsInAddress
	}
	if parsed.Host == "" {
		return "", NoHost
	}

	if user != "" {
		if password == "" {
			parsed.User = url.User(user)
		} else {
			parsed.User = url.UserPassword(user, password)
		}
	}
	return parsed.String(), None
}

// parseAddress accepts what someone would reasonably type: `host:27017`,
// `mongodb://host:27017`, or either with options hanging off it.
func parseAddress(address string) (*url.URL, Problem) {
	withScheme := address
	if !strings.Contains(address, "://") {
		// A bare `host:27017` parses as scheme "host", opaque "27017" — so the
		// scheme goes on before parsing rather than being detected after.
		withScheme = "mongodb://" + address
	}

	if problem := Validate(withScheme); problem != None {
		return nil, problem
	}
	parsed, err := url.Parse(withScheme)
	if err != nil {
		return nil, NotMongo
	}
	return parsed, None
}

// DefaultDatabase is used when the user gives a URL and no database name.
const DefaultDatabase = "mangatracker"

// Database falls back rather than demanding a second field: the name matters
// far less than the URL, and asking for both makes an optional feature feel
// like a form.
func Database(raw string) string {
	if name := strings.TrimSpace(raw); name != "" {
		return name
	}
	return DefaultDatabase
}
