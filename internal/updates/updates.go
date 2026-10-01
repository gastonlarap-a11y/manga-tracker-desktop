// Package updates finds out whether a newer release of this app exists.
//
// It asks GitHub's releases API one question — the newest published release
// of this repository — and compares its tag with the one this build carries.
// Asked once per launch, and only while the user leaves it on: it is the one
// request the app makes on its own to a service nobody here runs, and GitHub
// sees this machine's address when it is made. Nothing else is sent.
//
// Updating itself stays a person's act: the window says a version exists and
// opens its page. The installer already moves an installed backend to the new
// payload when the new app is run (see installer.Prepare).
package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

// Repository is where releases are published.
const Repository = "gastonlarap-a11y/manga-tracker-desktop"

// Endpoint answers with the newest release that is neither a draft nor a
// prerelease.
const Endpoint = "https://api.github.com/repos/" + Repository + "/releases/latest"

// State is what the check found out.
type State string

const (
	// Current: no published release is newer than this build.
	Current State = "current"
	// Available: a newer release is published; Result.Latest names it.
	Available State = "available"
	// Unknown: the question could not be answered — no network, GitHub
	// refusing, an answer that is not a release. Never read as "up to date".
	Unknown State = "unknown"
	// Development: a build without a release tag, which has no version to
	// compare. No request is made.
	Development State = "development"
)

// Result is a State and, when one was found, the newest published tag.
type Result struct {
	State  State  `json:"state"`
	Latest string `json:"latest"`
	// Problem is the technical reason behind Unknown, for the screen to show
	// under a sentence of its own — never as the message itself.
	Problem string `json:"problem"`
}

// Version is a release tag, vMAJOR.MINOR.PATCH.
type Version struct{ Major, Minor, Patch int }

var tagPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// Parse reads a release tag. Anything else — "dev", "dev-1a2b3c", a tag with a
// suffix — is not a version this compares.
func Parse(tag string) (Version, bool) {
	match := tagPattern.FindStringSubmatch(tag)
	if match == nil {
		return Version{}, false
	}
	parts := [3]int{}
	for index, digits := range match[1:] {
		value, err := strconv.Atoi(digits)
		if err != nil {
			return Version{}, false
		}
		parts[index] = value
	}
	return Version{Major: parts[0], Minor: parts[1], Patch: parts[2]}, true
}

// Newer reports whether v comes after other.
func (v Version) Newer(other Version) bool {
	if v.Major != other.Major {
		return v.Major > other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor > other.Minor
	}
	return v.Patch > other.Patch
}

// ReleasePage is the page of a release, built here from a tag that parsed —
// never a URL taken from the network's answer, so whatever the API returned,
// what the window opens is a page of this repository.
func ReleasePage(tag string) (string, bool) {
	if _, ok := Parse(tag); !ok {
		return "", false
	}
	return "https://github.com/" + Repository + "/releases/tag/" + tag, true
}

// Client is an HTTP client for the one question. Keep-alives off, as in
// internal/publicip: a Transport dropped with idle connections in it keeps
// them open, and this is asked once.
func Client(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{DisableKeepAlives: true},
	}
}

// Check compares the build's tag (`current`, payload.Version()) with the
// newest published release.
func Check(ctx context.Context, client *http.Client, endpoint, current string) Result {
	running, ok := Parse(current)
	if !ok {
		return Result{State: Development}
	}
	tag, err := latestTag(ctx, client, endpoint)
	if err != nil {
		return Result{State: Unknown, Problem: err.Error()}
	}
	latest, ok := Parse(tag)
	if !ok {
		return Result{State: Unknown, Problem: fmt.Sprintf("the newest release is tagged %q, which is not a version", tag)}
	}
	if latest.Newer(running) {
		return Result{State: Available, Latest: tag}
	}
	return Result{State: Current, Latest: tag}
}

// maxAnswer bounds what is read of the answer: a release is a few kilobytes,
// and an endpoint that turned into something else must not be read to the end.
const maxAnswer = 1 << 20

var errNotRelease = errors.New("the answer is not a published release")

func latestTag(ctx context.Context, client *http.Client, endpoint string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("building the release request: %w", err)
	}
	// GitHub refuses requests without a User-Agent, and pins the format with
	// the version header.
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "Manga-Tracker-Desktop")

	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("asking GitHub for the newest release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		// 403/429 is the anonymous rate limit (60 an hour per address), 404 a
		// repository with no published release yet.
		return "", fmt.Errorf("GitHub answered %s", response.Status)
	}

	var release struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxAnswer)).Decode(&release); err != nil {
		return "", fmt.Errorf("%w: %v", errNotRelease, err)
	}
	// The endpoint already leaves both out; this keeps it so if it ever does not.
	if release.TagName == "" || release.Draft || release.Prerelease {
		return "", errNotRelease
	}
	return release.TagName, nil
}
