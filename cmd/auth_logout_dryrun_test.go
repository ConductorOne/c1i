package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// plantCredential writes a 0600 credential file for host and returns its path.
func plantCredential(t *testing.T, host string) string {
	t.Helper()
	dir := filepath.Join(os.Getenv("HOME"), ".config", "c1i", "credentials")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "c1i_"+host+".json")
	if err := os.WriteFile(p, []byte(`{"client_id":"id","client_secret":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// auth logout deletes the stored credential, so a preview cannot be honored:
// --dry-run must refuse before anything is removed.
func TestAuthLogoutRejectsDryRun(t *testing.T) {
	const host = "tenant.example.invalid"
	for _, tc := range []struct {
		name string
		args []string
		env  bool
	}{
		{"flag", []string{"--dry-run"}, false},
		{"env", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", "")
			t.Setenv("C1I_CLIENT_ID", "")
			t.Setenv("C1I_CLIENT_SECRET", "")
			t.Setenv("C1I_DRY_RUN", "")
			if tc.env {
				t.Setenv("C1I_DRY_RUN", "1")
			}
			resetCmdFlags(t, rootCmd)
			clearViperOverrides(t, "dry_run")
			p := plantCredential(t, host)

			err := runRootWithArgs(t, append([]string{"auth", "logout", "--url", "https://" + host}, tc.args...))
			if got := exitCode(err); got != exitUsage {
				t.Fatalf("exitCode = %d, want %d; err: %v", got, exitUsage, err)
			}
			if !strings.Contains(err.Error(), "--dry-run is unsupported for auth logout") {
				t.Errorf("error = %q, want the dry-run explanation", err)
			}
			if _, statErr := os.Stat(p); statErr != nil {
				t.Errorf("credential file was removed by --dry-run: %v", statErr)
			}
		})
	}
}

// The guard must not change a real logout.
func TestAuthLogoutStillRemovesCredential(t *testing.T) {
	const host = "tenant.example.invalid"
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("C1I_CLIENT_ID", "")
	t.Setenv("C1I_CLIENT_SECRET", "")
	resetCmdFlags(t, rootCmd)
	p := plantCredential(t, host)

	if err := runRootWithArgs(t, []string{"auth", "logout", "--url", "https://" + host}); err != nil {
		t.Fatalf("logout failed: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("credential file still present after logout: %v", err)
	}
}
