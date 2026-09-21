package selfupdate

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ConductorOne/c1i/internal/transport"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/theupdateframework/go-tuf/v2/metadata"
	tuffetcher "github.com/theupdateframework/go-tuf/v2/metadata/fetcher"
)

// The release manifest is signed keylessly by ConductorOne's reusable release
// workflow via Fulcio. These pin the exact identity that signature must carry:
// a wrong or absent pin makes the whole check worthless.
const (
	// releaseSANURI is the Subject Alternative Name (a URI) Fulcio stamps with
	// the signing workflow's identity: the reusable workflow at the v4 tag.
	releaseSANURI = "https://github.com/ConductorOne/github-workflows/.github/workflows/release.yaml@refs/tags/v4"
	// releaseOIDCIssuer is the OIDC issuer that minted the workflow's identity
	// token (GitHub Actions).
	releaseOIDCIssuer = "https://token.actions.githubusercontent.com"
	// releaseSourceRepositoryURI binds the shared release workflow to c1i.
	releaseSourceRepositoryURI = "https://github.com/ConductorOne/c1i"

	trustRootTimeout = 30 * time.Second
)

// Indirected so tests can pin different identities / a stub trust root without
// the network. Nothing in production writes to them.
var (
	pinnedSANURI              = releaseSANURI
	pinnedOIDCIssuer          = releaseOIDCIssuer
	pinnedSourceRepositoryURI = releaseSourceRepositoryURI
	fetchTrustedRoot          = fetchTrustedRootWithContext
)

// VerifyManifest verifies that manifestBytes carries a valid Sigstore signature
// from ConductorOne's pinned release-workflow identity (keyless / Fulcio). dist
// serves the signature and certificate base64-encoded: sigBase64 decodes to the
// raw signature bytes, certBase64 decodes to the signing certificate in PEM.
func VerifyManifest(ctx context.Context, manifestBytes, sigBase64, certBase64, rekorBundle []byte) error {
	return verifyManifest(ctx, manifestBytes, sigBase64, certBase64, rekorBundle, pinnedSANURI, pinnedOIDCIssuer, pinnedSourceRepositoryURI)
}

// verifyManifest is the identity-parameterized core so a test can drive a
// deliberately wrong pin. Every check below is mandatory: the function returns
// nil only if the certificate identity matches the pin, the signature is valid
// over exactly manifestBytes, the certificate chains to a Fulcio root, and the
// signed Rekor entry proves the signature existed while the certificate was valid.
func verifyManifest(ctx context.Context, manifestBytes, sigBase64, certBase64, rekorBundle []byte, sanURI, issuer, sourceRepositoryURI string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	sig, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(sigBase64)))
	if err != nil {
		return fmt.Errorf("manifest signature verification failed: decoding signature: %w", err)
	}
	certPEM, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(certBase64)))
	if err != nil {
		return fmt.Errorf("manifest signature verification failed: decoding certificate: %w", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return fmt.Errorf("manifest signature verification failed: certificate is not PEM-encoded")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("manifest signature verification failed: parsing certificate: %w", err)
	}

	// Identity pin (local): the certificate must name the exact release
	// workflow and OIDC issuer.
	sanMatcher, err := verify.NewSANMatcher(sanURI, "")
	if err != nil {
		return fmt.Errorf("manifest signature verification failed: %w", err)
	}
	issuerMatcher, err := verify.NewIssuerMatcher(issuer, "")
	if err != nil {
		return fmt.Errorf("manifest signature verification failed: %w", err)
	}
	certID, err := verify.NewCertificateIdentity(sanMatcher, issuerMatcher, certificate.Extensions{SourceRepositoryURI: sourceRepositoryURI})
	if err != nil {
		return fmt.Errorf("manifest signature verification failed: %w", err)
	}
	summary, err := certificate.SummarizeCertificate(leaf)
	if err != nil {
		return fmt.Errorf("manifest signature verification failed: %w", err)
	}
	if err := certID.Verify(summary); err != nil {
		return fmt.Errorf("manifest signature verification failed: %w", err)
	}

	// Signature (local): the certificate's key must sign exactly these bytes.
	sv, err := signature.LoadVerifier(leaf.PublicKey, crypto.SHA256)
	if err != nil {
		return fmt.Errorf("manifest signature verification failed: %w", err)
	}
	if err := sv.VerifySignature(bytes.NewReader(sig), bytes.NewReader(manifestBytes)); err != nil {
		return fmt.Errorf("manifest signature verification failed: %w", err)
	}

	// Trust root + signed Rekor entry: the entry binds this manifest signature
	// and certificate to a trusted transparency-log timestamp.
	trustedRoot, err := fetchTrustedRoot(ctx)
	if err != nil {
		return fmt.Errorf("could not load Sigstore trust root: %w", err)
	}
	integratedTime, err := verifyRekorBundle(manifestBytes, sig, leaf, rekorBundle, trustedRoot)
	if err != nil {
		return fmt.Errorf("manifest signature verification failed: %w", err)
	}
	chains, err := verify.VerifyLeafCertificate(integratedTime, leaf, trustedRoot)
	if err != nil {
		return fmt.Errorf("manifest signature verification failed: %w", err)
	}
	if err := verify.VerifySignedCertificateTimestamp(chains, 1, trustedRoot); err != nil {
		return fmt.Errorf("manifest signature verification failed: %w", err)
	}
	return nil
}

func fetchTrustedRootWithContext(ctx context.Context) (root.TrustedMaterial, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	opts := tuf.DefaultOptions().
		WithContext(ctx).
		WithFetcher(trustRootFetcher{ctx: ctx})
	return root.FetchTrustedRootWithOptions(opts)
}

type trustRootFetcher struct {
	ctx context.Context
}

var _ tuffetcher.Fetcher = trustRootFetcher{}

func (f trustRootFetcher) DownloadFile(url string, maxLength int64, _ time.Duration) ([]byte, error) {
	req, err := http.NewRequestWithContext(f.ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := transport.NewSingleAttemptHTTPClient(trustRootTimeout).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, &metadata.ErrDownloadHTTP{StatusCode: resp.StatusCode, URL: url}
	}
	if length := resp.ContentLength; length > maxLength {
		return nil, &metadata.ErrDownloadLengthMismatch{Msg: fmt.Sprintf("download failed for %s, length %d is larger than expected %d", url, length, maxLength)}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxLength+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxLength {
		return nil, &metadata.ErrDownloadLengthMismatch{Msg: fmt.Sprintf("download failed for %s: response exceeds %d bytes", url, maxLength)}
	}
	return body, nil
}
