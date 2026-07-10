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
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"golang.org/x/oauth2"
)

const assertionType = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"

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
	httpClient   *http.Client
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

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, &TokenError{StatusCode: resp.StatusCode}
	}

	c1t := &c1Token{}
	err = json.NewDecoder(resp.Body).Decode(c1t)
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

func NewTokenSource(ctx context.Context, clientID string, clientSecret string, tokenHost string) (oauth2.TokenSource, error) {
	secret, err := parseSecret([]byte(clientSecret))
	if err != nil {
		return nil, err
	}

	return oauth2.ReuseTokenSource(nil, &c1TokenSource{
		clientID:     clientID,
		clientSecret: secret,
		tokenHost:    strings.TrimPrefix(tokenHost, "https://"),
		httpClient:   &http.Client{},
	}), nil
}
