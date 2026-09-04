// Package selfupdate resolves and applies c1i upgrades from the C1
// distribution center (dist.conductorone.com). It consumes the public release
// interface documented in ConductorOne/baton-admin's dist-release RFC: a
// per-CLI index.json (release channels + version list) and a per-release
// manifest.json (per-GOOS-GOARCH assets with sha256). The canonical schema is
// the protobuf in ConductorOne/github-workflows/pb/artifacts/v1; this package
// hand-rolls the small stable subset it needs rather than importing that
// workflow-tooling module.
package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"

	"github.com/ConductorOne/c1i/internal/transport"
)

// DefaultBaseURL is the dist release path for c1i. Assets, index.json and
// per-version manifest.json all live under it.
const DefaultBaseURL = "https://dist.conductorone.com/releases/ConductorOne/c1i"

// Doer sends a request through the shared transport, so upgrade inherits the
// same retries, --max-retries, --debug tracing and user-agent as every other
// network path. *transport.Client satisfies it; tests fake it.
type Doer interface {
	Do(*http.Request) (*transport.Response, error)
}

// Index is the subset of dist's index.json the updater reads.
type Index struct {
	// Channels maps a channel name ("stable", "latest", "preview") to a
	// version tag; a released version is "latest" immediately but stays out of
	// "stable" until it is promoted.
	Channels map[string]string      `json:"channels"`
	Semvers  map[string]SemverEntry `json:"semvers"`
}

// SemverEntry is one version's entry in index.json.
type SemverEntry struct {
	Yanked   bool   `json:"yanked"`
	Hidden   bool   `json:"hidden"`
	Manifest string `json:"manifest"` // absolute URL of this version's manifest.json
}

// Manifest is the subset of a dist <version>/manifest.json the updater reads.
type Manifest struct {
	Semver string           `json:"semver"`
	Assets map[string]Asset `json:"assets"` // keyed by "<goos>-<goarch>", plus "checksums"
}

// Asset is one downloadable artifact in a manifest.
type Asset struct {
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"`
	Href     string `json:"href"` // absolute download URL
}

// Client fetches the dist index and manifests.
type Client struct {
	HTTP    Doer
	BaseURL string
}

// PlatformKey is the manifest asset key for the running binary, e.g.
// "linux-amd64" or "darwin-arm64".
func PlatformKey() string { return runtime.GOOS + "-" + runtime.GOARCH }

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return DefaultBaseURL
}

// Index fetches and decodes index.json.
func (c *Client) Index(ctx context.Context) (*Index, error) {
	var idx Index
	if err := c.getJSON(ctx, c.baseURL()+"/index.json", &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

// Manifest fetches and decodes a version's manifest.json from the URL the
// index entry names.
func (c *Client) Manifest(ctx context.Context, url string) (*Manifest, error) {
	var m Manifest
	if err := c.getJSON(ctx, url, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (c *Client) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("fetching %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetching %s: HTTP %d", url, resp.StatusCode)
	}
	// dist serves the SPA HTML shell (text/html) with 200 for a path that has
	// no object; a real API response is application/json. Guard against
	// decoding the shell as an empty struct.
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "json") {
		return fmt.Errorf("fetching %s: expected JSON, got Content-Type %q (no such release object?)", url, ct)
	}
	if err := json.Unmarshal(resp.Body, out); err != nil {
		return fmt.Errorf("parsing %s: %w", url, err)
	}
	return nil
}

// CompareVersions orders two "vMAJOR.MINOR.PATCH[-prerelease]" tags: it returns
// -1 if a < b, 0 if equal, 1 if a > b. A release outranks its own prerelease
// (v1.0.0 > v1.0.0-rc.1). ok is false if either side is not a parseable
// version (e.g. "dev"), and the caller must handle that rather than trust 0.
func CompareVersions(a, b string) (cmp int, ok bool) {
	am, ap, aok := parseVersion(a)
	bm, bp, bok := parseVersion(b)
	if !aok || !bok {
		return 0, false
	}
	for i := 0; i < 3; i++ {
		if am[i] != bm[i] {
			if am[i] < bm[i] {
				return -1, true
			}
			return 1, true
		}
	}
	// Equal core: absent prerelease outranks a present one; otherwise compare
	// prerelease text (enough for this project's -rc.N tags).
	switch {
	case ap == "" && bp == "":
		return 0, true
	case ap == "":
		return 1, true
	case bp == "":
		return -1, true
	case ap < bp:
		return -1, true
	case ap > bp:
		return 1, true
	default:
		return 0, true
	}
}

func parseVersion(v string) (core [3]int, pre string, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return core, "", false
	}
	if i := strings.IndexByte(v, '-'); i != -1 {
		pre = v[i+1:]
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return core, "", false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return core, "", false
		}
		core[i] = n
	}
	return core, pre, true
}
