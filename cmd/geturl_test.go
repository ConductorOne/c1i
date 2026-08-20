package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// resetRootURLFlag restores rootCmd's persistent --url flag to unset after a
// test drives it, so state can't leak into another test the way
// resetMcpServersGetCmdAppIDFlag (cmd/root_test.go) already does for
// --app-id.
func resetRootURLFlag(t *testing.T) {
	t.Helper()
	f := rootCmd.PersistentFlags().Lookup("url")
	origValue := f.Value.String()
	origChanged := f.Changed
	t.Cleanup(func() {
		_ = f.Value.Set(origValue)
		f.Changed = origChanged
	})
}

// runRootWithArgs executes rootCmd with args and returns the resulting
// error, mirroring the pattern already established in cmd/root_test.go
// (TestNonUTF8PositionalArgIsUsageError et al).
func runRootWithArgs(t *testing.T, args []string) error {
	t.Helper()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(args)
	return rootCmd.ExecuteContext(context.Background())
}

// --- A bare token is refused, one test per source ---

func TestGetBaseURLBareTokenFromFlagNamesFlag(t *testing.T) {
	resetRootURLFlag(t)
	t.Setenv("C1I_URL", "")

	err := runRootWithArgs(t, []string{"users", "get", "someid", "--url", "acme"})
	if err == nil {
		t.Fatal("expected an error for a bare --url, got nil")
	}
	if !strings.Contains(err.Error(), "--url flag") {
		t.Errorf("error = %q, want it to name the --url flag", err.Error())
	}
	if !strings.Contains(err.Error(), "is not a full host") {
		t.Errorf("error = %q, want the actionable ParseURL message", err.Error())
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode = %d, want %d", got, want)
	}
}

func TestGetBaseURLBareTokenFromEnvNamesEnvVar(t *testing.T) {
	resetRootURLFlag(t)
	t.Setenv("C1I_URL", "acme")

	err := runRootWithArgs(t, []string{"users", "get", "someid"})
	if err == nil {
		t.Fatal("expected an error for a bare C1I_URL, got nil")
	}
	if !strings.Contains(err.Error(), "C1I_URL environment variable") {
		t.Errorf("error = %q, want it to name the C1I_URL environment variable", err.Error())
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode = %d, want %d", got, want)
	}
}

// TestGetBaseURLBareTokenFromConfigNamesConfigFile simulates a value that
// arrived via ~/.c1i.yaml by setting it directly in viper -- the same key
// GetBaseURLWithSource's config branch reads (viper.GetString("url")) once
// neither the flag nor C1I_URL is set, so it exercises the exact branch a
// real config file populates without touching the developer's real
// ~/.c1i.yaml. The live-verification section separately drives this through
// an actual temporary config file end to end.
func TestGetBaseURLBareTokenFromConfigNamesConfigFile(t *testing.T) {
	resetRootURLFlag(t)
	t.Setenv("C1I_URL", "")
	orig := viper.GetString("url")
	viper.Set("url", "acme")
	t.Cleanup(func() { viper.Set("url", orig) })

	err := runRootWithArgs(t, []string{"users", "get", "someid"})
	if err == nil {
		t.Fatal("expected an error for a bare url in the config source, got nil")
	}
	if !strings.Contains(err.Error(), "~/.c1i.yaml") {
		t.Errorf("error = %q, want it to name ~/.c1i.yaml", err.Error())
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode = %d, want %d", got, want)
	}
}

func TestPromptForURLBareTokenNamesInteractivePrompt(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	var out bytes.Buffer
	cmd.SetOut(&out)

	_, err := promptForURL(cmd, strings.NewReader("acme\n"))
	if err == nil {
		t.Fatal("expected an error for a bare name typed at the prompt, got nil")
	}
	if !strings.Contains(err.Error(), "interactive login prompt") {
		t.Errorf("error = %q, want it to name the interactive login prompt", err.Error())
	}
	if !strings.Contains(err.Error(), "is not a full host") {
		t.Errorf("error = %q, want the actionable ParseURL message", err.Error())
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode = %d, want %d", got, want)
	}
}

// TestPromptForURLValidInputUnaffected is the regression guard: a full host
// typed at the prompt must still work.
func TestPromptForURLValidInputUnaffected(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	var out bytes.Buffer
	cmd.SetOut(&out)

	got, err := promptForURL(cmd, strings.NewReader("acme.conductor.one\n"))
	if err != nil {
		t.Fatalf("promptForURL error = %v, want nil", err)
	}
	if want := "https://acme.conductor.one"; got != want {
		t.Errorf("promptForURL = %q, want %q", got, want)
	}
}

