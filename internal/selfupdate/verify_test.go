package selfupdate

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	_ "embed"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ConductorOne/c1i/internal/transport"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/theupdateframework/go-tuf/v2/metadata"
)

// The real v0.7.0 release manifest with its signature, certificate and Rekor
// bundle, plus a snapshot of the Sigstore public-good trusted root, so the
// whole verification runs offline.
var (
	//go:embed testdata/manifest.json
	realManifest []byte
	//go:embed testdata/manifest.json.sig
	realSigB64 []byte
	//go:embed testdata/manifest.json.cert
	realCertB64 []byte
	//go:embed testdata/manifest.json.sigstore.json
	realRekorBundle []byte
	//go:embed testdata/trusted_root.json
	trustedRootJSON []byte
)

const realSemver = "v0.7.0"

// offlineClient verifies against trustedRootJSON after edit (nil: unchanged).
func offlineClient(t *testing.T, edit func(map[string]any)) *Client {
	t.Helper()
	raw := trustedRootJSON
	if edit != nil {
		var doc map[string]any
		if err := json.Unmarshal(trustedRootJSON, &doc); err != nil {
			t.Fatal(err)
		}
		edit(doc)
		var err error
		if raw, err = json.Marshal(doc); err != nil {
			t.Fatal(err)
		}
	}
	tr, err := root.NewTrustedRootFromJSON(raw)
	if err != nil {
		t.Fatalf("loading trusted root: %v", err)
	}
	return &Client{TrustedRoot: func(context.Context) (root.TrustedMaterial, error) { return tr, nil }}
}

// editBundle returns realRekorBundle with edit applied to its decoded JSON.
func editBundle(t *testing.T, edit func(b map[string]any, payload map[string]any)) []byte {
	t.Helper()
	var b map[string]any
	if err := json.Unmarshal(realRekorBundle, &b); err != nil {
		t.Fatal(err)
	}
	edit(b, b["rekorBundle"].(map[string]any)["Payload"].(map[string]any))
	out, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestVerifyManifestRealReleaseOffline(t *testing.T) {
	if err := offlineClient(t, nil).VerifyManifest(t.Context(), realManifest, realSigB64, realCertB64, realRekorBundle, realSemver); err != nil {
		t.Fatalf("VerifyManifest on the real release = %v, want nil", err)
	}
}

func TestVerifyManifestRejects(t *testing.T) {
	realCert, err := decodeCertificate(realCertB64)
	if err != nil {
		t.Fatal(err)
	}
	forged, forgedKey := forgeCert(t, goodIdentity())
	forgedB64 := certB64(forged)
	forgedSig := signB64(t, forgedKey, realManifest)
	tampered := append([]byte(nil), realManifest...)
	tampered[len(tampered)/2] ^= 0x01

	cases := []struct {
		name                        string
		client                      *Client
		manifest, sig, cert, bundle []byte
		semver                      string
	}{
		{name: "tampered manifest bytes", manifest: tampered},
		{name: "signature not base64", sig: []byte("!!!not base64!!!")},
		{name: "certificate not PEM", cert: []byte(base64.StdEncoding.EncodeToString([]byte("not a certificate")))},
		{name: "certificate for a different release", semver: "v0.7.1"},
		{name: "self-signed certificate with forged identity", sig: forgedSig, cert: forgedB64},
		{name: "self-signed certificate in the Rekor bundle too", sig: forgedSig, cert: forgedB64,
			bundle: editBundle(t, func(b, _ map[string]any) {
				b["base64Signature"] = string(forgedSig)
				b["cert"] = string(forgedB64)
			})},
		{name: "bundle signature differs from the manifest signature",
			bundle: editBundle(t, func(b, _ map[string]any) {
				b["base64Signature"] = base64.StdEncoding.EncodeToString([]byte("another signature"))
			})},
		{name: "bundle certificate differs from the manifest certificate",
			bundle: editBundle(t, func(b, _ map[string]any) { b["cert"] = string(forgedB64) })},
		{name: "signed entry timestamp corrupted",
			bundle: editBundle(t, func(b, _ map[string]any) {
				rb := b["rekorBundle"].(map[string]any)
				set, _ := base64.StdEncoding.DecodeString(rb["SignedEntryTimestamp"].(string))
				set[len(set)-1] ^= 0x01
				rb["SignedEntryTimestamp"] = base64.StdEncoding.EncodeToString(set)
			})},
		{name: "integrated time after the certificate expired",
			bundle: editBundle(t, func(_, p map[string]any) { p["integratedTime"] = realCert.NotAfter.Add(time.Hour).Unix() })},
		{name: "certificate not issued by a trusted Fulcio CA",
			client: offlineClient(t, func(d map[string]any) { delete(d, "certificateAuthorities") })},
		{name: "certificate without a trusted SCT",
			client: offlineClient(t, func(d map[string]any) { delete(d, "ctlogs") })},
		{name: "entry not in a trusted Rekor log",
			client: offlineClient(t, func(d map[string]any) { delete(d, "tlogs") })},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.client
			if c == nil {
				c = offlineClient(t, nil)
			}
			pick := func(v, def []byte) []byte {
				if v != nil {
					return v
				}
				return def
			}
			semver := tc.semver
			if semver == "" {
				semver = realSemver
			}
			err := c.VerifyManifest(t.Context(), pick(tc.manifest, realManifest), pick(tc.sig, realSigB64),
				pick(tc.cert, realCertB64), pick(tc.bundle, realRekorBundle), semver)
			if err == nil {
				t.Fatal("VerifyManifest = nil, want an error")
			}
			if !strings.Contains(err.Error(), "signature verification failed") {
				t.Errorf("error = %v", err)
			}
		})
	}
}

