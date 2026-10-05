package cmd

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/url"
	"testing"
	"time"

	"github.com/sigstore/sigstore-go/pkg/root"
)

// fakeSigstore is a throwaway Fulcio CA, CT log and Rekor log, so a test can
// sign any manifest the way c1i's release workflow does and verify it for real.
type fakeSigstore struct {
	ca                   *x509.Certificate
	caKey, ctKey, rkrKey *ecdsa.PrivateKey
	root                 root.TrustedMaterial
}

func newFakeSigstore(t *testing.T) *fakeSigstore {
	t.Helper()
	f := &fakeSigstore{caKey: newKey(t), ctKey: newKey(t), rkrKey: newKey(t)}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "fake fulcio"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	f.ca = createCert(t, tmpl, tmpl, &f.caKey.PublicKey, f.caKey)

	logs := func(key *ecdsa.PrivateKey) map[string]*root.TransparencyLog {
		id := logID(t, key)
		return map[string]*root.TransparencyLog{hex.EncodeToString(id): {
			BaseURL: "https://log.invalid", ID: id, ValidityPeriodStart: now.Add(-time.Hour),
			HashFunc: crypto.SHA256, PublicKey: key.Public(), SignatureHashFunc: crypto.SHA256,
		}}
	}
	tr, err := root.NewTrustedRoot(root.TrustedRootMediaType01,
		[]root.CertificateAuthority{&root.FulcioCertificateAuthority{Root: f.ca, ValidityPeriodStart: now.Add(-time.Hour)}},
		logs(f.ctKey), nil, logs(f.rkrKey))
	if err != nil {
		t.Fatal(err)
	}
	f.root = tr
	return f
}

// sign returns dist's signature material for manifest: the base64 signature,
// the base64 PEM certificate (identity for semver), and the Rekor bundle.
func (f *fakeSigstore) sign(t *testing.T, manifest []byte, semver string) (sigB64, certB64, bundle []byte) {
	t.Helper()
	leafKey := newKey(t)
	cert := f.leaf(t, leafKey, "refs/tags/"+semver)
	digest := sha256.Sum256(manifest)
	sig, err := ecdsa.SignASN1(rand.Reader, leafKey, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	sigB64 = []byte(base64.StdEncoding.EncodeToString(sig))
	certB64 = []byte(base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})))

	body, _ := json.Marshal(map[string]any{
		"apiVersion": "0.0.1",
		"kind":       "hashedrekord",
		"spec": map[string]any{
			"data":      map[string]any{"hash": map[string]any{"algorithm": "sha256", "value": hex.EncodeToString(digest[:])}},
			"signature": map[string]any{"content": string(sigB64), "publicKey": map[string]any{"content": string(certB64)}},
		},
	})
	// Field order is the canonical (sorted) JSON the SET signs.
	payload := struct {
		Body           string `json:"body"`
		IntegratedTime int64  `json:"integratedTime"`
		LogID          string `json:"logID"`
		LogIndex       int64  `json:"logIndex"`
	}{base64.StdEncoding.EncodeToString(body), time.Now().Unix(), hex.EncodeToString(logID(t, f.rkrKey)), 1}
	canonical, _ := json.Marshal(payload)
	set := signDigest(t, f.rkrKey, canonical)
	bundle, _ = json.Marshal(map[string]any{
		"base64Signature": string(sigB64),
		"cert":            string(certB64),
		"rekorBundle":     map[string]any{"SignedEntryTimestamp": base64.StdEncoding.EncodeToString(set), "Payload": payload},
	})
	return sigB64, certB64, bundle
}

// leaf issues a release-workflow certificate with an embedded SCT.
func (f *fakeSigstore) leaf(t *testing.T, key *ecdsa.PrivateKey, ref string) *x509.Certificate {
	t.Helper()
	san, _ := url.Parse("https://github.com/ConductorOne/github-workflows/.github/workflows/release.yaml@refs/tags/v4")
	ext := func(n int, v string) pkix.Extension {
		der, err := asn1.MarshalWithParams(v, "utf8")
		if err != nil {
			t.Fatal(err)
		}
		return pkix.Extension{Id: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, n}, Value: der}
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(10 * time.Minute),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		URIs:         []*url.URL{san},
		ExtraExtensions: []pkix.Extension{
			ext(8, "https://token.actions.githubusercontent.com"), ext(12, "https://github.com/ConductorOne/c1i"),
			ext(14, ref), ext(20, "push"), ext(11, "github-hosted"),
		},
	}
	pre := createCert(t, tmpl, f.ca, &key.PublicKey, f.caKey)

	// RFC 6962 SCT over the precertificate entry.
	ts := uint64(now.UnixMilli()) // #nosec G115 -- a current time is positive
	issuerHash := sha256.Sum256(f.ca.RawSubjectPublicKeyInfo)
	var in []byte
	in = append(in, 0, 0) // v1, certificate_timestamp
	in = binary.BigEndian.AppendUint64(in, ts)
	in = append(in, 0, 1) // precert_entry
	in = append(in, issuerHash[:]...)
	tbs := pre.RawTBSCertificate
	in = append(in, byte(len(tbs)>>16), byte(len(tbs)>>8), byte(len(tbs)))
	in = append(in, tbs...)
	in = append(in, 0, 0) // no extensions
	sctSig := signDigest(t, f.ctKey, in)

	var sct []byte
	sct = append(sct, 0) // v1
	sct = append(sct, logID(t, f.ctKey)...)
	sct = binary.BigEndian.AppendUint64(sct, ts)
	sct = append(sct, 0, 0, 4, 3)                                 // no extensions; sha256, ecdsa
	sct = binary.BigEndian.AppendUint16(sct, uint16(len(sctSig))) // #nosec G115 -- an ECDSA signature is short
	sct = append(sct, sctSig...)
	list := binary.BigEndian.AppendUint16(nil, uint16(len(sct)+2)) // #nosec G115
	list = binary.BigEndian.AppendUint16(list, uint16(len(sct)))   // #nosec G115
	list = append(list, sct...)
	listDER, err := asn1.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	tmpl.ExtraExtensions = append(tmpl.ExtraExtensions, pkix.Extension{Id: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 11129, 2, 4, 2}, Value: listDER})
	return createCert(t, tmpl, f.ca, &key.PublicKey, f.caKey)
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func createCert(t *testing.T, tmpl, parent *x509.Certificate, pub *ecdsa.PublicKey, signer *ecdsa.PrivateKey) *x509.Certificate {
	t.Helper()
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, signer)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func logID(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil {
		t.Fatal(err)
	}
	id := sha256.Sum256(der)
	return id[:]
}

func signDigest(t *testing.T, key *ecdsa.PrivateKey, msg []byte) []byte {
	t.Helper()
	d := sha256.Sum256(msg)
	sig, err := ecdsa.SignASN1(rand.Reader, key, d[:])
	if err != nil {
		t.Fatal(err)
	}
	return sig
}
