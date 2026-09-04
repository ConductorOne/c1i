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
	"net/url"
	"runtime"
	"strconv"
	"strings"

	"github.com/ConductorOne/c1i/internal/transport"
)

// DefaultBaseURL is the dist release path for c1i. Assets, index.json and
// per-version manifest.json all live under it.
const DefaultBaseURL = "https://dist.conductorone.com/releases/ConductorOne/c1i"

// MaxMetadataBytes bounds a JSON metadata fetch (index.json, manifest.json) and
// the small detached .sig/.cert, so a hostile endpoint can't stream an
// unbounded body into memory before it is parsed. MaxArtifactBytes bounds the
// release archive download. Callers wire these into the transport that backs
// the Doer(s) below.
const (
	MaxMetadataBytes = 8 << 20   // 8 MiB
	MaxArtifactBytes = 200 << 20 // 200 MiB
)

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
	// Signature and Certificate are absolute URLs of the manifest's detached
	// Sigstore signature (.sig) and signing certificate (.cert), each
	// base64-encoded on the wire. They authenticate manifest.json.
	Signature   string `json:"signature"`
	Certificate string `json:"certificate"`
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
	// HTTP fetches metadata (index.json, manifest.json, .sig, .cert). Its
	// backing transport should be bounded to MaxMetadataBytes.
	HTTP Doer
	// Download fetches the (larger) release archive; its transport should be
	// bounded to MaxArtifactBytes. When nil, HTTP is used.
	Download Doer
	BaseURL  string
}

func (c *Client) downloadDoer() Doer {
	if c.Download != nil {
		return c.Download
	}
	return c.HTTP
}

// validateURL rejects any URL that is not https or whose host differs from the
// configured dist base host, before it is fetched. In production baseURL() is
// DefaultBaseURL, so this pins every fetched URL (manifest, .sig, .cert, asset
// href) to dist.conductorone.com over TLS; a test that points BaseURL at its
// own host pins to that host instead.
func (c *Client) validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("refusing to fetch non-https URL %q", raw)
	}
	base, err := url.Parse(c.baseURL())
	if err != nil {
		return fmt.Errorf("invalid base URL %q: %w", c.baseURL(), err)
	}
	if !strings.EqualFold(u.Host, base.Host) {
		return fmt.Errorf("refusing to fetch off-host URL %q (expected host %q)", raw, base.Host)
	}
	return nil
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
	m, _, err := c.ManifestRaw(ctx, url)
	return m, err
}

// ManifestRaw fetches a version's manifest.json and returns both the decoded
// Manifest and the exact bytes it was decoded from. The Sigstore signature is
// over those raw bytes, so the caller must verify the same bytes it parsed.
func (c *Client) ManifestRaw(ctx context.Context, url string) (*Manifest, []byte, error) {
	raw, err := c.getJSONBytes(ctx, url)
	if err != nil {
		return nil, nil, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, nil, fmt.Errorf("parsing %s: %w", url, err)
	}
	return &m, raw, nil
}

// GetBytes fetches url and returns the response body for a 200, with no
// JSON/content-type check. Used for the detached .sig/.cert, which are
// base64 text rather than JSON.
func (c *Client) GetBytes(ctx context.Context, url string) ([]byte, error) {
	if err := c.validateURL(url); err != nil {
		return nil, err
	}
	return c.get(ctx, c.HTTP, url)
}

func (c *Client) getJSON(ctx context.Context, url string, out any) error {
	raw, err := c.getJSONBytes(ctx, url)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("parsing %s: %w", url, err)
	}
	return nil
}

func (c *Client) getJSONBytes(ctx context.Context, url string) ([]byte, error) {
	if err := c.validateURL(url); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: HTTP %d", url, resp.StatusCode)
	}
	// dist serves the SPA HTML shell (text/html) with 200 for a path that has
	// no object; a real API response is application/json. Guard against
	// decoding the shell as an empty struct.
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "json") {
		return nil, fmt.Errorf("fetching %s: expected JSON, got Content-Type %q (no such release object?)", url, ct)
	}
	return resp.Body, nil
}

// get issues a GET through the given Doer and returns the body for a 200. The
// URL must already have been validated by the caller.
func (c *Client) get(ctx context.Context, doer Doer, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := doer.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: HTTP %d", url, resp.StatusCode)
	}
	return resp.Body, nil
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
	// the prerelease per semver §11.
	switch {
	case ap == "" && bp == "":
		return 0, true
	case ap == "":
		return 1, true
	case bp == "":
		return -1, true
	default:
		return comparePrerelease(ap, bp), true
	}
}

// comparePrerelease orders two prerelease strings per semver §11.4: compare
// dot-separated identifiers left to right; two numeric identifiers compare
// numerically (so rc.10 > rc.2), a numeric identifier ranks below a
// non-numeric one, non-numeric identifiers compare lexically (ASCII), and a
// longer identifier list outranks a shorter prefix of it.
func comparePrerelease(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if c := compareIdentifier(as[i], bs[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	default:
		return 0
	}
}

// compareIdentifier orders one prerelease identifier against another per the
// numeric/alphanumeric rules of semver §11.4.
func compareIdentifier(a, b string) int {
	an, aErr := strconv.Atoi(a)
	bn, bErr := strconv.Atoi(b)
	aNum := aErr == nil
	bNum := bErr == nil
	switch {
	case aNum && bNum:
		switch {
		case an < bn:
			return -1
		case an > bn:
			return 1
		default:
			return 0
		}
	case aNum: // numeric identifiers have lower precedence than non-numeric
		return -1
	case bNum:
		return 1
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
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