func TestReleaseIdentity(t *testing.T) {
	realCert, err := decodeCertificate(realCertB64)
	if err != nil {
		t.Fatal(err)
	}
	check := func(cert *x509.Certificate, semver string) error {
		id, err := releaseIdentity(semver)
		if err != nil {
			t.Fatal(err)
		}
		s, err := certificate.SummarizeCertificate(cert)
		if err != nil {
			t.Fatal(err)
		}
		return id.Verify(s)
	}
	if err := check(realCert, "v0.7.0"); err != nil {
		t.Errorf("real v0.7.0 certificate rejected: %v", err)
	}
	if err := check(realCert, "0.7.0"); err != nil {
		t.Errorf("real certificate rejected for an unprefixed semver: %v", err)
	}
	if check(realCert, "v0.7.1") == nil {
		t.Error("v0.7.0 certificate accepted for v0.7.1")
	}

	const workflow = "https://github.com/ConductorOne/github-workflows/.github/workflows/release.yaml"
	cases := []struct {
		name   string
		edit   func(*certIdentity)
		accept bool
	}{
		{"all fields correct", func(*certIdentity) {}, true},
		{"next workflow major", func(c *certIdentity) { c.san = workflow + "@refs/tags/v5" }, true},
		{"two-digit workflow major", func(c *certIdentity) { c.san = workflow + "@refs/tags/v12" }, true},
		{"workflow minor tag", func(c *certIdentity) { c.san = workflow + "@refs/tags/v4.1" }, false},
		{"workflow tag with a suffix", func(c *certIdentity) { c.san = workflow + "@refs/tags/v4-evil" }, false},
		{"workflow on a branch", func(c *certIdentity) { c.san = workflow + "@refs/heads/v4" }, false},
		{"workflow in another org", func(c *certIdentity) {
			c.san = "https://github.com/evil/github-workflows/.github/workflows/release.yaml@refs/tags/v4"
		}, false},
		{"workflow URL with a prefix", func(c *certIdentity) { c.san = "https://evil.example/" + workflow + "@refs/tags/v4" }, false},
		{"another OIDC issuer", func(c *certIdentity) { c.issuer = "https://accounts.google.com" }, false},
		{"another source repository", func(c *certIdentity) { c.repo = "https://github.com/ConductorOne/other" }, false},
		{"another tag", func(c *certIdentity) { c.ref = "refs/tags/v0.7.1" }, false},
		{"a branch ref", func(c *certIdentity) { c.ref = "refs/heads/main" }, false},
		{"manual trigger", func(c *certIdentity) { c.trigger = "workflow_dispatch" }, false},
		{"self-hosted runner", func(c *certIdentity) { c.runner = "self-hosted" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := goodIdentity()
			tc.edit(&id)
			cert, _ := forgeCert(t, id)
			if err := check(cert, "v0.7.0"); (err == nil) != tc.accept {
				t.Errorf("identity check = %v, want accept=%v", err, tc.accept)
			}
		})
	}
}

