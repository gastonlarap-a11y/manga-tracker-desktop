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
	backendEntry = "dist/app/index.js"
	// Written next to the extracted tree so a second launch is a string
	// comparison instead of tens of thousands of file writes.
	versionMarker = ".payload-version"

	// RuntimeSubdir is the only directory this package owns, and the only one
	// it is allowed to delete.
	//
	// Extraction wipes its destination before rewriting it, so that a leftover
	// file from an older version cannot survive an upgrade. Pointing that at
	// the data directory would have wiped the library along with it: the
	// database, and every backup beside it, live there. The payload gets a
	// directory of its own precisely so that "delete everything here" can never
	// mean anything else.
	RuntimeSubdir = "runtime"
	// AppSubdir is where the backend lands, and is what the service CLI is
	// given as --app-dir. Flat on purpose: the CLI looks for the interpreter at
	// <app-dir>/bun and runs index.js from there.
	AppSubdir = "app"
	// ExtensionSubdir holds the MV3 build, for loading unpacked while the
	// store review is pending.
	ExtensionSubdir = "extension"
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

// RuntimeDir is where the payload lives inside a data directory. Nothing else
// in `dataDir` is this package's business.
func RuntimeDir(dataDir string) string {
	return filepath.Join(dataDir, RuntimeSubdir)
}

// ExtractedAt reports whether the payload in `dataDir` is already this version.
func ExtractedAt(dataDir string) bool {
	return markerAt(RuntimeDir(dataDir)) == Version()
}

// Extract writes the payload into `dataDir/runtime`, skipping the work when the
// same version is already there.
//
// It takes the data directory rather than the destination so that no caller can
// aim the deletion somewhere else by mistake — which is exactly how the library
// nearly got wiped.
func Extract(dataDir string) error {
	if !Available() {
		return ErrNoPayload
	}
	return extractFS(dist, "dist", RuntimeDir(dataDir), Version())
}

// extractFS takes the source as a parameter so the extraction itself can be
// tested. A development build embeds nothing, so without this the riskiest part
// of the package — permissions, the marker, replacing an older version — would
// only ever run for the first time on a user's machine.
//
// `dir` must be a directory this package owns: it is deleted before it is
// rewritten. See RuntimeSubdir.
//
// The marker is written last on purpose: a run that dies halfway leaves none,
// so the next one starts over instead of trusting a half-written tree.
func extractFS(source fs.FS, root, dir, version string) error {
	if markerAt(dir) == version {
		return nil
	}
	// A previous version, or the remains of an interrupted extraction.
	if err := clearForRewrite(dir); err != nil {
		return err
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

// retiredSuffix names the old tree while it is on its way out. A fixed name
// rather than a timestamped one: at most one can be pending, and the next
// launch has to be able to find and finish removing it.
const retiredSuffix = ".old"

// clearForRewrite empties the payload directory so a new version can be written
// into it.
//
// Deleting it is the normal path. Windows refuses to delete a directory holding
// a running executable, and updating is exactly the moment when one is running:
// the backend is a service started at login, and it runs `bun.exe` out of this
// very tree. Windows does allow *renaming* it, though, which frees the path
// immediately — the running process keeps the files it already opened, and what
// is left behind is removed on a later launch, once nothing is using it.
//
// Without this an update on Windows fails on the first file it tries to unlink,
// leaving the machine on the old version with no way to move forward short of
// uninstalling.
func clearForRewrite(dir string) error {
	// A rename from a previous update that could not be cleaned up yet. Best
	// effort: if it is still in use, the retirement below is what matters.
	retired := dir + retiredSuffix
	_ = os.RemoveAll(retired)

	err := os.RemoveAll(dir)
	if err == nil {
		return nil
	}
	if renameErr := os.Rename(dir, retired); renameErr != nil {
		return fmt.Errorf("could not clear %s: %w", dir, err)
	}
	return nil
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
