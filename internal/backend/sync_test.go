package backend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func syncServer(t *testing.T, bodies ...string) string {
	t.Helper()
	call := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sync/status" {
			http.NotFound(w, r)
			return
		}
		body := bodies[len(bodies)-1]
		if call < len(bodies) {
			body = bodies[call]
		}
		call++
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("could not write the response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestFetchSyncStatusReadsTheShape(t *testing.T) {
	url := syncServer(t, `{"enabled":true,"connected":true,"lastSyncAt":null,"lastResult":null,"lastError":null}`)

	status, err := FetchSyncStatus(context.Background(), http.DefaultClient, url)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Enabled || !status.Connected || status.LastError != "" {
		t.Errorf("unexpected status: %+v", status)
	}
}

func TestFetchSyncStatusCarriesTheReason(t *testing.T) {
	// The message is the only thing that tells someone who just typed a
	// connection string what was wrong with it.
	url := syncServer(t, `{"enabled":true,"connected":false,"lastError":{"message":"authentication failed","at":"2026-08-09T00:00:00Z"}}`)

	status, err := FetchSyncStatus(context.Background(), http.DefaultClient, url)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.LastError != "authentication failed" {
		t.Errorf("expected the reason, got %q", status.LastError)
	}
}

func TestWaitForSyncKeepsAskingUntilItSettles(t *testing.T) {
	// Restarting the service is asynchronous: the first answer comes from the
	// process on its way out, and believing it would report "not connected"
	// for credentials that are perfectly fine.
	url := syncServer(t,
		`{"enabled":false,"connected":false,"lastError":null}`,
		`{"enabled":true,"connected":false,"lastError":null}`,
		`{"enabled":true,"connected":true,"lastError":null}`,
	)

	status, err := WaitForSync(context.Background(), http.DefaultClient, url, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Connected {
		t.Errorf("expected it to settle on connected, got %+v", status)
	}
}

func TestWaitForSyncStopsOnAReportedFailure(t *testing.T) {
	// A failure is a settled answer too — waiting out the timeout would only
	// make a wrong password take thirty seconds to say so.
	url := syncServer(t, `{"enabled":true,"connected":false,"lastError":{"message":"bad auth","at":"2026-08-09T00:00:00Z"}}`)

	start := time.Now()
	status, err := WaitForSync(context.Background(), http.DefaultClient, url, 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.LastError != "bad auth" {
		t.Errorf("expected the failure, got %+v", status)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("a reported failure should not wait out the timeout")
	}
}

func TestWaitForSyncGivesUpQuietly(t *testing.T) {
	// Neither connected nor failed within the window: return what was last
	// seen rather than an error, because "still trying" is a real state.
	url := syncServer(t, `{"enabled":true,"connected":false,"lastError":null}`)

	status, err := WaitForSync(context.Background(), http.DefaultClient, url, 700*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !status.Enabled || status.Connected {
		t.Errorf("unexpected status: %+v", status)
	}
}
