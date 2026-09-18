package tokensource

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/oauth2"
)

// Every c1i process is fresh, so without an on-disk cache each one re-mints an
// access token before its first request. The cost that matters is not latency
// (~25% of a short call) but one client_credentials event in the customer's
// audit log per invocation; agent workloads are long sequences of one-shot
// processes, so a cache collapses a burst to a single mint.
//
// The token sits beside the client secret it was minted from, same 0600/0700
// perms. It is strictly shorter-lived than that secret and grants nothing the
// secret could not re-mint on demand, so it widens no exposure. Opt out with
// C1I_NO_TOKEN_CACHE=1.
const (
	// expirySkew keeps a token that is about to expire from being handed to a
	// request that would outlive it.
	expirySkew = 60 * time.Second

	noCacheEnv = "C1I_NO_TOKEN_CACHE"
)

type cachedToken struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	Expiry      time.Time `json:"expiry"`
}

// cacheKey identifies a token by the host it is valid for AND the client id it
// was minted from -- switching either must not reuse the other's token. The id
// is hashed so it never becomes a filename.
func cacheKey(tokenHost, clientID string) string {
	sum := sha256.Sum256([]byte(tokenHost + "\x00" + clientID))
	return hex.EncodeToString(sum[:])
}

func cachePath(tokenHost, clientID string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating config dir: %w", err)
	}
	return filepath.Join(dir, "c1i", "tokens", cacheKey(tokenHost, clientID)+".json"), nil
}

// loadCachedToken returns a still-valid token, or nil. Every failure is a cache
// miss, never an error: a corrupt or unreadable cache must degrade to minting,
// not break the command.
func loadCachedToken(tokenHost, clientID string) *oauth2.Token {
	if os.Getenv(noCacheEnv) != "" {
		return nil
	}
	p, err := cachePath(tokenHost, clientID)
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(p) // #nosec G304 -- path is derived from a hash, not caller input
	if err != nil {
		return nil
	}
	var c cachedToken
	if err := json.Unmarshal(b, &c); err != nil {
		return nil
	}
	t := &oauth2.Token{AccessToken: c.AccessToken, TokenType: c.TokenType, Expiry: c.Expiry}
	if !tokenFresh(t) {
		return nil
	}
	return t
}

// tokenFresh reports whether t is usable with expirySkew of headroom. Both the
// disk load and cacheTokenSource's in-memory reuse gate on it so the same skew
// applies to every tier -- oauth2.Token.Valid() uses only its own ~10s buffer.
func tokenFresh(t *oauth2.Token) bool {
	return t != nil && t.AccessToken != "" && time.Until(t.Expiry) > expirySkew
}

// storeCachedToken persists a freshly minted token. Errors are deliberately
// swallowed: failing to cache must never fail the request that just succeeded.
func storeCachedToken(tokenHost, clientID string, t *oauth2.Token) {
	if os.Getenv(noCacheEnv) != "" || t == nil || t.AccessToken == "" {
		return
	}
	p, err := cachePath(tokenHost, clientID)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return
	}
	b, err := json.Marshal(cachedToken{AccessToken: t.AccessToken, TokenType: t.TokenType, Expiry: t.Expiry}) // #nosec G117 -- serializing for the 0600 token cache, not logging or a response
	if err != nil {
		return
	}
	// Written then renamed so a concurrent reader never sees a partial file.
	// The temp name is unique per process so two c1i runs cannot clobber it.
	tmp := fmt.Sprintf("%s.tmp%d", p, os.Getpid())
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
	}
}

// InvalidateCachedToken drops the cached token for these credentials. Callers
// use it when the API rejects a token the cache believed was still valid --
// a revoked credential or a clock far enough out to defeat expirySkew.
func InvalidateCachedToken(tokenHost, clientID string) {
	p, err := cachePath(tokenHost, clientID)
	if err != nil {
		return
	}
	_ = os.Remove(p)
}
