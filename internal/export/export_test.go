package export

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// backend answers the export route the way the API does, recording the format
// it was asked for. Anything else is a 404, as on the real server.
func backend(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/library/export" {
			http.NotFound(w, r)
			return
		}
		handle(w, r)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func serving(contentType, body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}
}

// leftovers lists what is in dir besides the file wanted: a temporary file
// left behind by a save that failed.
func leftovers(t *testing.T, dir, wanted string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	var others []string
	for _, entry := range entries {
		if entry.Name() != wanted {
			others = append(others, entry.Name())
		}
	}
	return others
}

func TestSave(t *testing.T) {
	t.Parallel()

	const backup = `{"format":"manga-tracker-library","version":1,"series":[]}`
	// The UTF-8 byte order mark the API starts its CSV with, as bytes: Go
	// refuses a literal one anywhere but at the start of a source file.
	const sheet = "\xEF\xBB\xBFtitle,status\r\nBerserk,on_hold\r\n"

	tests := []struct {
		name    string
		format  Format
		handle  func(http.ResponseWriter, *http.Request)
		want    string
		wantErr error
	}{
		{
			name:   "a JSON export lands whole",
			format: JSON,
			handle: serving("application/json; charset=utf-8", backup),
			want:   backup,
		},
		{
			name:   "a CSV export lands whole",
			format: CSV,
			handle: serving("text/csv; charset=utf-8; header=present", sheet),
			want:   sheet,
		},
		{
			name:    "an older backend without the export",
			format:  JSON,
			handle:  http.NotFound,
			wantErr: ErrUnsupported,
		},
		{
			name:   "a backend that failed",
			format: JSON,
			handle: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
			},
			wantErr: ErrDownload,
		},
		{
			name:    "something that is not the export",
			format:  JSON,
			handle:  serving("text/html; charset=utf-8", "<html>proxy login</html>"),
			wantErr: ErrDownload,
		},
		{
			name:    "the other format's export",
			format:  CSV,
			handle:  serving("application/json", backup),
			wantErr: ErrDownload,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			baseURL := backend(t, test.handle)
			dir := t.TempDir()
			path := filepath.Join(dir, FileName(test.format, time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)))

			written, err := Save(context.Background(), Client(), baseURL, test.format, path)

			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("Save() error = %v, want %v", err, test.wantErr)
				}
				if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
					t.Errorf("a failed save left %s behind", path)
				}
				if others := leftovers(t, dir, filepath.Base(path)); len(others) > 0 {
					t.Errorf("a failed save left %v behind", others)
				}
				return
			}
			if err != nil {
				t.Fatalf("Save() error = %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading the export: %v", err)
			}
			if string(got) != test.want {
				t.Errorf("saved %q, want %q", got, test.want)
			}
			if written != int64(len(test.want)) {
				t.Errorf("Save() = %d bytes, want %d", written, len(test.want))
			}
			if others := leftovers(t, dir, filepath.Base(path)); len(others) > 0 {
				t.Errorf("a save left %v behind", others)
			}
		})
	}
}

func TestSaveAsksForTheFormat(t *testing.T) {
	t.Parallel()
	var asked string
	baseURL := backend(t, func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Query().Get("format")
		serving("text/csv", "title\r\n")(w, r)
	})

	if _, err := Save(context.Background(), Client(), baseURL, CSV, filepath.Join(t.TempDir(), "x.csv")); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if asked != "csv" {
		t.Errorf("asked for format %q, want csv", asked)
	}
}

// The reason it writes beside the file and renames: someone exporting over
// last month's backup must not lose it to a transfer that stopped halfway.
func TestSaveLeavesThePreviousFileAloneWhenTheTransferStops(t *testing.T) {
	t.Parallel()
	baseURL := backend(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Promises more than it sends, then hangs up: what a backend failing
		// halfway through its stream looks like from here.
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte(`{"format":"manga-tracker-library","series":[`))
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "backup.json")
	if err := os.WriteFile(path, []byte("last month's backup"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Save(context.Background(), Client(), baseURL, JSON, path)

	if !errors.Is(err, ErrDownload) {
		t.Fatalf("Save() error = %v, want %v", err, ErrDownload)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil || string(got) != "last month's backup" {
		t.Errorf("the previous file is now %q (%v)", got, readErr)
	}
	if others := leftovers(t, dir, "backup.json"); len(others) > 0 {
		t.Errorf("a failed save left %v behind", others)
	}
}

func TestSaveIntoAFolderThatCannotBeWritten(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("a read-only folder is not expressed with permission bits on Windows")
	}
	baseURL := backend(t, serving("application/json", `{}`))
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	_, err := Save(context.Background(), Client(), baseURL, JSON, filepath.Join(locked, "backup.json"))

	if !errors.Is(err, ErrWrite) {
		t.Fatalf("Save() error = %v, want %v", err, ErrWrite)
	}
}

func TestSaveWithNoBackendThere(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.NotFoundHandler())
	baseURL := server.URL
	server.Close()

	_, err := Save(context.Background(), Client(), baseURL, JSON, filepath.Join(t.TempDir(), "x.json"))

	if !errors.Is(err, ErrDownload) {
		t.Fatalf("Save() error = %v, want %v", err, ErrDownload)
	}
}

func TestParseFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw    string
		want   Format
		wantOK bool
	}{
		{raw: "json", want: JSON, wantOK: true},
		{raw: "csv", want: CSV, wantOK: true},
		{raw: "xml"},
		{raw: "JSON"},
		{raw: ""},
	}
	for _, test := range tests {
		t.Run(test.raw, func(t *testing.T) {
			t.Parallel()
			got, ok := ParseFormat(test.raw)
			if got != test.want || ok != test.wantOK {
				t.Errorf("ParseFormat(%q) = %q, %v; want %q, %v", test.raw, got, ok, test.want, test.wantOK)
			}
		})
	}
}

func TestFileNameAndPattern(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 10, 13, 23, 30, 0, 0, time.Local)

	if got := FileName(CSV, day); got != "manga-tracker-2026-10-13.csv" {
		t.Errorf("FileName() = %q", got)
	}
	if got := Pattern(JSON); got != "*.json" {
		t.Errorf("Pattern() = %q", got)
	}
	if !strings.HasSuffix(FileName(JSON, day), Pattern(JSON)[1:]) {
		t.Error("the proposed name does not match its own filter")
	}
}
