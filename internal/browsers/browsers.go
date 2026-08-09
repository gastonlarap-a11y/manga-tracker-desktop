// Package browsers finds the Chromium browsers installed on this machine and
// opens a page in one of them.
//
// The extension has to end up in the browser the person actually uses. Opening
// the default one is not enough: someone whose default is Safari still installs
// this into Brave, and a link that lands in the wrong browser installs nothing.
package browsers

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Browser is one that was found on disk.
type Browser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Path is the .app bundle on macOS, the executable on Windows.
	Path string `json:"path"`
}

type candidate struct {
	id   string
	name string
	// paths are tried in order; the first that exists wins. A browser can be
	// installed per-machine or per-user, and both are normal.
	paths []string
}

func macCandidates() []candidate {
	return []candidate{
		{id: "chrome", name: "Google Chrome", paths: []string{"/Applications/Google Chrome.app"}},
		{id: "brave", name: "Brave", paths: []string{"/Applications/Brave Browser.app"}},
		{id: "edge", name: "Microsoft Edge", paths: []string{"/Applications/Microsoft Edge.app"}},
	}
}

func windowsCandidates(env func(string) string) []candidate {
	programFiles := env("ProgramFiles")
	programFilesX86 := env("ProgramFiles(x86)")
	localAppData := env("LOCALAPPDATA")
	return []candidate{
		{id: "chrome", name: "Google Chrome", paths: []string{
			filepath.Join(programFiles, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(programFilesX86, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(localAppData, `Google\Chrome\Application\chrome.exe`),
		}},
		{id: "brave", name: "Brave", paths: []string{
			filepath.Join(programFiles, `BraveSoftware\Brave-Browser\Application\brave.exe`),
			filepath.Join(programFilesX86, `BraveSoftware\Brave-Browser\Application\brave.exe`),
			filepath.Join(localAppData, `BraveSoftware\Brave-Browser\Application\brave.exe`),
		}},
		{id: "edge", name: "Microsoft Edge", paths: []string{
			filepath.Join(programFilesX86, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(programFiles, `Microsoft\Edge\Application\msedge.exe`),
		}},
	}
}

// Detect lists the browsers present on this machine.
func Detect() []Browser {
	if runtime.GOOS == "windows" {
		return detectIn(windowsCandidates(os.Getenv), exists)
	}
	return detectIn(macCandidates(), exists)
}

// detectIn takes the candidates and the existence check as parameters, so the
// tests describe a machine instead of inheriting the one they run on.
func detectIn(candidates []candidate, present func(string) bool) []Browser {
	found := make([]Browser, 0, len(candidates))
	for _, c := range candidates {
		for _, path := range c.paths {
			// An empty path means the environment variable was unset, which is
			// not the same as a browser installed at the filesystem root.
			if path == "" || strings.TrimSpace(path) == "" {
				continue
			}
			if present(path) {
				found = append(found, Browser{ID: c.id, Name: c.name, Path: path})
				break
			}
		}
	}
	return found
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ErrUnknownBrowser is returned rather than silently opening a different one.
var ErrUnknownBrowser = errors.New("that browser is not installed on this machine")

// Open launches a URL in one specific browser.
func Open(id, url string) error {
	return openWith(Detect(), id, url, launch)
}

func openWith(found []Browser, id, url string, run func(path, url string) error) error {
	for _, b := range found {
		if b.ID == id {
			return run(b.Path, url)
		}
	}
	return ErrUnknownBrowser
}

func launch(path, url string) error {
	if runtime.GOOS == "windows" {
		return exec.Command(path, url).Start()
	}
	// `open -a` targets one application bundle; plain `open` would hand the URL
	// to whichever browser is the default, which is the thing to avoid.
	return exec.Command("open", "-a", path, url).Start()
}

// Reveal opens a folder in the system's file manager, for the manual
// "Load unpacked" path while the store review is pending.
func Reveal(dir string) error {
	if runtime.GOOS == "windows" {
		return exec.Command("explorer", dir).Start()
	}
	return exec.Command("open", dir).Start()
}
