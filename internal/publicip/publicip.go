// Package publicip finds the address this machine appears to come from.
//
// It exists because an allowlist is written in terms of the address the server
// sees, which is not an address this machine can read off its own interfaces:
// behind a router, that is a private one. The only way to learn it is to ask
// something on the outside what it saw.
//
// IPv4 on purpose. A managed database's allowlist takes IPv4 ranges, and the
// host this app connects to publishes no AAAA record — so the session that
// matters leaves over IPv4. On a dual-stack machine an unpinned request answers
// with the IPv6 address instead, which is the right answer to the wrong
// question and produces a firewall rule that allows nothing.
package publicip

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Endpoints are asked in order until one answers usefully.
//
// More than one because this is the only part of the app that depends on a
// service nobody here runs, and a single one being down would present its
// outage as "we could not find your address".
var Endpoints = []string{
	"https://api.ipify.org",
	"https://checkip.amazonaws.com",
}

// ErrNotFound is "I could not find out" — the third state again, kept distinct
// from any address at all so a caller never writes a firewall rule for a guess.
var ErrNotFound = errors.New("this machine's public address could not be determined")

// Client is an HTTP client pinned to IPv4, which is the point of building one
// here rather than taking the default.
//
// A fresh one per question, so keep-alives are off: a Transport built and
// dropped with idle connections in it holds them open forever — its idle
// timeout is none unless set.
//
// No proxy, deliberately. The address wanted is the one the database sees, and
// the backend's connection to it never goes through an HTTP proxy: asked
// through one, the answer would be the proxy's address, stated as this
// machine's, for an allowlist it would not fix.
func Client(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
				return dialer.DialContext(ctx, "tcp4", address)
			},
		},
	}
}

// Find asks each endpoint in turn and returns the first well-formed IPv4
// address one of them reports.
func Find(ctx context.Context, client *http.Client, endpoints []string) (string, error) {
	var last error
	for _, endpoint := range endpoints {
		address, err := ask(ctx, client, endpoint)
		if err == nil {
			return address, nil
		}
		last = err
		// A cancelled context is this app shutting down, not an endpoint being
		// unhelpful. Trying the next one would only produce the same failure.
		if ctx.Err() != nil {
			break
		}
	}
	if last == nil {
		last = ErrNotFound
	}
	return "", fmt.Errorf("%w: %v", ErrNotFound, last)
}

func ask(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %s", endpoint, response.Status)
	}

	// Bounded because the answer is an address: anything longer is not one, and
	// an endpoint that turned into something else must not be read to the end.
	body, err := io.ReadAll(io.LimitReader(response.Body, 64))
	if err != nil {
		return "", err
	}

	address := strings.TrimSpace(string(body))
	parsed := net.ParseIP(address)
	if parsed == nil || parsed.To4() == nil {
		return "", fmt.Errorf("%s answered %q, which is not an IPv4 address", endpoint, address)
	}
	return parsed.String(), nil
}
