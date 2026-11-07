package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// dryRunPinSkips are leaves Rule C does not run: no C1 REST mutation, and
// running them would touch the network, the keychain, or a terminal.
var dryRunPinSkips = []string{"c1i auth", "c1i docs", "c1i upgrade", "c1i mcp gateway", "c1i version"}

// dryRunStubs supplies inputs for a mutating leaf whose generated stub (one
// value per <positional> and required flag) does not reach its preview. A
// "--flag=@<json>" value becomes a temp file holding <json>.
var dryRunStubs = map[string][]string{
	"c1i access-profiles bundle-automation create":        {"x1", "--body-file=@{}"},
	"c1i access-profiles bundle-automation set":           {"x1", "--body-file=@{}"},
	"c1i access-profiles requestable-entitlements add":    {"x1", "--app-id=x1", "--entitlement-id=x1"},
	"c1i access-profiles requestable-entitlements remove": {"x1", "--app-id=x1", "--entitlement-id=x1"},
	"c1i access-profiles requestable-entitlements set":    {"x1", `--refs-file=@[{"appId":"x1","id":"x1"}]`},
	"c1i access-profiles visibility-entitlements add":     {"x1", "--app-id=x1", "--entitlement-id=x1"},
	"c1i access-profiles visibility-entitlements remove":  {"x1", "--app-id=x1", "--entitlement-id=x1"},
	"c1i access-profiles update":                          {"x1", "--display-name=x1"},
	"c1i access-reviews create":                           {`--body-file=@{"ownerIds":["u1"]}`},
	"c1i access-reviews update":                           {"x1", "--body-file=@{}", "--update-mask=displayName"},
	"c1i apps update":                                     {"x1", "--description=d"},
	"c1i automations update":                              {"x1", "--description=d"},
	"c1i entitlements update":                             {"x1", "--app-id=x1", "--description=d"},
	"c1i findings assignee":                               {"x1", "--body-file=@{}"},
	"c1i findings bulk-create-tasks":                      {"--body-file=@{}"},
	"c1i findings bulk-state":                             {"--body-file=@{}"},
	"c1i findings create":                                 {"--body-file=@{}"},
	"c1i findings routing-rules create":                   {"--body-file=@{}"},
	"c1i findings routing-rules update":                   {"x1", "--body-file=@{}"},
	"c1i findings settings update":                        {"--body-file=@{}"},
	"c1i findings state":                                  {"x1", "--body-file=@{}"},
	"c1i findings transformation-rules create":            {"--body-file=@{}"},
	"c1i findings transformation-rules update":            {"x1", "--body-file=@{}"},
	"c1i mcp classifiers bindings create":                 {"--body-file=@{}"},
	"c1i mcp classifiers create":                          {"--body-file=@{}"},
	"c1i mcp classifiers policy update":                   {"--body-file=@{}", "--update-mask=x"},
	"c1i mcp classifiers tool-gates create":               {"--body-file=@{}"},
	"c1i mcp classifiers tool-gates update":               {"x1", "--body-file=@{}", "--update-mask=x"},
	"c1i mcp classifiers update":                          {"x1", `--body-file=@{"displayName":"x1"}`, "--update-mask=x"},
	"c1i mcp servers register":                            {"--app-id=x1", "--type=external", "--display-name=x1", "--server-url=https://mcp.example.invalid"},
	"c1i mcp servers update":                              {"x1", "--app-id=x1", "--display-name=x1"},
	"c1i mcp servers update-credentials":                  {"x1", "--app-id=x1", "--type=external", "--server-url=https://mcp.example.invalid"},
	"c1i mcp toolsets update":                             {"x1", "--app-id=x1", "--connector-id=x1", "--display-name=x1"},
	"c1i policies create":                                 {"--display-name=x1", "--policy-type=grant", "--allow-deny-all"},
	"c1i policies update":                                 {"x1", "--display-name=x1"},
	"c1i requests create grant":                           {"--app-id=x1", "--entitlement-id=x1", "--user-id=x1"},
	"c1i requests create revoke":                          {"--app-id=x1", "--entitlement-id=x1", "--user-id=x1"},
	"c1i role-mining access-profiles create":              {"--body-file=@{}"},
	"c1i role-mining config update":                       {"--body-file=@{}"},
	"c1i role-mining custom-analysis evaluate":            {"x1", "--body-file=@{}"},
	"c1i role-mining custom-analysis trigger":             {"--body-file=@{}"},
	"c1i role-mining suggestions state":                   {"x1", "--body-file=@{}"},
	"c1i service-principals bindings add":                 {"--service-principal-id=x1", "--function-id=x1"},
	"c1i service-principals bindings delete":              {"--service-principal-id=x1", "--function-id=x1"},
	"c1i service-principals credentials update":           {"x1", "--service-principal-id=x1", "--display-name=x1"},
	"c1i service-principals update":                       {"x1", "--display-name=x1"},
	"c1i tasks approve":                                   {"x1", "--policy-step-id=p1"},
	"c1i tasks reassign":                                  {"x1", "--to-user-id=x1", "--policy-step-id=p1"},
	"c1i tasks skip-step":                                 {"x1", "--policy-step-id=p1"},
	"c1i tasks update-grant-duration":                     {"x1", "--duration=8h"},
	"c1i webhooks create":                                 {"--destination-url=https://hooks.example.invalid/x", "--display-name=x1"},
	"c1i webhooks update":                                 {"x1", "--display-name=x1"},
}

