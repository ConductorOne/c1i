package tokensource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// useTempConfig redirects os.UserConfigDir at a temp dir so cache files never
// touch the real config directory. Skips where the redirect does not take
// (e.g. Windows keys off %AppData%, not HOME).
func useTempConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	p, err := cachePath("h", "c")
	if err != nil || !strings.HasPrefix(p, dir) {
		t.Skipf("os.UserConfigDir not redirected under temp on this platform (path=%q)", p)
	}
	return dir
}

func freshToken(d time.Duration) *oauth2.Token {
	return &oauth2.Token{AccessToken: "tok", TokenType: "Bearer", Expiry: time.Now().Add(d)}
}

func TestCacheKey_StableAndSeparates(t *testing.T) {
	a := cacheKey("host", "client")
	if a != cacheKey("host", "client") {
		t.Fatal("cacheKey not stable for identical inputs")
	}
	if len(a) != 64 {
		t.Errorf("key length = %d, want 64 hex chars", len(a))
	}
	// Host and client id must each separate the namespace.
	if cacheKey("host2", "client") == a {
		t.Error("cacheKey ignored host")
	}
	if cacheKey("host", "client2") == a {
		t.Error("cacheKey ignored client id")
	}
	// The NUL joiner must stop ("ab","c") colliding with ("a","bc").
	if cacheKey("ab", "c") == cacheKey("a", "bc") {
		t.Error("cacheKey joiner failed: (ab,c) collided with (a,bc)")
	}
}

func TestStoreLoadRoundTripAndPerms(t *testing.T) {
	useTempConfig(t)
	storeCachedToken("host", "client", freshToken(30*time.Minute))

	got := loadCachedToken("host", "client")
	if got == nil || got.AccessToken != "tok" || got.TokenType != "Bearer" {
		t.Fatalf("load after store = %+v, want the stored token", got)
	}

	p, _ := cachePath("host", "client")
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat cache file: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("cache file perm = %o, want 600", fi.Mode().Perm())
	}
	di, err := os.Stat(filepath.Dir(p))
	if err != nil {
		t.Fatalf("stat cache dir: %v", err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("cache dir perm = %o, want 700", di.Mode().Perm())
	}
	// No temp file left beside the final one.
	entries, _ := os.ReadDir(filepath.Dir(p))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestLoadMisses(t *testing.T) {
	useTempConfig(t)
	if loadCachedToken("host", "client") != nil {
		t.Error("expected miss when no file exists")
	}

	p, _ := cachePath("host", "client")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"corrupt json":      "not json {{{",
		"empty access":      `{"access_token":"","token_type":"Bearer","expiry":"2999-01-01T00:00:00Z"}`,
		"expired":           `{"access_token":"t","token_type":"Bearer","expiry":"2000-01-01T00:00:00Z"}`,
		"within skew (30s)": `{"access_token":"t","token_type":"Bearer","expiry":"` + time.Now().Add(30*time.Second).UTC().Format(time.RFC3339) + `"}`,
	}
	for name, body := range cases {
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := loadCachedToken("host", "client"); got != nil {
			t.Errorf("%s: load = %+v, want nil (miss)", name, got)
		}
	}
}

func TestStoreNoOps(t *testing.T) {
	dir := useTempConfig(t)
	storeCachedToken("host", "client", nil)
	storeCachedToken("host", "client", &oauth2.Token{AccessToken: ""})
	if entries, _ := os.ReadDir(filepath.Join(dir, "c1i", "tokens")); len(entries) != 0 {
		t.Errorf("nil/empty token should write nothing, found %d files", len(entries))
	}
}

func TestNoCacheEnvDisablesDisk(t *testing.T) {
	useTempConfig(t)
	t.Setenv(noCacheEnv, "1")
	storeCachedToken("host", "client", freshToken(30*time.Minute))
	p, _ := cachePath("host", "client")
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Error("store must write nothing when C1I_NO_TOKEN_CACHE is set")
	}
	// And a pre-existing file is ignored on load.
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"access_token":"t","token_type":"Bearer","expiry":"2999-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if loadCachedToken("host", "client") != nil {
		t.Error("load must ignore the cache when C1I_NO_TOKEN_CACHE is set")
	}
}

func TestInvalidateRemovesFile(t *testing.T) {
	useTempConfig(t)
	storeCachedToken("host", "client", freshToken(30*time.Minute))
	InvalidateCachedToken("host", "client")
	if loadCachedToken("host", "client") != nil {
		t.Error("token still present after Invalidate")
	}
	InvalidateCachedToken("host", "client") // absent: must not panic or error
}

