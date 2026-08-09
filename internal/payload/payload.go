// Package payload carries the backend inside the app binary.
//
// The point of this whole project is that someone downloads one file and has a
// working tracker. That means the server, its migrations, the dashboard build
// and a Bun to run them all travel inside the app and get written out on first
// launch.
//
// Embedded rather than placed beside the executable as a bundle resource: on
// macOS the app is ad-hoc signed, and the signature covers the binary — so a
// payload inside it is covered too, with nothing to re-sign after the fact.
//
// A development build has no payload. `dist/` holds only a VERSION marker in
// git, and the release workflow fills it. `Available` is how the app tells the
// two apart, so `wails dev` keeps working instead of failing to compile.
package payload

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed all:dist
var dist embed.FS

const (
	// The file whose presence means a real payload was built in.
	backendEntry = "dist/backend/index.js"
	// Written next to the extracted tree so a second launch is a string
	// comparison instead of tens of thousands of file writes.
	versionMarker = ".payload-version"
)

// Version identifies what is embedded — a release tag, or "dev".
func Version() string {
	raw, err := dist.ReadFile("dist/VERSION")
	if err != nil {
		return "dev"
	}
	return strings.TrimSpace(string(raw))
}

// Available reports whether this build actually carries a backend.
func Available() bool {
	_, err := fs.Stat(dist, backendEntry)
	return err == nil
}

// ErrNoPayload is what a development build gives back: not a fault, just a
// build that was never meant to install anything.
var ErrNoPayload = errors.New("this build carries no bundled backend")

// ExtractedAt reports whether `dir` already holds this exact payload.
func ExtractedAt(dir string) bool {
	return markerAt(dir) == Version()
}

// Extract writes the payload into dir, skipping the work when the same version
// is already there.
func Extract(dir string) error {
	if !Available() {
		return ErrNoPayload
	}
	return extractFS(dist, "dist", dir, Version())
}

// extractFS takes the source as a parameter so the extraction itself can be
// tested. A development build embeds nothing, so without this the riskiest part
// of the package — permissions, the marker, replacing an older version — would
// only ever run for the first time on a user's machine.
//
// The marker is written last on purpose: a run that dies halfway leaves none,
// so the next one starts over instead of trusting a half-written tree.
func extractFS(source fs.FS, root, dir, version string) error {
	if markerAt(dir) == version {
		return nil
	}
	// A previous version, or the remains of an interrupted extraction.
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("could not clear %s: %w", dir, err)
	}

	err := fs.WalkDir(source, root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, strings.TrimPrefix(path, root))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(source, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, modeFor(path))
	})
	if err != nil {
		return fmt.Errorf("could not extract the bundled backend: %w", err)
	}

	return os.WriteFile(filepath.Join(dir, versionMarker), []byte(version), 0o644)
}

func markerAt(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, versionMarker))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// go:embed does not keep permissions — everything comes back read-only — so the
// interpreter has to be made executable explicitly. Without this the install
// fails with "permission denied" on a file that is plainly there, which is a
// miserable thing to debug.
func modeFor(path string) os.FileMode {
	if IsExecutable(path) {
		return 0o755
	}
	return 0o644
}

// IsExecutable reports whether an embedded path is the interpreter.
func IsExecutable(path string) bool {
	base := filepath.Base(path)
	return base == "bun" || base == "bun.exe"
}
