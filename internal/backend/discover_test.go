package backend

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// serveHealth starts a server on a free loopback port and returns its port.
// Real sockets rather than a stubbed transport: what is being tested is that a
// port either holds our backend or does not, and a stub would prove nothing
// about a refused connection.
func serveHealth(t *testing.T, body string, status int) int {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	return portOf(t, server.URL)
}

func portOf(t *testing.T, rawURL string) int {
	t.Helper()
	_, portText, err := net.SplitHostPort(rawURL[len("http://"):])
	if err != nil {
		t.Fatalf("could not read the port out of %q: %v", rawURL, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("port %q is not a number: %v", portText, err)
	}
	return port
}

// freePort returns a port with nothing listening on it: bind, read the port,
// release it. Used to prove that a refused connection is handled, not that a
// specific number is free.
func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not bind a port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("could not release the port: %v", err)
	}
	return port
}

func TestPortsIsTheWindowTheInstallerPicksFrom(t *testing.T) {
	ports := Ports()

	if len(ports) != 10 {
		t.Fatalf("expected 10 candidates, got %d", len(ports))
	}
	if ports[0] != DefaultPort {
		t.Errorf("the default port must be tried first, got %d", ports[0])
	}
	if ports[len(ports)-1] != LastPort {
		t.Errorf("expected the window to end at %d, got %d", LastPort, ports[len(ports)-1])
	}
}

func TestDiscoverFindsTheBackend(t *testing.T) {
	port := serveHealth(t, `{"status":"ok","service":"manga-tracker-api"}`, http.StatusOK)

	found := discoverIn(context.Background(), http.DefaultClient, []int{freePort(t), port})

	want := fmt.Sprintf("http://127.0.0.1:%d", port)
	if found != want {
		t.Errorf("expected %q, got %q", want, found)
	}
}

func TestDiscoverAcceptsAnOlderBackendOnlyOnTheDefaultPort(t *testing.T) {
	// Updating the app before the backend must not break an install that is
	// already working — but only where that backend has always been.
	port := serveHealth(t, `{"status":"ok"}`, http.StatusOK)

	asDefault := discoverIn(context.Background(), http.DefaultClient, []int{port})
	if asDefault == "" {
		t.Error("a backend without the service field must still be found on the default port")
	}

	asExtra := discoverIn(context.Background(), http.DefaultClient, []int{freePort(t), port})
	if asExtra != "" {
		t.Error("a nameless 200 on a non-default port must not be taken for the backend")
	}
}

func TestDiscoverIgnoresSomethingElseListening(t *testing.T) {
	// Posting reading data into an unrelated local server would be silent data
	// loss, so anything that is not us is skipped.
	cases := map[string]string{
		"another app that answers ok":      `{"status":"ok","service":"some-other-app"}`,
		"a health endpoint that is not ok": `{"status":"degraded","service":"manga-tracker-api"}`,
		"a page that is not JSON at all":   `<html>nope</html>`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			port := serveHealth(t, body, http.StatusOK)

			if found := discoverIn(context.Background(), http.DefaultClient, []int{freePort(t), port}); found != "" {
				t.Errorf("expected no backend, got %q", found)
			}
		})
	}
}

func TestDiscoverIgnoresAnErrorStatus(t *testing.T) {
	port := serveHealth(t, `{"status":"ok","service":"manga-tracker-api"}`, http.StatusServiceUnavailable)

	if found := discoverIn(context.Background(), http.DefaultClient, []int{port}); found != "" {
		t.Errorf("expected no backend, got %q", found)
	}
}

func TestDiscoverReturnsNothingWhenNobodyIsListening(t *testing.T) {
	if found := discoverIn(context.Background(), http.DefaultClient, []int{freePort(t), freePort(t)}); found != "" {
		t.Errorf("expected no backend, got %q", found)
	}
}

func TestDiscoverPrefersTheEarlierCandidate(t *testing.T) {
	// Reproducibility: two backends running must not mean a different answer
	// per run.
	first := serveHealth(t, `{"status":"ok","service":"manga-tracker-api"}`, http.StatusOK)
	second := serveHealth(t, `{"status":"ok","service":"manga-tracker-api"}`, http.StatusOK)

	found := discoverIn(context.Background(), http.DefaultClient, []int{freePort(t), first, second})

	if want := fmt.Sprintf("http://127.0.0.1:%d", first); found != want {
		t.Errorf("expected the earlier candidate %q, got %q", want, found)
	}
}

func TestDiscoverStopsWhenTheContextIsCancelled(t *testing.T) {
	port := serveHealth(t, `{"status":"ok","service":"manga-tracker-api"}`, http.StatusOK)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if found := discoverIn(ctx, http.DefaultClient, []int{port}); found != "" {
		t.Errorf("a cancelled lookup must not report a backend, got %q", found)
	}
}
