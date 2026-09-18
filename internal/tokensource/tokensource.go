package tokensource

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ConductorOne/c1i/internal/transport"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"golang.org/x/oauth2"
)

const assertionType = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"

// tokenRequestTimeout bounds a single client-credentials mint. Generous for a
// simple token POST, but finite so a hung/black-holed token host can't stall
// the CLI forever (the request uses context.Background()). It overrides
// transport's own (much longer) default, since a token mint should fail fast
// rather than sit at transport.DefaultTimeout. A var, not a const, so a test
// can lower it instead of waiting out the real duration.
var tokenRequestTimeout = 30 * time.Second

var (
	v1SecretTokenIdentifier = []byte("v1")
	ErrInvalidClientSecret  = fmt.Errorf("invalid client secret")
)

// TokenError is returned when the token endpoint rejects the client credentials
// (a non-200 on the client_credentials grant). Callers use errors.As to treat
// it as an authentication failure rather than a generic request error.
type TokenError struct {
	StatusCode int
}

func (e *TokenError) Error() string {
	return fmt.Sprintf("token request failed with status %d", e.StatusCode)
}

type c1Token struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Expiry      int    `json:"expires_in"`
}

type c1TokenSource struct {
	clientID     string
	clientSecret *jose.JSONWebKey
	tokenHost    string
	transport    *transport.Client
}

func parseSecret(input []byte) (*jose.JSONWebKey, error) {
	items := bytes.SplitN(input, []byte(":"), 4)
	if len(items) != 4 {
		return nil, ErrInvalidClientSecret
	}

	if !bytes.Equal(items[2], v1SecretTokenIdentifier) {
		return nil, ErrInvalidClientSecret
	}

	jwkData, err := base64.RawURLEncoding.DecodeString(string(items[3]))
	if err != nil {
		return nil, ErrInvalidClientSecret
	}

	npk := &jose.JSONWebKey{}
	err = npk.UnmarshalJSON(jwkData)
	if err != nil {
		return nil, ErrInvalidClientSecret
	}

	if npk.IsPublic() || !npk.Valid() {
		return nil, ErrInvalidClientSecret
	}

	_, ok := npk.Key.(ed25519.PrivateKey)
	if !ok {
		return nil, ErrInvalidClientSecret
	}

	return npk, nil
}

func randomNonce() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type nonceSource struct{}

func (n *nonceSource) Nonce() (string, error) {
	return randomNonce()
}

func (c *c1TokenSource) Token() (*oauth2.Token, error) {
	jsigner, err := jose.NewSigner(
		jose.SigningKey{
			Algorithm: jose.EdDSA,
			Key:       c.clientSecret,
		},
		&jose.SignerOptions{
			NonceSource: &nonceSource{},
		})
	if err != nil {
		return nil, fmt.Errorf("creating signer: %w", err)
	}

	aud := c.tokenHost
	if h, _, ok := strings.Cut(aud, ":"); ok {
		aud = h
	}

	now := time.Now()
	claims := &jwt.Claims{
		Issuer:    c.clientID,
		Subject:   c.clientID,
		Audience:  jwt.Audience{aud},
		Expiry:    jwt.NewNumericDate(now.Add(2 * time.Minute)),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now.Add(-2 * time.Minute)),
	}

	s, err := jwt.Signed(jsigner).Claims(claims).Serialize()
	if err != nil {
		return nil, fmt.Errorf("signing JWT: %w", err)
	}

	body := url.Values{
		"client_id":             {c.clientID},
		"grant_type":            {"client_credentials"},
		"client_assertion_type": {assertionType},
		"client_assertion":      {s},
	}

	tokenURL := url.URL{
		Scheme: "https",
		Host:   c.tokenHost,
		Path:   "auth/v1/token",
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, tokenURL.String(), strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.transport.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &TokenError{StatusCode: resp.StatusCode}
	}

	c1t := &c1Token{}
	err = json.Unmarshal(resp.Body, c1t)
	if err != nil {
		return nil, err
	}

	if c1t.AccessToken == "" {
		return nil, fmt.Errorf("token response contained empty access token")
	}

	return &oauth2.Token{
		AccessToken: c1t.AccessToken,
		TokenType:   c1t.TokenType,
		Expiry:      time.Now().Add(time.Duration(c1t.Expiry) * time.Second),
	}, nil
}

