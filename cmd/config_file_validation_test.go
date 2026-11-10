package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/tokensource"
)

// useConfigFile points HOME at a temp dir holding ~/.c1i.yaml with body, so no
// test reads the real file.
func useConfigFile(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.WriteFile(filepath.Join(home, ".c1i.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
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
