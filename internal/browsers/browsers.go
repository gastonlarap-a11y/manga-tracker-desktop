// Package browsers finds the Chromium browsers installed on this machine and
// opens a page in one of them.
//
// The extension has to end up in the browser the person actually uses. Opening
// the default one is not enough: someone whose default is Safari still installs
// this into Brave, and a link that lands in the wrong browser installs nothing.
package browsers

import (
	"errors"
	neturl "net/url"
	"os"
	"os/exec"
	"path"
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

// macCandidates looks in /Applications and then in the user's own
// ~/Applications, which is where a browser installed without an administrator
// password lands. An empty home skips the second: joined onto nothing it would
// be a path relative to wherever the app happened to start.
//
// path, not filepath: these are macOS paths wherever they are built, and the
// tests that describe a Mac also run on the Windows CI runner.
func macCandidates(home string) []candidate {
	dirs := []string{"/Applications"}
	if home != "" {
		dirs = append(dirs, path.Join(home, "Applications"))
	}
	bundle := func(name string) []string {
		paths := make([]string, 0, len(dirs))
		for _, dir := range dirs {
			paths = append(paths, path.Join(dir, name))
		}
		return paths
	}
	return []candidate{
		{id: "chrome", name: "Google Chrome", paths: bundle("Google Chrome.app")},
		{id: "brave", name: "Brave", paths: bundle("Brave Browser.app")},
		{id: "edge", name: "Microsoft Edge", paths: bundle("Microsoft Edge.app")},
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
	// Without a home directory there is still /Applications to look in; not
	// finding one only narrows the search, it does not stop it.
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return detectIn(macCandidates(home), exists)
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

// Installed reports whether an id names a browser found on this machine.
func Installed(id string) bool {
	for _, b := range Detect() {
		if b.ID == id {
			return true
		}
	}
	return false
}

// IsWebURL reports whether a URL is safe to hand to the operating system.
//
// The gate on everything arriving from the embedded dashboard: that page is
// served by the local backend, but any page it renders can carry any href, and
// `javascript:`, `file:` or `data:` must never reach a shell command. Only
// http and https describe a page a browser should be asked to fetch.
func IsWebURL(raw string) bool {
	parsed, err := neturl.Parse(raw)
	if err != nil {
		return false
	}
	// Parse accepts a bare path ("/etc/passwd") with an empty scheme, and a
	// scheme alone with no host ("https://") — neither addresses a page.
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
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
		return startDetached(exec.Command(path, url))
	}
	// `open -a` targets one application bundle; plain `open` would hand the URL
	// to whichever browser is the default, which is the thing to avoid. Run, not
	// Start: `open` returns as soon as the browser has the URL, and waiting for
	// it is what surfaces its error ("Unable to find application") and reaps it,
	// instead of leaving a zombie behind every click.
	return exec.Command("open", "-a", path, url).Run()
}

// Reveal opens a folder in the system's file manager, for loading the
// extension unpacked — a development build, or a copy loaded by hand.
func Reveal(dir string) error {
	if runtime.GOOS == "windows" {
		// Not Run: explorer exits with status 1 even when it opened the
		// folder, so its exit status says nothing.
		return startDetached(exec.Command("explorer", dir))
	}
	return exec.Command("open", dir).Run()
}

// startDetached starts a program that goes on running on its own — a browser,
// the file manager — and reaps it whenever it exits. Waiting inline would hold
// the window until someone closed their browser.
func startDetached(command *exec.Cmd) error {
	if err := command.Start(); err != nil {
		return err
	}
	go func() {
		// The exit status of a program handed off to the person is not
		// something this app acts on; Wait is here to release the process.
		_ = command.Wait()
	}()
	return nil
}