// certIdentity is the identity a forged certificate claims.
type certIdentity struct{ san, issuer, repo, ref, trigger, runner string }

func goodIdentity() certIdentity {
	return certIdentity{
		san:     "https://github.com/ConductorOne/github-workflows/.github/workflows/release.yaml@refs/tags/v4",
		issuer:  releaseOIDCIssuer,
		repo:    releaseSourceRepositoryURI,
		ref:     "refs/tags/v0.7.0",
		trigger: "push",
		runner:  "github-hosted",
	}
}

// forgeCert self-signs a code-signing certificate claiming id.
func forgeCert(t *testing.T, id certIdentity) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	san, err := url.Parse(id.san)
	if err != nil {
		t.Fatal(err)
	}
	ext := func(n int, v string) pkix.Extension {
		der, err := asn1.MarshalWithParams(v, "utf8")
		if err != nil {
			t.Fatal(err)
		}
		return pkix.Extension{Id: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, n}, Value: der}
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		URIs:         []*url.URL{san},
		ExtraExtensions: []pkix.Extension{
			ext(8, id.issuer), ext(12, id.repo), ext(14, id.ref), ext(20, id.trigger), ext(11, id.runner),
		},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key
}

func certB64(c *x509.Certificate) []byte {
	return []byte(base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})))
}

func signB64(t *testing.T, key *ecdsa.PrivateKey, msg []byte) []byte {
	t.Helper()
	digest := sha256.Sum256(msg)
	sig, err := key.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	return []byte(base64.StdEncoding.EncodeToString(sig))
}

// recordingDoer answers every request with resp and records the URLs asked for.
type recordingDoer struct {
	resp *transport.Response
	urls []string
}

func (d *recordingDoer) Do(req *http.Request) (*transport.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	d.urls = append(d.urls, req.URL.String())
	return d.resp, nil
}

func TestTUFFetcher(t *testing.T) {
	const u = "https://tuf.example/2.root.json"
	ok := &recordingDoer{resp: &transport.Response{StatusCode: 200, Body: []byte("12345")}}
	if body, err := (tufFetcher{ctx: t.Context(), doer: ok}).DownloadFile(u, 5, 0); err != nil || string(body) != "12345" {
		t.Errorf("DownloadFile = (%q, %v), want the body", body, err)
	}
	if len(ok.urls) != 1 || ok.urls[0] != u {
		t.Errorf("requests = %v, want one through the Doer", ok.urls)
	}
	if _, err := (tufFetcher{ctx: t.Context(), doer: ok}).DownloadFile(u, 4, 0); err == nil {
		t.Error("DownloadFile accepted a body over maxLength")
	}

	// go-tuf ends its root-version probe only on a typed 404.
	notFound := &recordingDoer{resp: &transport.Response{StatusCode: 404}}
	_, err := (tufFetcher{ctx: t.Context(), doer: notFound}).DownloadFile(u, 5, 0)
	var httpErr *metadata.ErrDownloadHTTP
	if !errors.As(err, &httpErr) || httpErr.StatusCode != 404 {
		t.Errorf("404 error = %v, want *metadata.ErrDownloadHTTP{404}", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := (tufFetcher{ctx: ctx, doer: ok}).DownloadFile(u, 5, 0); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled DownloadFile error = %v, want context.Canceled", err)
	}
}

// TestVerifyManifestReal verifies the live v0.7.0 release, fetching the trust
// root through the shared transport into an empty TUF cache. Skips offline.
func TestVerifyManifestReal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network integration test in -short mode")
	}
	const base = "https://dist.conductorone.com/releases/ConductorOne/c1i/v0.7.0"
	man := fetchOrSkip(t, base+"/manifest.json")
	sig := fetchOrSkip(t, base+"/manifest.json.sig")
	cert := fetchOrSkip(t, base+"/manifest.json.cert")
	rekorBundle := fetchOrSkip(t, base+"/manifest.json.sigstore.json")

	t.Setenv("HOME", t.TempDir())
	c := &Client{HTTP: transport.New(nil, transport.WithMaxResponseBytes(MaxMetadataBytes))}
	if err := c.VerifyManifest(t.Context(), man, sig, cert, rekorBundle, "v0.7.0"); err != nil {
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