// countingMint records how many times a token was minted, standing in for the
// real client_credentials exchange.
type countingMint struct {
	n   int
	tok *oauth2.Token
	err error
}

func (m *countingMint) Token() (*oauth2.Token, error) {
	m.n++
	if m.err != nil {
		return nil, m.err
	}
	return m.tok, nil
}

func TestCacheTokenSource_InMemoryReuse(t *testing.T) {
	useTempConfig(t)
	// Disable the disk cache so in-memory reuse is the ONLY thing that can hold
	// the mint count at 1 -- otherwise a disk hit would mask a broken in-memory
	// path.
	t.Setenv(noCacheEnv, "1")
	m := &countingMint{tok: freshToken(30 * time.Minute)}
	src := &cacheTokenSource{mint: m, host: "h", clientID: "c"}
	for i := 0; i < 5; i++ {
		if _, err := src.Token(); err != nil {
			t.Fatal(err)
		}
	}
	if m.n != 1 {
		t.Errorf("mints = %d, want 1 (in-memory reuse within a process)", m.n)
	}
}

func TestCacheTokenSource_NearExpiryNotReused(t *testing.T) {
	useTempConfig(t)
	// A token with less than expirySkew left must not be reused from any tier;
	// each call re-mints. Guards against reuse gating on oauth2's ~10s buffer
	// instead of the package's 60s skew.
	m := &countingMint{tok: freshToken(30 * time.Second)}
	src := &cacheTokenSource{mint: m, host: "h", clientID: "c"}
	if _, err := src.Token(); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Token(); err != nil {
		t.Fatal(err)
	}
	if m.n != 2 {
		t.Errorf("mints = %d, want 2 (a near-expiry token must not be reused)", m.n)
	}
}

func TestCacheTokenSource_DiskHitSkipsMint(t *testing.T) {
	useTempConfig(t)
	storeCachedToken("h", "c", freshToken(30*time.Minute))
	m := &countingMint{err: errors.New("mint must not be called on a disk hit")}
	src := &cacheTokenSource{mint: m, host: "h", clientID: "c"}
	if _, err := src.Token(); err != nil {
		t.Fatalf("Token: %v", err)
	}
	if m.n != 0 {
		t.Errorf("mints = %d, want 0 (served from disk)", m.n)
	}
}

func TestCacheTokenSource_MintOnMissWritesDisk(t *testing.T) {
	useTempConfig(t)
	m := &countingMint{tok: freshToken(30 * time.Minute)}
	src := &cacheTokenSource{mint: m, host: "h", clientID: "c"}
	if _, err := src.Token(); err != nil {
		t.Fatal(err)
	}
	// A second, independent process (fresh source) must read the disk, not mint.
	m2 := &countingMint{err: errors.New("must not mint; disk was written")}
	src2 := &cacheTokenSource{mint: m2, host: "h", clientID: "c"}
	if _, err := src2.Token(); err != nil {
		t.Fatalf("second source Token: %v", err)
	}
	if m.n != 1 || m2.n != 0 {
		t.Errorf("mints first=%d second=%d, want 1 and 0", m.n, m2.n)
	}
}

func TestCacheTokenSource_InvalidateForcesReMint(t *testing.T) {
	useTempConfig(t)
	m := &countingMint{tok: freshToken(30 * time.Minute)}
	src := &cacheTokenSource{mint: m, host: "h", clientID: "c"}
	if _, err := src.Token(); err != nil {
		t.Fatal(err)
	}
	src.Invalidate()
	p, _ := cachePath("h", "c")
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		t.Error("Invalidate must remove the on-disk token")
	}
	if _, err := src.Token(); err != nil {
		t.Fatal(err)
	}
	if m.n != 2 {
		t.Errorf("mints = %d, want 2 (re-mint after Invalidate)", m.n)
	}
}

func TestCachingSourceIsInvalidatorPlainIsNot(t *testing.T) {
	caching, err := NewCachingTokenSource(context.Background(), "client1", validSecret(t), "example.test")
	if err != nil {
		t.Fatalf("NewCachingTokenSource: %v", err)
	}
	if _, ok := caching.(Invalidator); !ok {
		t.Fatalf("caching source is %T, does not implement Invalidator; the client's self-heal wiring would silently disengage", caching)
	}
	// The plain source must NOT cache, so it must not be an Invalidator -- this
	// is what keeps `auth token` handing out a fresh, unpersisted bearer.
	plain, err := NewTokenSource(context.Background(), "client1", validSecret(t), "example.test")
	if err != nil {
		t.Fatalf("NewTokenSource: %v", err)
	}
	if _, ok := plain.(Invalidator); ok {
		t.Fatalf("plain source %T implements Invalidator; it must not cache", plain)
	}
}
