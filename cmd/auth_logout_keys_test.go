package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/config"
	"github.com/ConductorOne/c1i/internal/keychain"
)

// TestLogoutRemovesEveryKeyTheTenantLivesUnder: a credential kept under an
// older key (explicit :443, legacy short name) must not survive logout and
// serve the next command through the load fallback.
func TestLogoutRemovesEveryKeyTheTenantLivesUnder(t *testing.T) {
	t.Setenv("C1I_CLIENT_ID", "")
	t.Setenv("C1I_CLIENT_SECRET", "")
	for _, host := range []string{"acme.example", "acme.conductor.one"} {
		t.Run(host, func(t *testing.T) {
			base := "https://" + host
			keys := config.KeychainServices(base)
			if len(keys) < 2 {
				t.Fatalf("KeychainServices(%q) = %v, want the current key and older ones", base, keys)
			}
			for _, k := range keys {
				if _, err := keychain.Store(k, "id", "secret"); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("C1I_URL", base)
			r := runCLI(t, nil, nil, "auth", "logout")
			if r.err != nil || r.stdout != "Removed credentials for "+base+".\n" {
				t.Fatalf("logout: exit %d err %v stdout %q", r.exit, r.err, r.stdout)
			}
			for _, k := range keys {
				if _, _, _, err := keychain.Load(k); err == nil {
					t.Errorf("%s survived logout", k)
				}
			}
			if r := runCLI(t, nil, nil, "users", "get", "x"); r.exit != exitAuth {
				t.Errorf("next command: exit %d (%v), want %d", r.exit, r.err, exitAuth)
			}
		})
	}
}

// A failed delete on one key must not stop the others: a surviving older copy
// would log the next command back in. The first error is still returned.
func TestLogoutAttemptsEveryKeyAfterAFailure(t *testing.T) {
	t.Setenv("C1I_CLIENT_ID", "")
	t.Setenv("C1I_CLIENT_SECRET", "")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("AppData", dir)
	base := "https://acme.example"
	keys := config.KeychainServices(base)
	// A non-empty directory where the current key's credentials file belongs
	// makes its file delete fail.
	p, err := keychain.FilePath(keys[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(p, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	// The current key holds a credential too, so loading does not migrate the
	// older copy away before logout runs.
	for _, k := range keys[:2] {
		if _, err := keychain.Store(k, "id", "secret"); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("C1I_URL", base)
	r := runCLI(t, nil, nil, "auth", "logout")
	if r.err == nil || !strings.HasPrefix(r.err.Error(), "logout: ") {
		t.Fatalf("err = %v, want the first delete error prefixed \"logout: \"", r.err)
	}
	if _, _, _, err := keychain.Load(keys[1]); err == nil {
		t.Errorf("%s survived a logout whose first delete failed", keys[1])
	}
}

// status and the roles prompt read the same keys loading does, so a credential
// left under the :443 name counts.
func TestStatusAndRolesSeeAnOlderKeyCredential(t *testing.T) {
	t.Setenv("C1I_CLIENT_ID", "")
	t.Setenv("C1I_CLIENT_SECRET", "")
	base := "https://acme.example"
	old := config.ExplicitPortKeychainService(base)
	if _, err := keychain.Store(old, "cid-443", "secret"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = keychain.Delete(old) })
	if got := storedClientID(base); got != "cid-443" {
		t.Errorf("storedClientID = %q, want the :443 credential's id", got)
	}
	srv := newMutServer(t, fixedReply(200, `{}`))
	t.Setenv("C1I_URL", base)
	r := runMutationCLI(t, srv.srv, 0, nil, "auth", "status")
	want := "Authenticated to " + base + ".\nSource: " + keyringName() + "\n"
	if r.err != nil || r.stdout != want {
		t.Errorf("status: err %v stdout %q, want %q", r.err, r.stdout, want)
	}
}
