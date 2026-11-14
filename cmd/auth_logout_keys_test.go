package cmd

import (
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
