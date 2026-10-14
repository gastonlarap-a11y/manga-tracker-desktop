// Package export saves a copy of the library, as the running backend serves
// it, into a file someone chose.
//
// It exists because the window cannot do it: Wails 2.16 implements no download
// handling in the macOS webview — no WKDownload, nothing deciding a navigation
// response — so a download link inside the embedded dashboard does nothing at
// all. The app asks the backend for the export itself and writes the file.
//
// Pure Go with no Wails import: the save dialog is the caller's, and this takes
// the path it answered.
package export

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// Format is one of the two files the backend exports.
type Format string

const (
	// JSON is the whole library: every series, its names and every chapter read.
	JSON Format = "json"
	// CSV is one row per series, for a spreadsheet or another tracker.
	CSV Format = "csv"
)

// The ways a save goes wrong that the window words differently, checked with
// errors.Is. Each one wraps the technical detail.
var (
	// ErrUnsupported is a backend older than the export: one run from a
	// checkout of before, answering on the same ports.
	ErrUnsupported = errors.New("the running backend has no export")
	// ErrDownload is the backend refusing, answering something that is not the
	// export, or the transfer stopping halfway.
	ErrDownload = errors.New("the export could not be downloaded")
	// ErrWrite is the chosen place refusing the file.
	ErrWrite = errors.New("the export could not be written")
)

// headerTimeout bounds the wait for the backend to start answering. It brings
// its read model current before the first byte — seconds for a big library
// after an update — so this is generous; the body itself has no bound, since a
// library of a million readings is hundreds of MB.
const headerTimeout = 60 * time.Second

// ParseFormat reads the format the window asked for.
func ParseFormat(raw string) (Format, bool) {
	switch Format(raw) {
	case JSON, CSV:
		return Format(raw), true
	default:
		return "", false
	}
}

// FileName is what the save dialog proposes: manga-tracker-2026-10-13.json.
func FileName(format Format, day time.Time) string {
	return "manga-tracker-" + day.Format("2006-01-02") + "." + string(format)
}

// Pattern is the save dialog's filter for the format, "*.json" or "*.csv".
func Pattern(format Format) string {
	return "*." + string(format)
}

// Client is the HTTP client a save uses: no overall timeout, which would cut a
// big export short, only one on the headers. A fresh one per save, so
// keep-alives are off — a dropped Transport holding idle connections keeps
// them open forever. No proxy: the backend is on this machine.
func Client() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 nil,
			DisableKeepAlives:     true,
			ResponseHeaderTimeout: headerTimeout,
		},
	}
}

// mediaTypes is what the backend sends each format as. Anything else is not
// the export, whatever its status — a proxy's page, an older route — and is
// never written over the file someone chose.
var mediaTypes = map[Format]string{
	JSON: "application/json",
	CSV:  "text/csv",
}

// Save downloads the export from the backend at baseURL into path, all or
// nothing: written beside it under a temporary name and renamed over it only
// once complete, so a transfer that stops halfway leaves whatever was there
// before untouched. Returns the bytes written.
func Save(ctx context.Context, client *http.Client, baseURL string, format Format, path string) (int64, error) {
	endpoint, err := url.JoinPath(baseURL, "api", "library", "export")
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrDownload, err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+url.Values{"format": {string(format)}}.Encode(), nil)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrDownload, err)
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrDownload, err)
	}
	defer response.Body.Close()

	switch {
	case response.StatusCode == http.StatusNotFound:
		return 0, fmt.Errorf("%w: it answered %s", ErrUnsupported, response.Status)
	case response.StatusCode != http.StatusOK:
		return 0, fmt.Errorf("%w: the backend answered %s", ErrDownload, response.Status)
	}
	contentType := response.Header.Get("Content-Type")
	if media, _, err := mime.ParseMediaType(contentType); err != nil || media != mediaTypes[format] {
		return 0, fmt.Errorf("%w: it answered %q, not a %s export", ErrDownload, contentType, format)
	}
	return writeWhole(path, response.Body)
}

// recording remembers a failed write, which is what tells a full disk apart
// from a transfer cut short: io.Copy returns either as its error.
type recording struct {
	file *os.File
	err  error
}

func (r *recording) Write(p []byte) (int, error) {
	n, err := r.file.Write(p)
	if err != nil {
		r.err = err
	}
	return n, err
}

// writeWhole copies body into path through a temporary file beside it.
func writeWhole(path string, body io.Reader) (int64, error) {
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.part")
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrWrite, err)
	}
	tempName := temp.Name()
	// Best effort, as in prefs.Save: on the happy path the file is closed and
	// renamed, so both fail harmlessly; on every error path they are the cleanup.
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()

	target := &recording{file: temp}
	written, err := io.Copy(target, body)
	if err != nil {
		if target.err != nil {
			return written, fmt.Errorf("%w: %w", ErrWrite, target.err)
		}
		return written, fmt.Errorf("%w: the transfer stopped after %d bytes: %w", ErrDownload, written, err)
	}
	// CreateTemp makes it readable by its owner only; an export is a file like
	// any other in the folder it was saved to.
	if err := temp.Chmod(0o644); err != nil {
		return written, fmt.Errorf("%w: %w", ErrWrite, err)
	}
	if err := temp.Sync(); err != nil {
		return written, fmt.Errorf("%w: %w", ErrWrite, err)
	}
	if err := temp.Close(); err != nil {
		return written, fmt.Errorf("%w: %w", ErrWrite, err)
	}
	if err := os.Rename(tempName, path); err != nil {
		return written, fmt.Errorf("%w: %w", ErrWrite, err)
	}
	return written, nil
}
