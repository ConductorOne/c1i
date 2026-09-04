package selfupdate

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"

	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/signature"
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
)

// Indirected so tests can pin different identities / a stub trust root without
// the network. Nothing in production writes to them.
var (
	pinnedSANURI     = releaseSANURI
	pinnedOIDCIssuer = releaseOIDCIssuer
	fetchTrustedRoot = root.FetchTrustedRoot
)

// VerifyManifest verifies that manifestBytes carries a valid Sigstore signature
// from ConductorOne's pinned release-workflow identity (keyless / Fulcio). dist
// serves the signature and certificate base64-encoded: sigBase64 decodes to the
// raw signature bytes, certBase64 decodes to the signing certificate in PEM.
//
// Trust model / deliberate tradeoff: the detached manifest .sig/.cert carry no
// bundled Rekor transparency-log entry, so Rekor inclusion is NOT enforced for
// the manifest signature. Instead the Fulcio certificate's embedded Signed
// Certificate Timestamp (SCT) is verified against the trust root's CT logs,
// which proves the certificate was publicly logged. Because Fulcio issues
// short-lived (~10 min) certificates, the certificate chain is validated at the
// certificate's own NotBefore (a time inside its validity window and bound to
// Fulcio's issuance), not at time.Now() — otherwise every release would fail to
// verify minutes after it was cut.
func VerifyManifest(ctx context.Context, manifestBytes, sigBase64, certBase64 []byte) error {
	return verifyManifest(ctx, manifestBytes, sigBase64, certBase64, pinnedSANURI, pinnedOIDCIssuer)
}

// verifyManifest is the identity-parameterized core so a test can drive a
// deliberately wrong pin. Every check below is mandatory: the function returns
// nil only if the certificate identity matches the pin, the signature is valid
// over exactly manifestBytes, the certificate chains to a Fulcio root, and its
// SCT verifies against the trust root's CT logs. Cheap local checks run first
// so a bad signature or wrong identity is caught without a network round trip.
func verifyManifest(ctx context.Context, manifestBytes, sigBase64, certBase64 []byte, sanURI, issuer string) error {
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
	certID, err := verify.NewCertificateIdentity(sanMatcher, issuerMatcher, certificate.Extensions{})
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

	// Trust root + chain + SCT (network / Fulcio+CT roots): the certificate must
	// chain to a Fulcio root at its issuance time and its SCT must verify.
	trustedRoot, err := fetchTrustedRoot()
	if err != nil {
		return fmt.Errorf("could not load Sigstore trust root: %w", err)
	}
	chains, err := verify.VerifyLeafCertificate(leaf.NotBefore, leaf, trustedRoot)
	if err != nil {
		return fmt.Errorf("manifest signature verification failed: %w", err)
	}
	if err := verify.VerifySignedCertificateTimestamp(chains, 1, trustedRoot); err != nil {
		return fmt.Errorf("manifest signature verification failed: %w", err)
	}
	return nil
}
