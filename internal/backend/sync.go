package backend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// SyncStatus mirrors GET /api/sync/status.
//
// Only the fields the window acts on. `enabled` answers "are credentials
// configured"; `connected` answers "did it actually reach the store", which is
// the question someone who just typed a connection string is really asking.
type SyncStatus struct {
	Enabled   bool   `json:"enabled"`
	Connected bool   `json:"connected"`
	LastError string `json:"lastError"`
	// LastSyncAt is an RFC 3339 timestamp, or empty if it has never run. Passed
	// through as written rather than parsed: turning it into "hace 3 minutos"
	// is wording, and wording belongs to the window.
	LastSyncAt string `json:"lastSyncAt"`
}

type syncStatusBody struct {
	Enabled   bool `json:"enabled"`
	Connected bool `json:"connected"`
	LastError *struct {
		Message string `json:"message"`
	} `json:"lastError"`
	LastSyncAt *string `json:"lastSyncAt"`
}

// FetchSyncStatus asks the backend where its sync stands.
func FetchSyncStatus(ctx context.Context, client *http.Client, baseURL string) (SyncStatus, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/sync/status", nil)
	if err != nil {
		return SyncStatus{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return SyncStatus{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return SyncStatus{}, errors.New("the backend did not report its sync state")
	}

	var body syncStatusBody
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return SyncStatus{}, err
	}
	status := SyncStatus{Enabled: body.Enabled, Connected: body.Connected}
	if body.LastError != nil {
		status.LastError = body.LastError.Message
	}
	if body.LastSyncAt != nil {
		status.LastSyncAt = *body.LastSyncAt
	}
	return status, nil
}

// WaitForSync polls until the backend reports a settled answer: either it
// connected, or it tried and failed and can say why.
//
// Restarting the service is asynchronous, so asking once right after saving
// credentials reads the state of the process that is on its way out.
func WaitForSync(ctx context.Context, client *http.Client, baseURL string, timeout time.Duration) (SyncStatus, error) {
	deadline := time.Now().Add(timeout)
	var last SyncStatus
	for {
		status, err := FetchSyncStatus(ctx, client, baseURL)
		if err == nil {
			last = status
			if status.Connected || status.LastError != "" {
				return status, nil
			}
		}
		if time.Now().After(deadline) {
			return last, nil
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
