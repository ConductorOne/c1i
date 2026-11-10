package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// envTestArgs is a mutating `api` call: a preview shows up as stdout's
// "dryRun": true with no server request, a send as one request.
var envTestArgs = []string{"api", "--method", "POST", "--path", "/api/v1/things", "--body", "{}"}

// clearEnvFlags isolates every global env-backed setting; "" is unset for both
// viper (allowEmptyEnv is off) and validateEnvFlags.
//
// viper.Set outranks a bound env var, and other tests leave a Set("dry_run", …)
// behind (their cleanup restores the old value by Set-ing it), which would mask
// the env var in a later test. Set(key, nil) is viper's "no override" state, so
// this restores the clean precedence the process would have.
func clearEnvFlags(t *testing.T) {
	t.Helper()
	t.Setenv("C1I_URL", mutTestURL)
	for _, k := range []string{"C1I_DRY_RUN", "C1I_DEBUG", "C1I_MAX_RETRIES", "C1I_ERROR_FORMAT"} {
		t.Setenv(k, "")
	}
	for _, k := range []string{"dry_run", "debug", "max_retries", "error_format"} {
		viper.Set(k, nil)
	}
}

// A non-empty, unparsable env value is a usage error before any request; a
// valid one keeps its documented meaning (C1I_DRY_RUN=true/1 still preview).
func TestEnvFlagValidation(t *testing.T) {
	cases := []struct {
		name       string
		envVar     string
		value      string
		args       []string
		wantExit   int
		wantErr    string
		wantReqs   int
		wantDryRun bool
	}{
		// Unparsable: exit 2, nothing sent, message names the variable.
		{name: "dry-run yes", envVar: "C1I_DRY_RUN", value: "yes", wantExit: exitUsage,
			wantErr: `invalid C1I_DRY_RUN "yes": must be true or false`},
		{name: "dry-run on", envVar: "C1I_DRY_RUN", value: "on", wantExit: exitUsage,
			wantErr: `invalid C1I_DRY_RUN "on": must be true or false`},
		{name: "dry-run bogus", envVar: "C1I_DRY_RUN", value: "bogus", wantExit: exitUsage,
			wantErr: `invalid C1I_DRY_RUN "bogus": must be true or false`},
		{name: "debug yes", envVar: "C1I_DEBUG", value: "yes", wantExit: exitUsage,
			wantErr: `invalid C1I_DEBUG "yes": must be true or false`},
		{name: "max-retries abc", envVar: "C1I_MAX_RETRIES", value: "abc", wantExit: exitUsage,
			wantErr: `invalid C1I_MAX_RETRIES "abc": must be an integer`},
		{name: "max-retries float", envVar: "C1I_MAX_RETRIES", value: "1.5", wantExit: exitUsage,
			wantErr: `invalid C1I_MAX_RETRIES "1.5": must be an integer`},

		// Parsable booleans behave exactly as before; true means preview.
		{name: "dry-run true", envVar: "C1I_DRY_RUN", value: "true", wantDryRun: true},
		{name: "dry-run 1", envVar: "C1I_DRY_RUN", value: "1", wantDryRun: true},
		{name: "dry-run T", envVar: "C1I_DRY_RUN", value: "T", wantDryRun: true},
		{name: "dry-run false", envVar: "C1I_DRY_RUN", value: "false", wantReqs: 1},
		{name: "dry-run 0", envVar: "C1I_DRY_RUN", value: "0", wantReqs: 1},
		{name: "dry-run empty", envVar: "C1I_DRY_RUN", value: "", wantReqs: 1},

		{name: "debug true", envVar: "C1I_DEBUG", value: "true", wantReqs: 1},
		{name: "debug false", envVar: "C1I_DEBUG", value: "false", wantReqs: 1},

		// Negative retries parse like `--max-retries -1` does (transport
		// clamps it to 0); parity with the flag is the contract.
		{name: "max-retries negative", envVar: "C1I_MAX_RETRIES", value: "-1", wantReqs: 1},
		{name: "max-retries zero", envVar: "C1I_MAX_RETRIES", value: "0", wantReqs: 1},

		// An explicitly-set flag wins over a bad env value.
		{name: "flag dry-run overrides bad env", envVar: "C1I_DRY_RUN", value: "yes",
			args: []string{"api", "--method", "POST", "--path", "/api/v1/things", "--body", "{}", "--dry-run=false"}, wantReqs: 1},
		{name: "flag max-retries overrides bad env", envVar: "C1I_MAX_RETRIES", value: "abc",
			args: []string{"api", "--method", "POST", "--path", "/api/v1/things", "--body", "{}", "--max-retries=2"}, wantReqs: 1},
		{name: "flag debug overrides bad env", envVar: "C1I_DEBUG", value: "yes",
			args: []string{"api", "--method", "POST", "--path", "/api/v1/things", "--body", "{}", "--debug=false"}, wantReqs: 1},

		// C1I_ERROR_FORMAT already reached a usage error through
		// validateErrorFormat; this pins that it still does.
		{name: "error-format invalid", envVar: "C1I_ERROR_FORMAT", value: "jsonn", wantExit: exitUsage,
			wantErr: `invalid --error-format "jsonn"`},
		{name: "error-format valid", envVar: "C1I_ERROR_FORMAT", value: "json", wantReqs: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnvFlags(t)
			t.Setenv(tc.envVar, tc.value)
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
				t.Fatalf("exit = %d (err %v), want %d; stderr %q", res.exit, res.err, tc.wantExit, res.stderr)
			}
			if tc.wantErr != "" {
				if res.err == nil || !strings.Contains(res.err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", res.err, tc.wantErr)
				}
				if !strings.Contains(res.stderr, tc.wantErr) {
					t.Errorf("stderr = %q, want it to contain %q", res.stderr, tc.wantErr)
				}
			}
			if got := len(srv.requests()); got != tc.wantReqs {
				t.Errorf("requests sent = %d, want %d", got, tc.wantReqs)
			}
			if got := strings.Contains(res.stdout, `"dryRun": true`); got != tc.wantDryRun {
				t.Errorf("stdout dry-run preview = %v, want %v; stdout %q", got, tc.wantDryRun, res.stdout)
			}
		})
	}
}