// stubArgs builds a leaf's arguments: one per required positional in Use and
// a typed value per required flag.
func stubArgs(c *cobra.Command) []string {
	if s, ok := dryRunStubs[c.CommandPath()]; ok {
		return s
	}
	var args []string
	use := regexp.MustCompile(`\[[^\]]*\]`).ReplaceAllString(c.Use, "")
	for range regexp.MustCompile(`<[^>]+>`).FindAllString(use, -1) {
		args = append(args, "x1")
	}
	c.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Annotations[cobra.BashCompOneRequiredFlag] == nil {
			return
		}
		v := "x1"
		switch f.Value.Type() {
		case "bool":
			v = "true"
		case "int", "int64", "int32":
			v = "1"
		case "duration":
			v = "1s"
		}
		args = append(args, "--"+f.Name+"="+v)
	})
	return args
}

// TestDryRunPreviewSetMatchesRuleB is Rule C: under C1I_DRY_RUN every leaf a
// closure reaches sendMutation* through prints the JSON preview, and no other
// leaf does. A pending leaf must still preview in the old form, which pins
// its stub for Phase B.
func TestDryRunPreviewSetMatchesRuleB(t *testing.T) {
	ix := loadSrcIndex(t, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	stub := func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client(), client.WithMaxRetries(0)), nil
	}
	origs := []*func(*cobra.Command, string) (*client.Client, error){&newClient, &newListClient, &newPoliciesClient, &newServicePrincipalsClient, &newGrantClient, &newRevokeClient, &newWhoamiClient}
	saved := make([]func(*cobra.Command, string) (*client.Client, error), len(origs))
	for i, p := range origs {
		saved[i] = *p
		*p = stub
	}
	t.Cleanup(func() {
		for i, p := range origs {
			*p = saved[i]
		}
	})
	useTempHome(t)
	withDryRun(t)
	t.Setenv("C1I_URL", mutTestURL)

	for _, l := range leafCommands(t, ix) {
		skip := false
		for _, s := range dryRunPinSkips {
			skip = skip || l.path == s || strings.HasPrefix(l.path, s+" ")
		}
		if skip {
			continue
		}
		rep := ix.closureOf(l.root)
		migrated := len(rep.helpers) > 0
		pending := pendingMigration[l.path]
		t.Run(strings.TrimPrefix(l.path, "c1i "), func(t *testing.T) {
			resetCmdFlags(t, rootCmd)
			resetCmdFlags(t, l.cmd)
			var out, errOut bytes.Buffer
			for _, c := range []*cobra.Command{rootCmd, l.cmd} {
				c.SetOut(&out)
				c.SetErr(&errOut)
			}
			t.Cleanup(func() {
				for _, c := range []*cobra.Command{rootCmd, l.cmd} {
					c.SetOut(nil)
					c.SetErr(nil)
					// A canceled context left on a shared command fails later tests.
					c.SetContext(context.Background())
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			l.cmd.SetContext(ctx)
			args := strings.Fields(strings.TrimPrefix(l.path, "c1i "))
			for _, a := range stubArgs(l.cmd) {
				if flag, body, ok := strings.Cut(a, "=@"); ok {
					file := filepath.Join(t.TempDir(), "body.json")
					if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
						t.Fatal(err)
					}
					a = flag + "=" + file
				}
				args = append(args, a)
			}
			rootCmd.SetArgs(args)
			_, err := rootCmd.ExecuteContextC(ctx)
			var doc map[string]json.RawMessage
			preview := json.Unmarshal(out.Bytes(), &doc) == nil && string(doc["dryRun"]) == "true"
			switch {
			case migrated && !preview:
				t.Errorf("reaches sendMutation* but printed no preview (args %q, err %v): %q; add a dryRunStubs entry if the stub is wrong", args, err, out.String())
			case !migrated && preview:
				t.Errorf("printed a preview but Rule B does not see sendMutation* in its closure")
			case pending && !strings.HasPrefix(out.String(), "[dry-run] "):
				t.Errorf("pending leaf did not reach its dry run (args %q, err %v): %q; add a dryRunStubs entry", args, err, out.String())
			}
		})
	}
}
