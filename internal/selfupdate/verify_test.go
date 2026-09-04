package selfupdate

import (
	"context"
	_ "embed"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The real v0.7.0 release manifest and its detached Sigstore signature +
// certificate. These are public release artifacts (no secrets), used to drive
// the offline failure paths: identity and signature are checked locally before
// any trust-root fetch, so a wrong pin or tampered bytes fail without network.
var (
	//go:embed testdata/manifest.json
	realManifest []byte
	//go:embed testdata/manifest.json.sig
	realSigB64 []byte
	//go:embed testdata/manifest.json.cert
	realCertB64 []byte
)

func TestVerifyManifestBadBase64(t *testing.T) {
	err := VerifyManifest(context.Background(), realManifest, []byte("!!!not base64!!!"), realCertB64)
	if err == nil {
		t.Fatal("expected an error for a non-base64 signature")
	}
	if !strings.Contains(err.Error(), "signature verification failed") {
		t.Errorf("error = %v", err)
	}
}

func TestVerifyManifestNonPEMCert(t *testing.T) {
	notPEM := base64.StdEncoding.EncodeToString([]byte("this is not a PEM certificate"))
	err := VerifyManifest(context.Background(), realManifest, realSigB64, []byte(notPEM))
	if err == nil {
		t.Fatal("expected an error for a non-PEM certificate")
	}
	if !strings.Contains(err.Error(), "not PEM-encoded") {
		t.Errorf("error = %v", err)
	}
}

func TestVerifyManifestTamperedBytes(t *testing.T) {
	// Real cert + real signature, but altered manifest bytes: the signature no
	// longer covers these bytes, so verification must fail. This runs offline —
	// the signature check precedes the trust-root fetch.
	tampered := append([]byte(nil), realManifest...)
	tampered[0] ^= 0xff
	err := VerifyManifest(context.Background(), tampered, realSigB64, realCertB64)
	if err == nil {
		t.Fatal("expected an error for tampered manifest bytes")
	}
	if !strings.Contains(err.Error(), "signature verification failed") {
		t.Errorf("error = %v", err)
	}
}

func TestVerifyManifestIdentityMismatch(t *testing.T) {
	// Verify the real signature against a deliberately wrong pinned SAN. The
	// identity check runs before the trust-root fetch, so this is offline.
	err := verifyManifest(context.Background(), realManifest, realSigB64, realCertB64,
		"https://github.com/evilcorp/evil/.github/workflows/release.yaml@refs/tags/v4",
		pinnedOIDCIssuer)
	if err == nil {
		t.Fatal("expected an error when the pinned SAN does not match the certificate")
	}
	if !strings.Contains(err.Error(), "signature verification failed") {
		t.Errorf("error = %v", err)
	}

	// A wrong issuer must fail too.
	err = verifyManifest(context.Background(), realManifest, realSigB64, realCertB64,
		pinnedSANURI, "https://accounts.google.com")
	if err == nil {
		t.Fatal("expected an error when the pinned issuer does not match the certificate")
	}
}

// TestVerifyManifestReal is the live integration test: it fetches the REAL
// v0.7.0 manifest, signature and certificate from dist and verifies them
// against the production pinned identity. It must PASS when online; it skips
// cleanly under -short or when the network is unavailable.
func TestVerifyManifestReal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network integration test in -short mode")
	}
	const base = "https://dist.conductorone.com/releases/ConductorOne/c1i/v0.7.0"
	man := fetchOrSkip(t, base+"/manifest.json")
	sig := fetchOrSkip(t, base+"/manifest.json.sig")
	cert := fetchOrSkip(t, base+"/manifest.json.cert")

	if err := VerifyManifest(context.Background(), man, sig, cert); err != nil {
		t.Fatalf("VerifyManifest on the real v0.7.0 release failed: %v", err)
	}
}

func fetchOrSkip(t *testing.T, url string) []byte {
	t.Helper()
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Skipf("skipping: cannot reach %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("skipping: %s returned HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxMetadataBytes))
	if err != nil {
		t.Skipf("skipping: reading %s: %v", url, err)
	}
	return body
}
