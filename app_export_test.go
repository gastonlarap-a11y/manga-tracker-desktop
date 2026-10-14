package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"manga-tracker-desktop/internal/installer"
)

// exportingBackend answers the export route with `body`, counting the times it
// was asked — which is how a test tells an export that never reached the
// backend from one that did.
func exportingBackend(t *testing.T, contentType, body string) (string, *atomic.Int32) {
	t.Helper()
	asked := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/library/export" {
			http.NotFound(w, r)
			return
		}
		asked.Add(1)
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server.URL, asked
}

// dialog records what the save dialog was opened with and answers `path`.
type dialog struct {
	opened  int
	options runtime.SaveDialogOptions
	path    string
}

func (d *dialog) ask(_ context.Context, options runtime.SaveDialogOptions) (string, error) {
	d.opened++
	d.options = options
	return d.path, nil
}

func appExporting(t *testing.T, baseURL string, save *dialog) *App {
	t.Helper()
	return &App{
		ctx: context.Background(),
		deps: installer.Deps{
			DataDir:  t.TempDir(),
			Discover: func(context.Context) string { return baseURL },
		},
		saveDialog: save.ask,
	}
}

var labels = ExportLabels{Title: "Exportar la biblioteca", FileKind: "Respaldo (JSON)"}

func TestExportLibrarySavesWhereTheDialogSaid(t *testing.T) {
	t.Parallel()
	const backup = `{"format":"manga-tracker-library","version":1,"series":[]}`
	baseURL, _ := exportingBackend(t, "application/json; charset=utf-8", backup)
	save := &dialog{path: filepath.Join(t.TempDir(), "mi-respaldo.json")}

	outcome, err := appExporting(t, baseURL, save).ExportLibrary("json", labels)

	if err != nil {
		t.Fatalf("ExportLibrary() error = %v", err)
	}
	if outcome.State != "saved" || outcome.Path != save.path || outcome.Bytes != int64(len(backup)) {
		t.Fatalf("ExportLibrary() = %+v", outcome)
	}
	if got, _ := os.ReadFile(save.path); string(got) != backup {
		t.Errorf("saved %q, want the backend's export", got)
	}
	// The window's words, and a name and filter the format agrees with.
	if save.options.Title != labels.Title || len(save.options.Filters) != 1 ||
		save.options.Filters[0].DisplayName != labels.FileKind || save.options.Filters[0].Pattern != "*.json" {
		t.Errorf("dialog opened with %+v", save.options)
	}
	if filepath.Ext(save.options.DefaultFilename) != ".json" {
		t.Errorf("proposed name %q, want a .json", save.options.DefaultFilename)
	}
}

func TestExportLibraryCancelledIsNotAFailure(t *testing.T) {
	t.Parallel()
	baseURL, asked := exportingBackend(t, "text/csv", "title\r\n")
	save := &dialog{path: ""}

	outcome, err := appExporting(t, baseURL, save).ExportLibrary("csv", labels)

	if err != nil || outcome.State != "cancelled" {
		t.Fatalf("ExportLibrary() = %+v, %v; want cancelled", outcome, err)
	}
	if asked.Load() != 0 {
		t.Errorf("the backend was asked %d times after a cancel", asked.Load())
	}
}

// Picking a place for a file that cannot be produced is a question wasted.
func TestExportLibraryWithNoBackendNeverOpensTheDialog(t *testing.T) {
	t.Parallel()
	save := &dialog{path: filepath.Join(t.TempDir(), "x.json")}

	outcome, err := appExporting(t, "", save).ExportLibrary("json", labels)

	if err != nil || outcome.State != "failed" || outcome.Problem != "no-backend" {
		t.Fatalf("ExportLibrary() = %+v, %v; want failed/no-backend", outcome, err)
	}
	if save.opened != 0 {
		t.Error("the save dialog opened with no backend to export from")
	}
}

func TestExportLibraryReportsAnOlderBackendAsUnsupported(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	save := &dialog{path: filepath.Join(t.TempDir(), "x.json")}

	outcome, err := appExporting(t, server.URL, save).ExportLibrary("json", labels)

	if err != nil || outcome.State != "failed" || outcome.Problem != "unsupported" {
		t.Fatalf("ExportLibrary() = %+v, %v; want failed/unsupported", outcome, err)
	}
	if outcome.Detail == "" {
		t.Error("no technical detail under the failure")
	}
}

func TestExportLibraryRefusesAFormatItDoesNotKnow(t *testing.T) {
	t.Parallel()
	save := &dialog{path: filepath.Join(t.TempDir(), "x.xml")}

	_, err := appExporting(t, "http://127.0.0.1:1", save).ExportLibrary("xml", labels)

	if err == nil || err.Error() != "bad-format" {
		t.Fatalf("ExportLibrary(xml) error = %v, want bad-format", err)
	}
	if save.opened != 0 {
		t.Error("the save dialog opened for a format there is no export of")
	}
}
