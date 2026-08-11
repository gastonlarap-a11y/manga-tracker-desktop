package browsers

import (
	"errors"
	"path/filepath"
	"testing"
)

// installed builds an existence check describing a machine, so the tests never
// depend on what happens to be installed where they run.
func installed(paths ...string) func(string) bool {
	set := make(map[string]bool, len(paths))
	for _, p := range paths {
		set[p] = true
	}
	return func(path string) bool { return set[path] }
}

func TestDetectListsOnlyWhatIsThere(t *testing.T) {
	found := detectIn(macCandidates(), installed("/Applications/Brave Browser.app"))

	if len(found) != 1 || found[0].ID != "brave" {
		t.Fatalf("expected only Brave, got %+v", found)
	}
	if found[0].Path != "/Applications/Brave Browser.app" {
		t.Errorf("unexpected path: %s", found[0].Path)
	}
}

func TestDetectFindsNothingOnABareMachine(t *testing.T) {
	// Not an error: someone may only have Safari or Firefox, and the window
	// then offers the manual path instead of a button that opens nothing.
	if found := detectIn(macCandidates(), installed()); len(found) != 0 {
		t.Errorf("expected nothing, got %+v", found)
	}
}

func TestDetectTakesTheFirstLocationThatExists(t *testing.T) {
	// A browser can be installed per-machine or per-user, and both are normal.
	env := func(name string) string {
		return map[string]string{
			"ProgramFiles":      `C:\Program Files`,
			"ProgramFiles(x86)": `C:\Program Files (x86)`,
			"LOCALAPPDATA":      `C:\Users\me\AppData\Local`,
		}[name]
	}
	// Built with filepath.Join, like the code does: the separator follows the
	// host, so a literal Windows path would stop matching when the suite runs
	// on a Mac and the test would pass while proving nothing.
	perUser := filepath.Join(
		`C:\Users\me\AppData\Local`,
		`Google\Chrome\Application\chrome.exe`,
	)

	found := detectIn(windowsCandidates(env), installed(perUser))

	if len(found) != 1 || found[0].Path != perUser {
		t.Fatalf("expected the per-user install, got %+v", found)
	}
}

func TestDetectIgnoresUnsetEnvironmentVariables(t *testing.T) {
	// An unset variable makes filepath.Join produce a bare relative path. That
	// must not be mistaken for a browser sitting at the filesystem root.
	env := func(string) string { return "" }
	always := func(string) bool { return true }

	for _, b := range detectIn(windowsCandidates(env), always) {
		if b.Path == "" {
			t.Errorf("%s was detected at an empty path", b.ID)
		}
	}
}

func TestOpenTargetsTheBrowserThatWasAskedFor(t *testing.T) {
	// Opening the default browser instead would install the extension into the
	// wrong one, which is the whole reason this package exists.
	var opened string
	found := []Browser{
		{ID: "chrome", Path: "/Applications/Google Chrome.app"},
		{ID: "brave", Path: "/Applications/Brave Browser.app"},
	}

	err := openWith(found, "brave", "https://example.com", func(path, _ string) error {
		opened = path
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opened != "/Applications/Brave Browser.app" {
		t.Errorf("opened the wrong browser: %s", opened)
	}
}

func TestOpenRefusesABrowserThatIsNotInstalled(t *testing.T) {
	err := openWith(nil, "edge", "https://example.com", func(string, string) error {
		t.Fatal("nothing should have been launched")
		return nil
	})

	if !errors.Is(err, ErrUnknownBrowser) {
		t.Errorf("expected ErrUnknownBrowser, got %v", err)
	}
}

func TestIsWebURL(t *testing.T) {
	// The gate on everything the embedded dashboard forwards: that page is
	// served locally, but any page it renders can carry any href.
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "a chapter link", url: "https://lectorxd.com/manhua/dragona/leer/56", want: true},
		{name: "plain http", url: "http://127.0.0.1:5150/manga/dragona", want: true},
		{name: "script", url: "javascript:alert(1)", want: false},
		{name: "local file", url: "file:///etc/passwd", want: false},
		{name: "inline data", url: "data:text/html,<h1>hi</h1>", want: false},
		{name: "mail", url: "mailto:alguien@example.com", want: false},
		{name: "a bare path", url: "/etc/passwd", want: false},
		{name: "a scheme with no host", url: "https://", want: false},
		{name: "empty", url: "", want: false},
		{name: "unparseable", url: "http://a b c", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsWebURL(test.url); got != test.want {
				t.Errorf("IsWebURL(%q) = %v, want %v", test.url, got, test.want)
			}
		})
	}
}
