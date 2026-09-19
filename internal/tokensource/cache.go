package tokensource

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ConductorOne/c1i/internal/keychain"
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

	// maxCachedTokenBytes bounds corrupt cache input. OAuth access tokens are
	// compact credentials, so 64 KiB leaves substantial room without letting a
	// hostile or accidental cache file exhaust the CLI's memory.
	maxCachedTokenBytes = 64 << 10

	noCacheEnv = "C1I_NO_TOKEN_CACHE"
)

const tokenKeychainService = "com.conductorone.c1i.tokens"

var (
	tokenKeyringGet    = keychain.GetSecret
	tokenKeyringSet    = keychain.SetSecret
	tokenKeyringDelete = keychain.DeleteSecret
)

type cachedToken struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	Expiry      time.Time `json:"expiry"`
}

// cacheKey identifies a token by the host, client id, and credential generation
// it was minted from. Replacing a secret for the same client id must not reuse
// a bearer minted by the old secret. The hash keeps all three out of filenames.
func cacheKey(tokenHost, clientID, clientSecret string) string {
	sum := sha256.Sum256([]byte(tokenHost + "\x00" + clientID + "\x00" + clientSecret))
	return hex.EncodeToString(sum[:])
}

func cachePath(key string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating config dir: %w", err)
	}
	return filepath.Join(dir, "c1i", "tokens", key+".json"), nil
}

// trustedCacheDir is deliberately stricter than the config root: the token
// cache holds a live bearer, so neither it nor its c1i parent may be a symlink
// or accessible to group/other users. The owning user is the trust boundary.
func trustedCacheDir(dir string) bool {
	info, err := os.Lstat(dir)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0o077 == 0
}

// loadCachedToken returns a still-valid token, or nil. Every failure is a cache
// miss, never an error: a corrupt or unreadable cache must degrade to minting,
// not break the command. The OS keychain is the primary store; the hardened
// file cache exists for headless environments without a usable keychain.
func loadCachedToken(key string) *oauth2.Token {
	if os.Getenv(noCacheEnv) != "" {
		return nil
	}
	if encoded, err := tokenKeyringGet(tokenKeychainService, key); err == nil {
		return decodeCachedToken([]byte(encoded))
	} else if !keychain.IsUnavailable(err) {
		return nil
	}
	return loadFileCachedToken(key)
}

func decodeCachedToken(b []byte) *oauth2.Token {
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

func loadFileCachedToken(key string) *oauth2.Token {
	p, err := cachePath(key)
	if err != nil {
		return nil
	}
	if !trustedCacheDir(filepath.Dir(filepath.Dir(p))) || !trustedCacheDir(filepath.Dir(p)) {
		return nil
	}
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil
	}
	f, err := os.Open(p) // #nosec G304 -- p contains only a locally-derived cache key
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxCachedTokenBytes+1))
	if err != nil || len(b) > maxCachedTokenBytes {
		return nil
	}
	return decodeCachedToken(b)
}

// tokenFresh reports whether t is usable with expirySkew of headroom. Both the
// disk load and cacheTokenSource's in-memory reuse gate on it so the same skew
// applies to every tier -- oauth2.Token.Valid() uses only its own ~10s buffer.
func tokenFresh(t *oauth2.Token) bool {
	return t != nil && t.AccessToken != "" && time.Until(t.Expiry) > expirySkew
}

// storeCachedToken persists a freshly minted token. Errors are deliberately
// swallowed: failing to cache must never fail the request that just succeeded.
func storeCachedToken(key string, t *oauth2.Token) {
	if os.Getenv(noCacheEnv) != "" || t == nil || t.AccessToken == "" {
		return
	}
	b, err := json.Marshal(cachedToken{AccessToken: t.AccessToken, TokenType: t.TokenType, Expiry: t.Expiry}) // #nosec G117 -- serializing for the cache, not logging or a response
	if err != nil {
		return
	}
	if err := tokenKeyringSet(tokenKeychainService, key, string(b)); err == nil {
		invalidateFileCachedToken(key)
		return
	} else if !keychain.IsUnavailable(err) {
		return
	}
	storeFileCachedToken(key, b)
}

func storeFileCachedToken(key string, b []byte) {
	p, err := cachePath(key)
	if err != nil {
		return
	}
	dir := filepath.Dir(p)
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o700); err != nil || !trustedCacheDir(parent) {
		return
	}
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return
	}
	if !trustedCacheDir(dir) {
		return
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(p)+".tmp-*") // #nosec G304 -- dir is checked local cache storage
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	_ = os.Rename(tmpName, p)
}

func invalidateFileCachedToken(key string) {
	p, err := cachePath(key)
	if err != nil {
		return
	}
	_ = os.Remove(p)
}

// invalidateCachedToken drops the cached token for these credentials. Callers
// use it when the API rejects a token the cache believed was still valid --
// a revoked credential or a clock far enough out to defeat expirySkew.
func invalidateCachedToken(key string) {
	_ = tokenKeyringDelete(tokenKeychainService, key)
	invalidateFileCachedToken(key)
}

// InvalidateCachedToken drops the cache entry for one credential generation.
// It is intentionally best-effort: cache cleanup must not make logout fail.
func InvalidateCachedToken(tokenHost, clientID, clientSecret string) {
	host := strings.TrimPrefix(tokenHost, "https://")
	invalidateCachedToken(cacheKey(host, clientID, clientSecret))
}
