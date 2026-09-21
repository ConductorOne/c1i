package selfupdate

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"time"

	bundlepb "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	commonpb "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	rekorpb "github.com/sigstore/protobuf-specs/gen/pb-go/rekor/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// legacyRekorBundle is the release bundle format currently published by dist.
type legacyRekorBundle struct {
	Base64Signature string `json:"base64Signature"`
	Cert            string `json:"cert"`
	RekorBundle     struct {
		SignedEntryTimestamp string `json:"SignedEntryTimestamp"`
		Payload              struct {
			Body           string `json:"body"`
			IntegratedTime int64  `json:"integratedTime"`
			LogIndex       int64  `json:"logIndex"`
			LogID          string `json:"logID"`
		} `json:"Payload"`
	} `json:"rekorBundle"`
}

func verifyRekorBundle(manifest, signature []byte, leaf *x509.Certificate, rawBundle []byte, trustedRoot root.TrustedMaterial) (time.Time, error) {
	var legacy legacyRekorBundle
	if err := json.Unmarshal(rawBundle, &legacy); err != nil {
		return time.Time{}, fmt.Errorf("parsing Rekor bundle: %w", err)
	}

	bundleSignature, err := base64.StdEncoding.DecodeString(legacy.Base64Signature)
	if err != nil {
		return time.Time{}, fmt.Errorf("decoding Rekor bundle signature: %w", err)
	}
	if !bytes.Equal(bundleSignature, signature) {
		return time.Time{}, fmt.Errorf("rekor bundle signature does not match manifest signature")
	}
	bundleCertificatePEM, err := base64.StdEncoding.DecodeString(legacy.Cert)
	if err != nil {
		return time.Time{}, fmt.Errorf("decoding Rekor bundle certificate: %w", err)
	}
	block, _ := pem.Decode(bundleCertificatePEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return time.Time{}, fmt.Errorf("rekor bundle certificate is not PEM-encoded")
	}
	bundleCertificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing Rekor bundle certificate: %w", err)
	}
	if !bytes.Equal(bundleCertificate.Raw, leaf.Raw) {
		return time.Time{}, fmt.Errorf("rekor bundle certificate does not match manifest certificate")
	}
	body, err := base64.StdEncoding.DecodeString(legacy.RekorBundle.Payload.Body)
	if err != nil {
		return time.Time{}, fmt.Errorf("decoding Rekor bundle entry: %w", err)
	}
	logID, err := hex.DecodeString(legacy.RekorBundle.Payload.LogID)
	if err != nil {
		return time.Time{}, fmt.Errorf("decoding Rekor log ID: %w", err)
	}
	set, err := base64.StdEncoding.DecodeString(legacy.RekorBundle.SignedEntryTimestamp)
	if err != nil {
		return time.Time{}, fmt.Errorf("decoding Rekor signed entry timestamp: %w", err)
	}

	digest := sha256.Sum256(manifest)
	entity, err := bundle.NewBundle(&bundlepb.Bundle{
		MediaType: "application/vnd.dev.sigstore.bundle+json;version=0.1",
		Content: &bundlepb.Bundle_MessageSignature{MessageSignature: &commonpb.MessageSignature{
			MessageDigest: &commonpb.HashOutput{Algorithm: commonpb.HashAlgorithm_SHA2_256, Digest: digest[:]},
			Signature:     signature,
		}},
		VerificationMaterial: &bundlepb.VerificationMaterial{
			Content: &bundlepb.VerificationMaterial_Certificate{Certificate: &commonpb.X509Certificate{RawBytes: leaf.Raw}},
			TlogEntries: []*rekorpb.TransparencyLogEntry{{
				LogIndex:       legacy.RekorBundle.Payload.LogIndex,
				LogId:          &commonpb.LogId{KeyId: logID},
				KindVersion:    &rekorpb.KindVersion{Kind: "hashedrekord", Version: "0.0.1"},
				IntegratedTime: legacy.RekorBundle.Payload.IntegratedTime,
				InclusionPromise: &rekorpb.InclusionPromise{
					SignedEntryTimestamp: set,
				},
				CanonicalizedBody: body,
			}},
		},
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("building Rekor bundle: %w", err)
	}
	timestamps, err := verify.VerifyTlogEntry(entity, trustedRoot, 1, true)
	if err != nil {
		return time.Time{}, fmt.Errorf("verifying Rekor bundle: %w", err)
	}
	if len(timestamps) != 1 {
		return time.Time{}, fmt.Errorf("verifying Rekor bundle: expected one verified timestamp, got %d", len(timestamps))
	}
	return timestamps[0].Time, nil
}
