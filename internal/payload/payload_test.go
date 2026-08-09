package payload

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"testing/fstest"
)

// A payload shaped like the real one, so the extraction is exercised without a
// release build. Without this, permissions and the version marker would run for
// the first time on someone's computer.
func fakePayload(version string) fstest.MapFS {
	return fstest.MapFS{
		"dist/VERSION":                     {Data: []byte(version + "\n")},
		"dist/backend/index.js":            {Data: []byte("// server")},
		"dist/backend/service.js":          {Data: []byte("// cli")},
		"dist/backend/migrations/1/up.sql": {Data: []byte("CREATE TABLE t(id);")},
		"dist/backend/public/index.html":   {Data: []byte("<title>Manga Tracker</title>")},
		"dist/runtime/bun":                 {Data: []byte("ELF")},
	}
}

func TestDevelopmentBuildCarriesNothing(t *testing.T) {
	// `wails dev` has to keep working, and a build with no payload is not a
	// fault — it just never offers to install.
	if Available() {
		t.Error("the committed dist/ must not contain a backend")
	}
	if Version() != "dev" {
		t.Errorf("expected version dev, got %q", Version())
	}
	if err := Extract(t.TempDir()); !errors.Is(err, ErrNoPayload) {
		t.Errorf("expected ErrNoPayload, got %v", err)
	}
}

func TestExtractWritesTheWholeTree(t *testing.T) {
	dir := t.TempDir()

	if err := extractFS(fakePayload("v1"), "dist", dir, "v1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{
		"backend/index.js",
		"backend/service.js",
		"backend/migrations/1/up.sql",
		"backend/public/index.html",
		"runtime/bun",
	} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("%s was not extracted: %v", want, err)
		}
	}
}

func TestExtractMakesTheInterpreterExecutable(t *testing.T) {
	// go:embed hands everything back read-only. Without an explicit mode the
	// install fails with "permission denied" on a file that is plainly there.
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not carry a POSIX executable bit")
	}
	dir := t.TempDir()

	if err := extractFS(fakePayload("v1"), "dist", dir, "v1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "runtime", "bun"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("bun is not executable: %v", info.Mode().Perm())
	}
	// And nothing else is, so the payload does not hand out execute bits.
	server, err := os.Stat(filepath.Join(dir, "backend", "index.js"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if server.Mode().Perm()&0o111 != 0 {
		t.Errorf("index.js should not be executable: %v", server.Mode().Perm())
	}
}

func TestExtractSkipsWorkWhenTheVersionMatches(t *testing.T) {
	dir := t.TempDir()
	if err := extractFS(fakePayload("v1"), "dist", dir, "v1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A file the payload does not contain: it survives only if the second run
	// did nothing.
	sentinel := filepath.Join(dir, "backend", "touched")
	if err := os.WriteFile(sentinel, []byte("x"), 0o644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := extractFS(fakePayload("v1"), "dist", dir, "v1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(sentinel); err != nil {
		t.Error("a matching version should have skipped the extraction entirely")
	}
}

func TestExtractReplacesAnOlderVersion(t *testing.T) {
	dir := t.TempDir()
	if err := extractFS(fakePayload("v1"), "dist", dir, "v1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stale := filepath.Join(dir, "backend", "removed-in-v2.js")
	if err := os.WriteFile(stale, []byte("x"), 0o644); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := extractFS(fakePayload("v2"), "dist", dir, "v2"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Leftovers from the previous version must not survive an upgrade.
	if _, err := os.Stat(stale); !errors.Is(err, fs.ErrNotExist) {
		t.Error("a file from the previous version survived the upgrade")
	}
	if markerAt(dir) != "v2" {
		t.Errorf("expected the marker to read v2, got %q", markerAt(dir))
	}
}

func TestAnInterruptedExtractionIsNotTrusted(t *testing.T) {
	// The marker is written last, so a tree without one is re-extracted rather
	// than used half-written.
	dir := t.TempDir()
	if err := extractFS(fakePayload("v1"), "dist", dir, "v1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, versionMarker)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ExtractedAt(dir) {
		t.Error("a tree with no marker must not count as extracted")
	}
}

func TestIsExecutableOnlyMatchesTheInterpreter(t *testing.T) {
	if !IsExecutable("dist/runtime/bun") || !IsExecutable("dist/runtime/bun.exe") {
		t.Error("the interpreter must be marked executable")
	}
	if IsExecutable("dist/backend/index.js") || IsExecutable("dist/backend/public/bundle.js") {
		t.Error("nothing but the interpreter should be executable")
	}
}
