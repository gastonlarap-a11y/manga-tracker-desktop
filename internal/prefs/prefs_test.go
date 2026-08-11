package prefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOnAMachineThatNeverChose(t *testing.T) {
	// The common case, and the one that must not look like a failure: no file
	// means no preference, so the caller falls back to the system default.
	got, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load on an empty directory: %v", err)
	}
	if got != (Prefs{}) {
		t.Errorf("Load = %+v, want the zero value", got)
	}
}

func TestSaveThenLoad(t *testing.T) {
	tests := []struct {
		name  string
		prefs Prefs
	}{
		{name: "a chosen browser", prefs: Prefs{BrowserID: "brave"}},
		{name: "back to the system default", prefs: Prefs{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := Save(dir, test.prefs); err != nil {
				t.Fatalf("Save: %v", err)
			}
			got, err := Load(dir)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got != test.prefs {
				t.Errorf("Load = %+v, want %+v", got, test.prefs)
			}
		})
	}
}

func TestSaveOverwritesTheEarlierChoice(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, Prefs{BrowserID: "chrome"}); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	if err := Save(dir, Prefs{BrowserID: "brave"}); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.BrowserID != "brave" {
		t.Errorf("BrowserID = %q, want the newer choice %q", got.BrowserID, "brave")
	}
}

func TestLoadReportsAFileItCannotRead(t *testing.T) {
	// The distinction the window depends on: this is "I could not find out",
	// not "nothing was chosen", and it must not arrive as the zero value alone.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("writing a corrupt file: %v", err)
	}

	if _, err := Load(dir); err == nil {
		t.Error("Load on a corrupt file returned no error")
	}
}

func TestSaveCreatesTheDirectory(t *testing.T) {
	// First run on a machine whose data directory does not exist yet.
	dir := filepath.Join(t.TempDir(), "MangaTracker")
	if err := Save(dir, Prefs{BrowserID: "edge"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.BrowserID != "edge" {
		t.Errorf("BrowserID = %q, want %q", got.BrowserID, "edge")
	}
}

func TestSaveLeavesNoTemporaryFilesBehind(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, Prefs{BrowserID: "chrome"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != fileName {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("directory holds %v, want only %q", names, fileName)
	}
}
