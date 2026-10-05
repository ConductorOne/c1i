package selfupdate

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/theupdateframework/go-tuf/v2/metadata"
	tuffetcher "github.com/theupdateframework/go-tuf/v2/metadata/fetcher"
)

// The manifest is signed keylessly (Fulcio) by the shared release workflow
// running in c1i's repository. A wrong or absent pin makes the check worthless.
const (
	// Any major tag of the workflow, so its next major doesn't strand installed clients.
	releaseSANPattern          = `^https://github\.com/ConductorOne/github-workflows/\.github/workflows/release\.yaml@refs/tags/v[0-9]+$`
	releaseOIDCIssuer          = "https://token.actions.githubusercontent.com"
	releaseSourceRepositoryURI = "https://github.com/ConductorOne/c1i"
)

// releaseIdentity is the certificate identity a manifest for semver must carry:
// c1i's tag for that version, built on a tag push by a GitHub-hosted runner.
func releaseIdentity(semver string) (verify.CertificateIdentity, error) {
	san, err := verify.NewSANMatcher("", releaseSANPattern)
	if err != nil {
		return verify.CertificateIdentity{}, err
	}
	issuer, err := verify.NewIssuerMatcher(releaseOIDCIssuer, "")
	if err != nil {
		return verify.CertificateIdentity{}, err
	}
	return verify.NewCertificateIdentity(san, issuer, certificate.Extensions{
		SourceRepositoryURI: releaseSourceRepositoryURI,
		SourceRepositoryRef: "refs/tags/v" + strings.TrimPrefix(semver, "v"),
		BuildTrigger:        "push",
		RunnerEnvironment:   "github-hosted",
	})
}

// VerifyManifest checks that manifestBytes carry a valid Sigstore signature
// from c1i's release workflow for release semver. dist serves the signature
// and the PEM certificate base64-encoded.
func (c *Client) VerifyManifest(ctx context.Context, manifestBytes, sigBase64, certBase64, rekorBundle []byte, semver string) error {
	if err := c.verifyManifest(ctx, manifestBytes, sigBase64, certBase64, rekorBundle, semver); err != nil {
		return fmt.Errorf("manifest signature verification failed: %w", err)
	}
	return nil
}

func (c *Client) verifyManifest(ctx context.Context, manifestBytes, sigBase64, certBase64, rekorBundle []byte, semver string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(sigBase64)))
	if err != nil {
		return fmt.Errorf("decoding signature: %w", err)
	}
	leaf, err := decodeCertificate(certBase64)
	if err != nil {
		return err
	}

	// Local checks first, so a bad identity or signature fails without the network.
	id, err := releaseIdentity(semver)
	if err != nil {
		return err
	}
	summary, err := certificate.SummarizeCertificate(leaf)
	if err != nil {
		return err
	}
	if err := id.Verify(summary); err != nil {
		return err
	}
	sv, err := signature.LoadVerifier(leaf.PublicKey, crypto.SHA256)
	if err != nil {
		return err
	}
	if err := sv.VerifySignature(bytes.NewReader(sig), bytes.NewReader(manifestBytes)); err != nil {
		return err
	}

	trustedRoot, err := c.trustedRoot(ctx)
	if err != nil {
		return fmt.Errorf("loading the Sigstore trust root: %w", err)
	}
	// The Rekor entry proves when the signature was made; the certificate must
	// chain to Fulcio and carry a CT log SCT as of that time.
	integratedTime, err := verifyRekorBundle(manifestBytes, sig, leaf, rekorBundle, trustedRoot)
	if err != nil {
		return err
	}
	chains, err := verify.VerifyLeafCertificate(integratedTime, leaf, trustedRoot)
	if err != nil {
		return err
	}
	return verify.VerifySignedCertificateTimestamp(chains, 1, trustedRoot)
}

func decodeCertificate(certBase64 []byte) (*x509.Certificate, error) {
	certPEM, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(certBase64)))
	if err != nil {
		return nil, fmt.Errorf("decoding certificate: %w", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("certificate is not PEM-encoded")
	}
	return x509.ParseCertificate(block.Bytes)
}

func (c *Client) trustedRoot(ctx context.Context) (root.TrustedMaterial, error) {
	if c.TrustedRoot != nil {
		return c.TrustedRoot(ctx)
	}
	opts := tuf.DefaultOptions().WithContext(ctx).WithFetcher(tufFetcher{ctx: ctx, doer: c.HTTP})
	return root.FetchTrustedRootWithOptions(opts)
}

// tufFetcher routes TUF downloads through the shared transport, so they get
// the redirect guard, --debug and --max-retries.
type tufFetcher struct {
	ctx  context.Context
	doer Doer
}

var _ tuffetcher.Fetcher = tufFetcher{}

func (f tufFetcher) DownloadFile(url string, maxLength int64, _ time.Duration) ([]byte, error) {
	req, err := http.NewRequestWithContext(f.ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.doer.Do(req)
	if err != nil {
		return nil, err
	}
	// go-tuf stops probing for newer roots on this typed 404.
	if resp.StatusCode != http.StatusOK {
		return nil, &metadata.ErrDownloadHTTP{StatusCode: resp.StatusCode, URL: url}
	}
	if int64(len(resp.Body)) > maxLength {
		return nil, &metadata.ErrDownloadLengthMismatch{Msg: fmt.Sprintf("%s exceeds %d bytes", url, maxLength)}
	}
	return resp.Body, nil
}
