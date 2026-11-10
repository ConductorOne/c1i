package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/ConductorOne/c1i/internal/tokensource"
)

// useConfigFile points HOME at a temp dir holding ~/.c1i.yaml with body, so no
// test reads the real file.
func useConfigFile(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(home, ".c1i.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// ~/.c1i.yaml is the third source of the same settings the flag and env var
// carry, so it gets the same parsers. viper coerces garbage to false/0, so
// `dry_run: yes` used to send the mutation it was meant to preview.
func TestConfigFileValidation(t *testing.T) {
	const tokenCacheEnv = tokensource.NoCacheEnv
	cases := []struct {
		name     string
		yaml     string
		env      map[string]string
		args     []string
		wantExit int
		wantErr  string
		wantReqs int
	}{
		{name: "dry_run yes", yaml: "dry_run: yes\n", wantExit: exitUsage,
			wantErr: `invalid dry_run "yes" in ~/.c1i.yaml: must be true or false`},
		{name: "debug maybe", yaml: "debug: maybe\n", wantExit: exitUsage,
			wantErr: `invalid debug "maybe" in ~/.c1i.yaml: must be true or false`},
		{name: "max_retries abc", yaml: "max_retries: abc\n", wantExit: exitUsage,
			wantErr: `invalid max_retries "abc" in ~/.c1i.yaml: must be an integer`},
		{name: "max_retries 2.5", yaml: "max_retries: 2.5\n", wantExit: exitUsage,
			wantErr: `invalid max_retries "2.5" in ~/.c1i.yaml: must be an integer`},
		{name: "max_retries float", yaml: "max_retries: 1.5\n", wantExit: exitUsage,
			wantErr: `invalid max_retries "1.5" in ~/.c1i.yaml: must be an integer`},

		{name: "valid", yaml: "dry_run: false\nmax_retries: 2\ndebug: false\n", wantReqs: 1},
		{name: "empty means unset", yaml: "dry_run: \"\"\nmax_retries: \"\"\n", wantReqs: 1},
		{name: "null means unset", yaml: "dry_run:\n", wantReqs: 1},

		// Flag > env > config: a shadowed bad value is not checked.
		{name: "env shadows config", yaml: "dry_run: yes\n", env: map[string]string{"C1I_DRY_RUN": "false"}, wantReqs: 1},
		{name: "flag shadows config", yaml: "max_retries: abc\n", wantReqs: 1,
			args: append(append([]string{}, envTestArgs...), "--max-retries=1")},
		{name: "bad env still rejected", yaml: "dry_run: false\n", env: map[string]string{"C1I_DRY_RUN": "yes"},
			wantExit: exitUsage, wantErr: `invalid C1I_DRY_RUN "yes"`},

		{name: "error_format xml", yaml: "error_format: xml\n", wantExit: exitUsage,
			wantErr: `invalid error_format "xml" in ~/.c1i.yaml: want text or json`},
		{name: "error_format env xml", env: map[string]string{"C1I_ERROR_FORMAT": "xml"}, wantExit: exitUsage,
			wantErr: `invalid C1I_ERROR_FORMAT "xml": want text or json`},
		{name: "error_format flag xml", wantExit: exitUsage, args: append(append([]string{}, envTestArgs...), "--error-format", "xml"),
			wantErr: `invalid --error-format "xml": want text or json`},
		{name: "error_format valid", yaml: "error_format: JSON\n", wantReqs: 1},
		{name: "integral float max_retries", yaml: "max_retries: 1e6\n", wantReqs: 1},

		{name: "token cache env garbage", env: map[string]string{tokenCacheEnv: "maybe"}, wantExit: exitUsage,
			wantErr: `invalid C1I_NO_TOKEN_CACHE "maybe": must be true or false`},
		{name: "token cache env 0", env: map[string]string{tokenCacheEnv: "0"}, wantReqs: 1},
		{name: "token cache env true", env: map[string]string{tokenCacheEnv: "true"}, wantReqs: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnvFlags(t)
			t.Setenv(tokenCacheEnv, "")
			useConfigFile(t, tc.yaml)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			srv := newMutServer(t, fixedReply(200, "{}"))
			args := tc.args
			if args == nil {
				args = envTestArgs
			}
			res := runMutationCLI(t, srv.srv, 0, nil, args...)
			if got := len(srv.requests()); got != tc.wantReqs {
				t.Fatalf("requests sent = %d, want %d; exit %d err %v", got, tc.wantReqs, res.exit, res.err)
			}
			if res.exit != tc.wantExit {
				t.Fatalf("exit = %d (err %v), want %d", res.exit, res.err, tc.wantExit)
			}
			if tc.wantErr != "" && (res.err == nil || !strings.Contains(res.err.Error(), tc.wantErr)) {
				t.Fatalf("err = %v, want it to contain %q", res.err, tc.wantErr)
			}
		})
	}
}

// A valid config value reaches the client, not only the validator: a GET
// against a 503 makes exactly one attempt with `max_retries: 0`.
func TestConfigFileValueTakesEffect(t *testing.T) {
	clearEnvFlags(t)
	useConfigFile(t, "max_retries: 0\n")
	// A fresh viper: the global one carries search paths from earlier tests'
	// HOMEs (and the real one), which would be read before this file.
	resetViper := func() { viper.Reset(); bindGlobalEnvSettings() }
	resetViper()
	t.Cleanup(resetViper)
	srv := newMutServer(t, fixedReply(503, "{}"))
	res := runMutationCLI(t, srv.srv, retriesFromConfig, nil, "api", "--method", "GET", "--path", "/api/v1/things")
	if got := len(srv.requests()); got != 1 {
		t.Fatalf("requests sent = %d, want 1; exit %d err %v", got, res.exit, res.err)
	}
	if res.exit != exitServer {
		t.Errorf("exit = %d, want %d", res.exit, exitServer)
	}
}

// Exactly one credential env var set must fail the real client build with exit
// 2, not fall back to the keyring.
func TestOneSidedCredentialEnvExitsUsage(t *testing.T) {
	clearEnvFlags(t)
	useConfigFile(t, "")
	t.Setenv("C1I_CLIENT_ID", "id-only")
	t.Setenv("C1I_CLIENT_SECRET", "")
	res := runCLI(t, nil, nil, envTestArgs...)
	if res.exit != exitUsage {
		t.Fatalf("exit = %d (err %v), want %d", res.exit, res.err, exitUsage)
	}
	if res.err == nil || !strings.Contains(res.err.Error(), "only one of C1I_CLIENT_ID and C1I_CLIENT_SECRET is set") {
		t.Fatalf("err = %v, want the one-sided credential message", res.err)
	}
}

// login and logout stay runnable (the recovery path) but say why every other
// command exits 2.
func TestPartialEnvCredentialsNotice(t *testing.T) {
	for _, tc := range []struct {
		name, id, secret string
		want             bool
	}{
		{"id only", "id", "", true},
		{"secret only", "", "sec", true},
		{"both", "id", "sec", false},
		{"neither", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("C1I_CLIENT_ID", tc.id)
			t.Setenv("C1I_CLIENT_SECRET", tc.secret)
			var errBuf bytes.Buffer
			authLogoutCmd.SetErr(&errBuf)
			t.Cleanup(func() { authLogoutCmd.SetErr(nil) })
			warnPartialEnvCredentials(authLogoutCmd)
			got := strings.Contains(errBuf.String(), "only one of C1I_CLIENT_ID and C1I_CLIENT_SECRET is set; commands that need credentials will exit 2 until both or neither are set")
			if got != tc.want {
				t.Errorf("notice present = %v, want %v; stderr %q", got, tc.want, errBuf.String())
			}
		})
	}
}

