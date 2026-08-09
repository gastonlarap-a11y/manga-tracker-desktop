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

import "strings"

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
