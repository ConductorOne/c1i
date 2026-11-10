package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// envTestArgs is a mutating `api` call: a preview shows up as stdout's
// "dryRun": true with no server request, a send as one request.
var envTestArgs = []string{"api", "--method", "POST", "--path", "/api/v1/things", "--body", "{}"}

// clearEnvFlags isolates every global env-backed setting; "" is unset for both
// viper (allowEmptyEnv is off) and validateGlobalSettings.
func clearEnvFlags(t *testing.T) {
	t.Helper()
	t.Setenv("C1I_URL", mutTestURL)
	for _, k := range []string{"C1I_DRY_RUN", "C1I_DEBUG", "C1I_MAX_RETRIES", "C1I_ERROR_FORMAT"} {
		t.Setenv(k, "")
	}
	clearViperOverrides(t, "url", "fields", "dry_run", "debug", "max_retries", "error_format")
}

// clearViperOverrides drops any viper.Set override for keys and leaves the same
// "no override" state behind, both before and after the test. A viper override
// outranks a bound env var, and several tests restore theirs by Set-ing the
// previous effective value, which keeps masking the env var for the rest of the
// process. nil is viper's own representation of "no override"; capturing and
// restoring a raw value would put the mask back.
func clearViperOverrides(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		viper.Set(k, nil)
	}
	t.Cleanup(func() {
		for _, k := range keys {
			viper.Set(k, nil)
		}
	})
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
		// clamps it to 0); parity with the flag is the contract. pflag's Int
		// parses with base 0, so hex/octal are valid on the flag and here.
		{name: "max-retries negative", envVar: "C1I_MAX_RETRIES", value: "-1", wantReqs: 1},
		{name: "max-retries zero", envVar: "C1I_MAX_RETRIES", value: "0", wantReqs: 1},
		{name: "max-retries hex", envVar: "C1I_MAX_RETRIES", value: "0x2", wantReqs: 1},
		{name: "max-retries octal", envVar: "C1I_MAX_RETRIES", value: "010", wantReqs: 1},
		{name: "max-retries negative hex", envVar: "C1I_MAX_RETRIES", value: "-0x1", wantReqs: 1},

		// An explicitly-set flag wins over a bad env value.
		{name: "flag dry-run overrides bad env", envVar: "C1I_DRY_RUN", value: "yes",
			args: []string{"api", "--method", "POST", "--path", "/api/v1/things", "--body", "{}", "--dry-run=false"}, wantReqs: 1},
		{name: "flag max-retries overrides bad env", envVar: "C1I_MAX_RETRIES", value: "abc",
			args: []string{"api", "--method", "POST", "--path", "/api/v1/things", "--body", "{}", "--max-retries=2"}, wantReqs: 1},
		{name: "flag debug overrides bad env", envVar: "C1I_DEBUG", value: "yes",
			args: []string{"api", "--method", "POST", "--path", "/api/v1/things", "--body", "{}", "--debug=false"}, wantReqs: 1},

		{name: "error-format invalid", envVar: "C1I_ERROR_FORMAT", value: "jsonn", wantExit: exitUsage,
			wantErr: `invalid C1I_ERROR_FORMAT "jsonn": want text or json`},
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

// TestGlobalEnvSettingsMatchFlagTypes keeps the one table honest: a setting
// whose flag is typed must carry the parser validateGlobalSettings applies to
// its flag, env var and ~/.c1i.yaml key. A new typed global that skips the table would be bound nowhere and
// validated nowhere. An env-only entry (no key, no flag) must have a parser.
func TestGlobalEnvSettingsMatchFlagTypes(t *testing.T) {
	if len(globalEnvSettings) < 7 {
		t.Fatalf("only %d global env settings; the table lost an entry", len(globalEnvSettings))
	}
	tableKeys := map[string]bool{}
	for _, s := range globalEnvSettings {
		if s.flag == "" {
			if s.key != "" || s.parse == nil {
				t.Errorf("%s: an env-only setting needs no key and a parser", s.env)
			}
			continue
		}
		tableKeys[s.flag] = true
		f := rootCmd.PersistentFlags().Lookup(s.flag)
		if f == nil {
			t.Fatalf("no persistent flag --%s for %s", s.flag, s.env)
		}
		if s.key != strings.ReplaceAll(s.flag, "-", "_") {
			t.Errorf("%s: config key %q is not --%s with underscores", s.env, s.key, s.flag)
		}
		if f.Value.Type() != "string" && s.parse == nil {
			t.Errorf("%s (--%s, type %s) has no parser; a typed value needs one", s.env, s.flag, f.Value.Type())
		}
	}
	// Every global flag must be in the table, or its env and config-file
	// values skip binding and validation.
	rootCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if !tableKeys[f.Name] {
			t.Errorf("--%s is missing from globalEnvSettings", f.Name)
		}
	})
}

// TestLocalOnlyRequestsIgnoreBadEnvFlags: completion never touches the network,
// so a bad global env value must not fail it.
func TestLocalOnlyRequestsIgnoreBadEnvFlags(t *testing.T) {
	completion := &cobra.Command{Use: "completion"}
	shell := &cobra.Command{Use: "bash"}
	completion.AddCommand(shell)
	for _, tc := range []struct {
		name string
		cmd  *cobra.Command
		want bool
	}{
		{"completion group", completion, true},
		{"completion shell", shell, true},
		{"__complete", &cobra.Command{Use: cobra.ShellCompRequestCmd}, true},
		{"api", &cobra.Command{Use: "api"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isLocalOnlyRequest(tc.cmd); got != tc.want {
				t.Errorf("isLocalOnlyRequest(%s) = %v, want %v", tc.cmd.Name(), got, tc.want)
			}
		})
	}

	// Drive the real pre-run with a bad value: it must pass for a local-only
	// command and fail for one that would send a request. Executing cobra's
	// __complete command directly would leave it in rootCmd for every later
	// test (cobra removes it only for a root with no other subcommand), so this
	// calls the hook instead.
	clearEnvFlags(t)
	t.Setenv("C1I_DRY_RUN", "yes")
	complete := &cobra.Command{Use: cobra.ShellCompRequestCmd}
	api := &cobra.Command{Use: "api"}
	for _, c := range []*cobra.Command{completion, shell, complete, api} {
		c.SetContext(context.Background())
	}
	if err := rootCmd.PersistentPreRunE(shell, nil); err != nil {
		t.Errorf("pre-run on completion %s: %v, want no error", shell.Name(), err)
	}
	if err := rootCmd.PersistentPreRunE(complete, nil); err != nil {
		t.Errorf("pre-run on %s: %v, want no error", cobra.ShellCompRequestCmd, err)
	}
	if err := rootCmd.PersistentPreRunE(api, nil); err == nil ||
		!strings.Contains(err.Error(), `invalid C1I_DRY_RUN "yes"`) {
		t.Errorf("pre-run on api: err = %v, want the env flag error", err)
	}
}