// NewTokenSource returns a TokenSource that mints a fresh token on every call
// via the client_credentials JWT-bearer grant, without touching the on-disk
// cache. `auth token` and the MCP gateway use it: both hand the bearer onward,
// so it must be freshly minted rather than a possibly-near-expiry cached one.
// opts are forwarded to the transport it mints through (e.g. to share a
// caller's --debug/--max-retries with the token request), but the request
// timeout is always tokenRequestTimeout regardless of what opts contains.
func NewTokenSource(ctx context.Context, clientID string, clientSecret string, tokenHost string, opts ...transport.Option) (oauth2.TokenSource, error) {
	return newMintSource(clientID, clientSecret, tokenHost, opts...)
}

// NewCachingTokenSource wraps NewTokenSource with the cross-process on-disk
// cache. The REST client uses it so a run of one-shot commands does not mint --
// and audit-log -- a token each time. It caches only the bearer c1i attaches
// automatically; a bearer handed to the caller (NewTokenSource) is never cached.
func NewCachingTokenSource(ctx context.Context, clientID string, clientSecret string, tokenHost string, opts ...transport.Option) (oauth2.TokenSource, error) {
	mint, err := newMintSource(clientID, clientSecret, tokenHost, opts...)
	if err != nil {
		return nil, err
	}
	host := strings.TrimPrefix(tokenHost, "https://")
	return &cacheTokenSource{mint: mint, host: host, clientID: clientID}, nil
}

func newMintSource(clientID string, clientSecret string, tokenHost string, opts ...transport.Option) (*c1TokenSource, error) {
	secret, err := parseSecret([]byte(clientSecret))
	if err != nil {
		return nil, err
	}
	t := transport.New(nil, append(opts, transport.WithTimeout(tokenRequestTimeout))...)
	return &c1TokenSource{
		clientID:     clientID,
		clientSecret: secret,
		tokenHost:    strings.TrimPrefix(tokenHost, "https://"),
		transport:    t,
	}, nil
}

// Invalidator is implemented by a caching TokenSource: Invalidate drops the
// cached token (in memory and on disk) so the next Token() re-mints. The REST
// client asserts for it to recover from a cached token the server has begun
// rejecting (clock skew past expirySkew, or a server-side revocation), which
// would otherwise 401 every invocation until the token's local expiry.
type Invalidator interface{ Invalidate() }

// cacheTokenSource serves a token from three tiers, cheapest first: an in-memory
// token reused for the life of this process, the on-disk cache shared across
// processes, then a fresh mint written back to disk. Cross-process reuse is the
// point: c1i workloads are long sequences of one-shot processes. Concurrency-safe.
type cacheTokenSource struct {
	mu       sync.Mutex
	tok      *oauth2.Token
	mint     oauth2.TokenSource
	host     string
	clientID string
}

func (c *cacheTokenSource) Token() (*oauth2.Token, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if tokenFresh(c.tok) {
		return c.tok, nil
	}
	if t := loadCachedToken(c.host, c.clientID); t != nil {
		c.tok = t
		return t, nil
	}
	t, err := c.mint.Token()
	if err != nil {
		return nil, err
	}
	storeCachedToken(c.host, c.clientID, t)
	c.tok = t
	return t, nil
}

// Invalidate satisfies Invalidator: drop both the in-memory and on-disk copies.
func (c *cacheTokenSource) Invalidate() {
	c.mu.Lock()
	c.tok = nil
	c.mu.Unlock()
	InvalidateCachedToken(c.host, c.clientID)
}
