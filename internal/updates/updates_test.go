package updates

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// releasing serves one fixed answer and records what it was asked, so a test
// can tell a request that was made from one that was skipped.
func releasing(t *testing.T, status int, body string, asked *[]*http.Request) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*asked = append(*asked, r)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current string
		status  int
		body    string
		want    Result
	}{
		{
			name: "a newer release", current: "v0.1.19", status: http.StatusOK,
			body: `{"tag_name":"v0.1.20","draft":false,"prerelease":false}`,
			want: Result{State: Available, Latest: "v0.1.20"},
		},
		{
			name: "newer by minor, though its patch is smaller", current: "v0.1.19", status: http.StatusOK,
			body: `{"tag_name":"v0.2.0"}`,
			want: Result{State: Available, Latest: "v0.2.0"},
		},
		{
			// Compared as numbers: "v0.1.9" sorts after "v0.1.10" as text.
			name: "an older release is not an update", current: "v0.1.10", status: http.StatusOK,
			body: `{"tag_name":"v0.1.9"}`,
			want: Result{State: Current, Latest: "v0.1.9"},
		},
		{
			name: "the same release", current: "v0.1.19", status: http.StatusOK,
			body: `{"tag_name":"v0.1.19"}`,
			want: Result{State: Current, Latest: "v0.1.19"},
		},
		{
			name: "a prerelease is never offered", current: "v0.1.19", status: http.StatusOK,
			body: `{"tag_name":"v0.2.0","prerelease":true}`,
			want: Result{State: Unknown},
		},
		{
			name: "a tag that is not a version", current: "v0.1.19", status: http.StatusOK,
			body: `{"tag_name":"nightly"}`,
			want: Result{State: Unknown},
		},
		{
			name: "not JSON", current: "v0.1.19", status: http.StatusOK,
			body: `<html>`,
			want: Result{State: Unknown},
		},
		{
			name: "the anonymous rate limit", current: "v0.1.19", status: http.StatusForbidden,
			body: `{"message":"API rate limit exceeded"}`,
			want: Result{State: Unknown},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var asked []*http.Request
			endpoint := releasing(t, test.status, test.body, &asked)

			got := Check(context.Background(), http.DefaultClient, endpoint, test.current)

			if got.State != test.want.State || got.Latest != test.want.Latest {
				t.Fatalf("Check() = %+v, want %+v", got, test.want)
			}
			// "I could not find out" carries its reason, and nothing else does.
			if (got.State == Unknown) != (got.Problem != "") {
				t.Errorf("Check() problem = %q for state %s", got.Problem, got.State)
			}
		})
	}
}

func TestCheckAsksAsGitHubRequires(t *testing.T) {
	t.Parallel()
	var asked []*http.Request
	endpoint := releasing(t, http.StatusOK, `{"tag_name":"v0.1.19"}`, &asked)

	Check(context.Background(), http.DefaultClient, endpoint, "v0.1.19")

	if len(asked) != 1 {
		t.Fatalf("asked %d times, want once", len(asked))
	}
	if asked[0].Header.Get("User-Agent") == "" {
		t.Error("no User-Agent: GitHub refuses requests without one")
	}
	if !strings.Contains(asked[0].Header.Get("Accept"), "github") {
		t.Errorf("Accept = %q, want GitHub's media type", asked[0].Header.Get("Accept"))
	}
}

func TestCheckAsksNothingForADevelopmentBuild(t *testing.T) {
	t.Parallel()
	for _, current := range []string{"dev", "dev-1a2b3c4", ""} {
		var asked []*http.Request
		endpoint := releasing(t, http.StatusOK, `{"tag_name":"v9.9.9"}`, &asked)

		got := Check(context.Background(), http.DefaultClient, endpoint, current)

		if got.State != Development {
			t.Errorf("Check(%q) = %s, want development", current, got.State)
		}
		if len(asked) != 0 {
			t.Errorf("Check(%q) asked GitHub; a build without a version has nothing to compare", current)
		}
	}
}

func TestCheckWithoutNetwork(t *testing.T) {
	t.Parallel()
	// A server already closed: the connection is refused.
	server := httptest.NewServer(http.NotFoundHandler())
	endpoint := server.URL
	server.Close()

	got := Check(context.Background(), http.DefaultClient, endpoint, "v0.1.19")

	if got.State != Unknown || got.Problem == "" {
		t.Errorf("Check() = %+v, want unknown with a reason", got)
	}
}

func TestReleasePage(t *testing.T) {
	t.Parallel()
	page, ok := ReleasePage("v0.1.20")
	if !ok || page != "https://github.com/gastonlarap-a11y/manga-tracker-desktop/releases/tag/v0.1.20" {
		t.Errorf("ReleasePage() = %q, %v", page, ok)
	}
	for _, tag := range []string{"", "nightly", "v0.1.20/../../evil", "v1.2"} {
		if page, ok := ReleasePage(tag); ok {
			t.Errorf("ReleasePage(%q) = %q, want refused", tag, page)
		}
	}
}
