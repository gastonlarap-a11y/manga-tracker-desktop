// Package backend locates the manga-tracker-api service running on this
// machine.
//
// Nothing here starts or supervises it: the backend is a system service
// (launchd / Task Scheduler) with its own lifecycle, and this app is a window
// onto it. See AGENTS.md for why that separation is not negotiable.
package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	// DefaultPort is where a checkout, and any install that could take it,
	// listens. Probed first and on its own, so the ordinary case costs one
	// request instead of a sweep.
	DefaultPort = 5150
	// LastPort closes the window an installer may pick from. Ten candidates.
	// The browser extension probes the same range: widening it here without
	// widening it there produces a backend nothing can reach.
	LastPort = 5159

	// ServiceName is what GET /health answers. Several things on a personal
	// machine return 200 on a loopback port; without a name in the body a probe
	// cannot tell them apart.
	ServiceName = "manga-tracker-api"
)

// ProbeTimeout bounds a port held by something that accepts a connection and
// never answers. A refused connection fails immediately and does not wait.
const ProbeTimeout = 800 * time.Millisecond

// 127.0.0.1 rather than localhost: the backend binds the IPv4 loopback
// explicitly, and on a machine that resolves localhost to ::1 first the name
// would cost a failed connection before the right one.
func baseURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

// Ports lists the candidates in the order they are tried.
func Ports() []int {
	ports := make([]int, 0, LastPort-DefaultPort+1)
	for port := DefaultPort; port <= LastPort; port++ {
		ports = append(ports, port)
	}
	return ports
}

// probe asks one port whether our backend is behind it.
//
// requireServiceName is what keeps the widened search honest. On the first
// candidate — the default port — a bare {"status":"ok"} is accepted, because a
// backend older than the release that added the field is still ours and still
// listening exactly there. On every other port the name is mandatory: those are
// ports we only reach because we went looking.
func probe(ctx context.Context, client *http.Client, port int, requireServiceName bool) bool {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL(port)+"/health", nil)
	if err != nil {
		return false
	}
	response, err := client.Do(request)
	if err != nil {
		// Refused, timed out or unreachable: expected for most candidates, and
		// not an error worth surfacing.
		return false
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return false
	}
	var health healthResponse
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		return false
	}
	if health.Status != "ok" {
		return false
	}
	if health.Service == "" {
		return !requireServiceName
	}
	return health.Service == ServiceName
}

// Discover returns the base URL of the backend, or an empty string when none of
// the candidate ports holds one.
func Discover(ctx context.Context, client *http.Client) string {
	return discoverIn(ctx, client, Ports())
}

// discoverIn takes the candidates as a parameter so a test can pin them to the
// ports it actually bound, instead of inheriting the machine it runs on.
//
// Candidates are tried in order and the first match wins, so the answer never
// depends on which one happened to reply first. The first entry is the default
// port and is the only one probed leniently.
func discoverIn(ctx context.Context, client *http.Client, ports []int) string {
	for index, port := range ports {
		if probe(ctx, client, port, index > 0) {
			return baseURL(port)
		}
	}
	return ""
}
