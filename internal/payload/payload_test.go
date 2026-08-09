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
		"dist/VERSION":                 {Data: []byte(version + "\n")},
		"dist/app/bun":                 {Data: []byte("ELF")},
		"dist/app/index.js":            {Data: []byte("// server")},
		"dist/app/service.js":          {Data: []byte("// cli")},
		"dist/app/migrations/1/up.sql": {Data: []byte("CREATE TABLE t(id);")},
		"dist/app/public/index.html":   {Data: []byte("<title>Manga Tracker</title>")},
		"dist/extension/manifest.json": {Data: []byte(`{"name":"Manga Tracker"}`)},
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
		"app/bun",
		"app/index.js",
		"app/service.js",
		"app/migrations/1/up.sql",
		"app/public/index.html",
		"extension/manifest.json",
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

	info, err := os.Stat(filepath.Join(dir, "app", "bun"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("bun is not executable: %v", info.Mode().Perm())
	}
	// And nothing else is, so the payload does not hand out execute bits.
	server, err := os.Stat(filepath.Join(dir, "app", "index.js"))
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
	sentinel := filepath.Join(dir, "app", "touched")
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

func TestExtractLeavesTheRestOfTheDataDirectoryAlone(t *testing.T) {
	// The one that matters. Extraction wipes its destination so a file from an
	// older version cannot survive an upgrade — and the destination used to be
	// the data directory itself, which is where the library and every backup
	// beside it live. Pressing Install would have deleted all of them.
	dataDir := t.TempDir()
	library := filepath.Join(dataDir, "mangatracker.db")
	backup := filepath.Join(dataDir, "mangatracker-pre-merge.db")
	for _, path := range []string{library, backup} {
		if err := os.WriteFile(path, []byte("irreplaceable"), 0o644); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if err := extractFS(fakePayload("v1"), "dist", RuntimeDir(dataDir), "v1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, path := range []string{library, backup} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s did not survive the extraction: %v", filepath.Base(path), err)
		}
	}
	// And a second extraction, which is what an upgrade does, must not either.
	if err := extractFS(fakePayload("v2"), "dist", RuntimeDir(dataDir), "v2"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(library); err != nil {
		t.Errorf("the library did not survive an upgrade: %v", err)
	}
}

func TestRuntimeDirIsTheOnlyThingOwned(t *testing.T) {
	// Stated as a test because it is the invariant the whole fix rests on.
	if RuntimeDir("/data") != filepath.Join("/data", RuntimeSubdir) {
		t.Errorf("unexpected runtime dir: %s", RuntimeDir("/data"))
	}
}

func TestExtractReplacesAnOlderVersion(t *testing.T) {
	dir := t.TempDir()
	if err := extractFS(fakePayload("v1"), "dist", dir, "v1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stale := filepath.Join(dir, "app", "removed-in-v2.js")
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

func TestUpgradeSurvivesADirectoryThatCannotBeDeleted(t *testing.T) {
	// The Windows update case, reproduced with permissions because that is the
	// portable way to make a deletion fail: Windows will not unlink a running
	// executable, and on an update the backend is running out of this very
	// tree. Renaming it is allowed there, and frees the path.
	if runtime.GOOS == "windows" {
		t.Skip("Windows ignores POSIX permissions; the case it stands for is its own")
	}
	if os.Getuid() == 0 {
		t.Skip("root deletes regardless of permissions")
	}
	dir := t.TempDir()
	if err := extractFS(fakePayload("v1"), "dist", dir, "v1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	locked := filepath.Join(dir, "app")
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// It ends up under the retired copy, and Go's own TempDir cleanup cannot
	// remove a directory it is not allowed to write to either.
	t.Cleanup(func() {
		_ = os.Chmod(locked, 0o755)
		_ = os.Chmod(filepath.Join(dir+retiredSuffix, "app"), 0o755)
	})

	if err := extractFS(fakePayload("v2"), "dist", dir, "v2"); err != nil {
		t.Fatalf("the upgrade should have moved the old tree aside: %v", err)
	}

	if markerAt(dir) != "v2" {
		t.Errorf("expected the marker to read v2, got %q", markerAt(dir))
	}
	if _, err := os.Stat(filepath.Join(dir, "app", "index.js")); err != nil {
		t.Errorf("the new version was not written: %v", err)
	}
	if _, err := os.Stat(dir + retiredSuffix); err != nil {
		t.Errorf("the old tree should have been retired beside it: %v", err)
	}
}

func TestARetiredTreeIsClearedOnTheNextUpgrade(t *testing.T) {
	// Whatever could not be deleted while it was in use is deleted later, so a
	// machine does not accumulate one copy of the backend per release.
	dir := t.TempDir()
	retired := dir + retiredSuffix
	if err := os.MkdirAll(filepath.Join(retired, "app"), 0o755); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := extractFS(fakePayload("v1"), "dist", dir, "v1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(retired); !errors.Is(err, fs.ErrNotExist) {
		t.Error("the retired tree from a previous upgrade was not cleared")
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
	if !IsExecutable("dist/app/bun") || !IsExecutable("dist/app/bun.exe") {
		t.Error("the interpreter must be marked executable")
	}
	if IsExecutable("dist/app/index.js") || IsExecutable("dist/app/public/bundle.js") {
		t.Error("nothing but the interpreter should be executable")
	}
}
