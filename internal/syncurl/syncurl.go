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
	"net/url"
	"strings"
)

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
)

// Validate reports whether the string can be stored.
func Validate(raw string) Problem {
	value := strings.TrimSpace(raw)
	switch {
	case value == "":
		return Empty
	case strings.HasPrefix(value, "mongodb+srv://"):
		return SRV
	case !strings.HasPrefix(value, "mongodb://"):
		return NotMongo
	default:
		return None
	}
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
