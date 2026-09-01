package publicip

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// answering serves one fixed reply, and records how many times it was asked —
// the fallback only earns its place if the first endpoint is really skipped.
func answering(t *testing.T, status int, body string, asked *int) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*asked++
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestFind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr bool
	}{
		{name: "a plain address", status: http.StatusOK, body: "198.51.100.4", want: "198.51.100.4"},
		{name: "trailing newline, as checkip sends it", status: http.StatusOK, body: "198.51.100.4\n", want: "198.51.100.4"},
		{name: "an IPv6 answer is the wrong question answered", status: http.StatusOK, body: "2001:db8::1", wantErr: true},
		{name: "not an address at all", status: http.StatusOK, body: "<html>rate limited</html>", wantErr: true},
		{name: "an empty body", status: http.StatusOK, body: "", wantErr: true},
		{name: "an endpoint that refuses", status: http.StatusTooManyRequests, body: "198.51.100.4", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			asked := 0
			endpoint := answering(t, test.status, test.body, &asked)

			got, err := Find(context.Background(), http.DefaultClient, []string{endpoint})
			if test.wantErr {
				if err == nil {
					t.Fatalf("Find() = %q, want an error", got)
				}
				if !errors.Is(err, ErrNotFound) {
					t.Errorf("Find() error = %v, want it to wrap ErrNotFound", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Find() error = %v", err)
			}
			if got != test.want {
				t.Errorf("Find() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFindFallsBackToTheNextEndpoint(t *testing.T) {
	t.Parallel()

	brokenAsked, goodAsked := 0, 0
	broken := answering(t, http.StatusInternalServerError, "down", &brokenAsked)
	good := answering(t, http.StatusOK, "198.51.100.4", &goodAsked)

	got, err := Find(context.Background(), http.DefaultClient, []string{broken, good})
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if got != "198.51.100.4" {
		t.Errorf("Find() = %q, want the second endpoint's answer", got)
	}
	if brokenAsked != 1 || goodAsked != 1 {
		t.Errorf("asked broken %d times and good %d, want 1 each", brokenAsked, goodAsked)
	}
}

// The first usable answer ends it: asking a second service for an address we
// already have is a request nobody needs to make.
func TestFindStopsAtTheFirstUsableAnswer(t *testing.T) {
	t.Parallel()

	firstAsked, secondAsked := 0, 0
	first := answering(t, http.StatusOK, "198.51.100.4", &firstAsked)
	second := answering(t, http.StatusOK, "203.0.113.7", &secondAsked)

	got, err := Find(context.Background(), http.DefaultClient, []string{first, second})
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if got != "198.51.100.4" {
		t.Errorf("Find() = %q, want the first endpoint's answer", got)
	}
	if secondAsked != 0 {
		t.Errorf("the second endpoint was asked %d times, want 0", secondAsked)
	}
}

func TestFindReportsNotFoundWithNoEndpoints(t *testing.T) {
	t.Parallel()

	if _, err := Find(context.Background(), http.DefaultClient, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("Find() error = %v, want it to wrap ErrNotFound", err)
	}
}