// error_format decides how every error is rendered, so a bad value fails
// completion too (as before the other globals were validated); the typed
// settings stay skipped there so a stray C1I_DRY_RUN cannot break tab completion.
func TestLocalOnlyRequestsValidateErrorFormatOnly(t *testing.T) {
	for _, tc := range []struct {
		name     string
		env      map[string]string
		yaml     string
		args     []string
		wantExit int
	}{
		{"flag", nil, "", []string{"completion", "bash", "--error-format", "xml"}, exitUsage},
		{"env", map[string]string{"C1I_ERROR_FORMAT": "xml"}, "", []string{"completion", "bash"}, exitUsage},
		{"config", nil, "error_format: xml\n", []string{"completion", "bash"}, exitUsage},
		{"bad dry-run env still skipped", map[string]string{"C1I_DRY_RUN": "yes"}, "", []string{"completion", "bash"}, exitOK},
		{"bad max_retries config still skipped", nil, "max_retries: abc\n", []string{"completion", "bash"}, exitOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearEnvFlags(t)
			useConfigFile(t, tc.yaml)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			var out bytes.Buffer
			res := runCLI(t, nil, &out, tc.args...)
			if res.exit != tc.wantExit {
				t.Fatalf("exit = %d (err %v), want %d", res.exit, res.err, tc.wantExit)
			}
		})
	}
}

// auth login and auth logout are the recovery path for a half-set credential
// pair: they run (no credential load), and say why everything else exits 2.
func TestAuthRecoveryCommandsWarnOnPartialEnvCredentials(t *testing.T) {
	const want = "only one of C1I_CLIENT_ID and C1I_CLIENT_SECRET is set; commands that need credentials will exit 2 until both or neither are set"
	for _, tc := range []struct {
		name     string
		args     []string
		wantExit int
	}{
		{"logout", []string{"auth", "logout"}, exitOK},
		// Stops at the missing secret, after the warning and before any login.
		{"login", []string{"auth", "login", "--client-id", "x"}, exitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearEnvFlags(t)
			home := useConfigFile(t, "")
			t.Setenv("XDG_CONFIG_HOME", filepath.Dir(home))
			t.Setenv("C1I_CLIENT_ID", "id-only")
			t.Setenv("C1I_CLIENT_SECRET", "")
			res := runCLI(t, nil, nil, tc.args...)
			if res.exit != tc.wantExit {
				t.Fatalf("exit = %d (err %v), want %d", res.exit, res.err, tc.wantExit)
			}
			if !strings.Contains(res.stderr, want) {
				t.Errorf("stderr = %q, want it to contain %q", res.stderr, want)
			}
		})
	}
}
