// Package prefs stores what the window remembers between runs.
//
// It lives beside the database in the data directory rather than inside the
// payload tree, which an update replaces wholesale — a preference that
// disappeared on every update would be worse than not offering one.
//
// Reading a missing file is not an error: nothing has been chosen yet, and the
// caller gets the zero value. Reading a file that is there but unreadable IS an
// error, and the distinction is the point. "I could not find out" must never
// arrive as "the default is in effect", or the screen ends up confidently
// stating a choice the person did not make.
package prefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// fileName is the preferences file inside the data directory.
const fileName = "preferences.json"

// Prefs is every setting the window keeps on this machine.
type Prefs struct {
	// BrowserID is which installed browser opens a chapter link, as an id from
	// internal/browsers. Empty means the system default: someone who never
	// chose gets the behaviour they had before there was anything to choose.
	BrowserID string `json:"browserId"`
}

// Load reads the preferences stored in dir.
//
// A missing file yields the zero value and no error — that is a machine where
// nothing has been chosen, not a failure.
func Load(dir string) (Prefs, error) {
	data, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Prefs{}, nil
		}
		return Prefs{}, fmt.Errorf("reading preferences: %w", err)
	}
	var p Prefs
	if err := json.Unmarshal(data, &p); err != nil {
		return Prefs{}, fmt.Errorf("parsing preferences: %w", err)
	}
	return p, nil
}

// Save writes the preferences into dir, creating it if it is not there yet.
//
// Written to a temporary file and renamed over the target, so a run that dies
// mid-write leaves the previous file intact instead of a truncated one that the
// next Load would report as unreadable.
func Save(dir string, p Prefs) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding preferences: %w", err)
	}
	temp, err := os.CreateTemp(dir, fileName+".*")
	if err != nil {
		return fmt.Errorf("creating temporary preferences file: %w", err)
	}
	tempName := temp.Name()
	// Best effort: on the happy path the file is already closed and renamed, so
	// both of these fail harmlessly. On every error path they are the cleanup.
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()

	if _, err := temp.Write(data); err != nil {
		return fmt.Errorf("writing preferences: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("closing preferences: %w", err)
	}
	if err := os.Rename(tempName, filepath.Join(dir, fileName)); err != nil {
		return fmt.Errorf("replacing preferences: %w", err)
	}
	return nil
}