// TestAuthLoginPromptDoesNotAdvertiseShortcut pins item 7: the prompt used
// to read "...or mycompany for conductor.one", advertising the now-retired
// shortcut. It must be gone.
func TestAuthLoginPromptDoesNotAdvertiseShortcut(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	var out bytes.Buffer
	cmd.SetOut(&out)
	_, _ = promptForURL(cmd, strings.NewReader("acme.conductor.one\n"))

	if strings.Contains(out.String(), "for conductor.one") {
		t.Errorf("prompt = %q, still advertises the retired bare short-name shortcut", out.String())
	}
	if !strings.Contains(out.String(), "c1eu.ai") {
		t.Errorf("prompt = %q, want it to mention the c1eu.ai domain family", out.String())
	}
}

// --- localhost is refused; the working local-dev escape hatches are not ---

func TestGetBaseURLLocalhostBareIsUsageError(t *testing.T) {
	resetRootURLFlag(t)
	t.Setenv("C1I_URL", "")

	err := runRootWithArgs(t, []string{"users", "get", "someid", "--url", "localhost"})
	if err == nil {
		t.Fatal("expected an error for --url localhost, got nil")
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode = %d, want %d", got, want)
	}
	if !strings.Contains(err.Error(), "http://localhost:8080") {
		t.Errorf("error = %q, want it to suggest an explicit scheme", err.Error())
	}
}

func TestGetBaseURLLocalhostWithPortBareIsUsageError(t *testing.T) {
	resetRootURLFlag(t)
	t.Setenv("C1I_URL", "")

	err := runRootWithArgs(t, []string{"users", "get", "someid", "--url", "localhost:8080"})
	if err == nil {
		t.Fatal("expected an error for --url localhost:8080, got nil")
	}
	if got, want := exitCode(err), exitUsage; got != want {
		t.Errorf("exitCode = %d, want %d", got, want)
	}
}

// TestGetBaseURLLocalhostWithSchemeUnaffected: an explicit scheme must not
// be rejected -- the command should get past URL parsing entirely and fail
// only for lack of credentials against the (fake) local target.
func TestGetBaseURLLocalhostWithSchemeUnaffected(t *testing.T) {
	resetRootURLFlag(t)
	t.Setenv("C1I_URL", "")
	t.Setenv("C1I_CLIENT_ID", "")
	t.Setenv("C1I_CLIENT_SECRET", "")

	err := runRootWithArgs(t, []string{"users", "get", "someid", "--url", "http://localhost:8080"})
	if err == nil {
		t.Fatal("expected an error (no credentials configured), got nil")
	}
	if strings.Contains(err.Error(), "is not a full host") {
		t.Errorf("http://localhost:8080 must not be rejected by the bare-token check, got: %v", err)
	}
}

// TestGetBaseURLFullHostsUnaffected is the regression guard for both tenant
// domain families: raw, with scheme, and mixed case must all still reach
// past URL parsing (and fail only for lack of credentials).
func TestGetBaseURLFullHostsUnaffected(t *testing.T) {
	cases := []string{
		"leet.conductor.one",
		"https://leet.conductor.one",
		"LEET.CONDUCTOR.ONE",
		"acme.c1eu.ai",
		"https://acme.c1eu.ai",
		"ACME.C1EU.AI",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			resetRootURLFlag(t)
			t.Setenv("C1I_URL", "")
			t.Setenv("C1I_CLIENT_ID", "")
			t.Setenv("C1I_CLIENT_SECRET", "")

			err := runRootWithArgs(t, []string{"users", "get", "someid", "--url", in})
			if err == nil {
				t.Fatal("expected an error (no credentials configured), got nil")
			}
			if strings.Contains(err.Error(), "is not a full host") {
				t.Errorf("%q must not be rejected by the bare-token check, got: %v", in, err)
			}
		})
	}
}

// A real temporary ~/.c1i.yaml is exercised end to end in the live
// verification (a fresh subprocess, so it isn't subject to viper's
// process-global config-file path caching -- see viper's getConfigFile,
// which caches whatever config file findConfigFile locates the FIRST time
// any test in this binary reads a real config, making a second, differently
// -HOME'd read within the same process unreliable). Not attempted here for
// that reason; TestGetBaseURLBareTokenFromConfigNamesConfigFile covers the
// same GetBaseURLWithSource code path via viper.Set instead.
