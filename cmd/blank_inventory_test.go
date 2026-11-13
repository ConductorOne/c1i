package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// emptyAllowedInventory maps "<command path> --<flag>" to the allowEmpty
// reason of every opted-out flag in the tree, persistent ones under the
// command that declares them.
func emptyAllowedInventory() map[string]string {
	got := map[string]string{}
	walkCommandTree(func(c *cobra.Command) {
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if r := f.Annotations[emptyAllowedAnnotation]; r != nil {
				got[c.CommandPath()+" --"+f.Name] = r[0]
			}
		})
	})
	return got
}

// wantEmptyAllowed pins every allowEmpty opt-out. Each one keeps an exact ""
// working that did something before the blank rule; deleting one, or adding a
// new one, must be a deliberate edit here (a new one must not be a filter).
var wantEmptyAllowed = map[string]string{
	"c1i --error-format": emptyOverridesEnv,
	"c1i --fields":       emptyOverridesEnv,
	"c1i access-profiles bundle-automation run --refs-file":          emptyIsOmitted,
	"c1i access-profiles create --description":                       emptyIsSent,
	"c1i access-profiles create --enrollment-behavior":               emptyIsSent,
	"c1i access-profiles create --unenrollment-behavior":             emptyIsSent,
	"c1i access-profiles create --unenrollment-entitlement-behavior": emptyIsSent,
	"c1i access-profiles update --description":                       emptyIsSent,
	"c1i access-profiles update --enrollment-behavior":               emptyIsSent,
	"c1i access-profiles update --unenrollment-behavior":             emptyIsSent,
	"c1i access-profiles update --unenrollment-entitlement-behavior": emptyIsSent,
	"c1i access-reviews reports generate --body-file":                emptyIsOmitted,
	"c1i access-reviews reports generate --format":                   emptyIsOmitted,
	"c1i api --body":                                            emptyIsOmitted,
	"c1i api --body-file":                                       emptyIsOmitted,
	"c1i api --list-key":                                        emptyIsOmitted,
	"c1i api --method":                                          emptyIsOmitted,
	"c1i apps create --description":                             emptyIsOmitted,
	"c1i apps update --description":                             emptyIsSent,
	"c1i auth login --client-secret":                            emptyIsOmitted,
	"c1i automations update --description":                      emptyIsSent,
	"c1i docs agents --output":                                  emptyIsOmitted,
	"c1i docs changelog --output":                               emptyIsOmitted,
	"c1i docs readme --output":                                  emptyIsOmitted,
	"c1i entitlements create --alias":                           emptyIsOmitted,
	"c1i entitlements create --description":                     emptyIsOmitted,
	"c1i entitlements create --slug":                            emptyIsOmitted,
	"c1i entitlements update --description":                     emptyIsSent,
	"c1i export events --sort":                                  emptyIsOmitted,
	"c1i functions source --commit":                             emptyIsOmitted,
	"c1i functions source --out-dir":                            emptyIsOmitted,
	"c1i mcp classifiers templates add-rule --body-file":        emptyIsOmitted,
	"c1i mcp classifiers templates get --template-version":      emptyIsOmitted,
	"c1i mcp classifiers templates instantiate --body-file":     emptyIsOmitted,
	"c1i mcp gateway --gateway-url":                             emptyIsOmitted,
	"c1i mcp gateway call --args":                               emptyIsOmitted,
	"c1i mcp servers register --auth":                           emptyIsOmitted,
	"c1i mcp servers register --basic-auth-password":            emptyIsOmitted,
	"c1i mcp servers register --basic-auth-username":            emptyIsOmitted,
	"c1i mcp servers register --bearer-token":                   emptyIsOmitted,
	"c1i mcp servers register --data-sensitivity":               emptyIsOmitted,
	"c1i mcp servers register --description":                    emptyIsOmitted,
	"c1i mcp servers register --header-name":                    emptyIsOmitted,
	"c1i mcp servers register --header-value":                   emptyIsOmitted,
	"c1i mcp servers register --server-url":                     emptyIsOmitted,
	"c1i mcp servers register --token-sharing":                  emptyIsOmitted,
	"c1i mcp servers register --tool-prefix":                    emptyIsOmitted,
	"c1i mcp servers register --transport":                      emptyIsOmitted,
	"c1i mcp servers search --tool-state":                       emptyIsOmitted,
	"c1i mcp servers test-connection --auth":                    emptyIsOmitted,
	"c1i mcp servers test-connection --basic-auth-password":     emptyIsOmitted,
	"c1i mcp servers test-connection --basic-auth-username":     emptyIsOmitted,
	"c1i mcp servers test-connection --bearer-token":            emptyIsOmitted,
	"c1i mcp servers test-connection --external-config-file":    emptyIsOmitted,
	"c1i mcp servers test-connection --header-name":             emptyIsOmitted,
	"c1i mcp servers test-connection --header-value":            emptyIsOmitted,
	"c1i mcp servers test-connection --server-url":              emptyIsOmitted,
	"c1i mcp servers test-connection --token-sharing":           emptyIsOmitted,
	"c1i mcp servers test-connection --transport":               emptyIsOmitted,
	"c1i mcp servers test-connection --update-mask":             emptyIsOmitted,
	"c1i mcp servers update --data-sensitivity":                 emptyIsSent,
	"c1i mcp servers update --description":                      emptyIsSent,
	"c1i mcp servers update --tool-prefix":                      emptyIsSent,
	"c1i mcp servers update-credentials --auth":                 emptyIsOmitted,
	"c1i mcp servers update-credentials --basic-auth-password":  emptyIsOmitted,
	"c1i mcp servers update-credentials --basic-auth-username":  emptyIsOmitted,
	"c1i mcp servers update-credentials --bearer-token":         emptyIsOmitted,
	"c1i mcp servers update-credentials --external-config-file": emptyIsOmitted,
	"c1i mcp servers update-credentials --header-name":          emptyIsOmitted,
	"c1i mcp servers update-credentials --header-value":         emptyIsOmitted,
	"c1i mcp servers update-credentials --hosted-config-file":   emptyIsOmitted,
	"c1i mcp servers update-credentials --server-url":           emptyIsOmitted,
	"c1i mcp servers update-credentials --token-sharing":        emptyIsOmitted,
	"c1i mcp servers update-credentials --transport":            emptyIsOmitted,
	"c1i mcp servers update-credentials --update-mask":          emptyIsOmitted,
	"c1i mcp tools approve --state":                             emptyIsOmitted,
	"c1i mcp toolsets create --description":                     emptyIsOmitted,
	"c1i mcp toolsets update --description":                     emptyIsSent,
	"c1i policies create --description":                         emptyIsOmitted,
	"c1i policies create --rules-file":                          emptyIsOmitted,
	"c1i policies create --steps-file":                          emptyIsOmitted,
	"c1i policies update --description":                         emptyIsSent,
	"c1i requests create grant --description":                   emptyIsOmitted,
	"c1i requests create revoke --description":                  emptyIsOmitted,
	"c1i tasks approve --comment":                               emptyIsOmitted,
	"c1i tasks close --comment":                                 emptyIsOmitted,
	"c1i tasks deny --comment":                                  emptyIsOmitted,
	"c1i tasks reassign --comment":                              emptyIsOmitted,
	"c1i tasks reset --comment":                                 emptyIsOmitted,
	"c1i tasks restart --comment":                               emptyIsOmitted,
	"c1i tasks skip-step --comment":                             emptyIsOmitted,
	"c1i webhooks create --description":                         emptyIsSent,
	"c1i webhooks update --description":                         emptyIsSent,
}

func TestEmptyAllowedInventoryIsPinned(t *testing.T) {
	got := emptyAllowedInventory()
	for k, want := range wantEmptyAllowed {
		if got[k] != want {
			t.Errorf("%s: allowEmpty reason %q, want %q (an opt-out was removed or changed)", k, got[k], want)
		}
	}
	for k, reason := range got {
		if _, ok := wantEmptyAllowed[k]; !ok {
			t.Errorf("%s: new allowEmpty (%s) not in wantEmptyAllowed; add it if an exact \"\" really means something and it is not a filter", k, reason)
		}
	}
}

// Every pinned opt-out still lets an exact "" past the blank rule, and an
// emptyIsSent one puts it on the wire.
func TestEmptyAllowedFlagsAcceptEmpty(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	for key, reason := range wantEmptyAllowed {
		c, flag := commandForInventoryKey(t, key)
		t.Run(key, func(t *testing.T) {
			srv := newMutServer(t, fixedReply(200, "{}"))
			extra := extraArgsFor(c, flag)
			if override, ok := inventoryKeyArgs[key]; ok {
				extra = override
			}
			args := append(blankArgs(t, c, flag, ""), extra...)
			res := runMutationCLI(t, srv.srv, 0, nil, args...)
			if res.err != nil && strings.Contains(res.err.Error(), "--"+flag+" requires a non-empty value") {
				t.Fatalf("\"\" refused: %v", res.err)
			}
			if want, ok := inventoryExpectedError[key]; ok {
				if res.err == nil || !strings.Contains(res.err.Error(), want) {
					t.Fatalf("err = %v, want the %q error that omitting the flag gives", res.err, want)
				}
				return
			}
			// Exit 2 means "" was refused by validation. A later failure (auth,
			// network, a reply shape the fake server can't give) means it got
			// past every check, which is what an opt-out promises.
			if res.exit == exitUsage {
				t.Fatalf("exit 2 with \"\": %v", res.err)
			}
			if reason != emptyIsSent {
				return
			}
			want := fmt.Sprintf("%q:\"\"", flagJSONKey(flag))
			sent := false
			for _, r := range srv.requests() {
				sent = sent || strings.Contains(r.body, want)
			}
			if !sent {
				t.Errorf("emptyIsSent, but no request body carried %s (exit %d, err %v)", want, res.exit, res.err)
			}
		})
	}
}

// inventoryExtraArgs gives a command the valid "--flag=value" args its
// placeholder "x" would fail on (a later occurrence of a flag wins). The flag
// under test is never overridden.
var inventoryExtraArgs = map[string][]string{
	"c1i api":                            {"--path=/api/v1/users"},
	"c1i webhooks create":                {"--destination-url=https://hooks.example.com/x"},
	"c1i policies create":                {"--policy-type=grant", "--allow-deny-all=true"},
	"c1i requests create grant":          {"--user-id=u1"},
	"c1i requests create revoke":         {"--user-id=u1"},
	"c1i tasks reassign":                 {"--to-user-id=u2", "--policy-step-id=s1"},
	"c1i tasks approve":                  {"--policy-step-id=s1"},
	"c1i tasks deny":                     {"--policy-step-id=s1"},
	"c1i tasks skip-step":                {"--policy-step-id=s1"},
	"c1i mcp servers register":           {"--type=external", "--server-url=https://mcp.example.com/x"},
	"c1i mcp servers test-connection":    {"--server-url=https://mcp.example.com/x"},
	"c1i mcp servers update-credentials": {"--type=external", "--server-url=https://mcp.example.com/x", "--update-mask=url"},
}

// inventoryKeyArgs replaces a command's fixture for one key whose flag is only
// read under other arguments.
var inventoryKeyArgs = map[string][]string{
	"c1i mcp servers update-credentials --hosted-config-file": {"--type=hosted", "--catalog-id=cat1", "--update-mask=hostedConfig"},
	// A closed local port: no DNS lookup for the device-flow endpoint.
	"c1i auth login --client-secret": {"--url=https://127.0.0.1:1"},
}

// extraArgsFor returns c's extra "--flag=value" args minus any that set flag.
func extraArgsFor(c *cobra.Command, flag string) []string {
	var out []string
	for _, a := range inventoryExtraArgs[c.CommandPath()] {
		if !strings.HasPrefix(a, "--"+flag+"=") {
			out = append(out, a)
		}
	}
	return out
}

// flagJSONKey is the camelCase body key a flag maps to (tool-prefix ->
// toolPrefix).
func flagJSONKey(flag string) string {
	parts := strings.Split(flag, "-")
	for i := 1; i < len(parts); i++ {
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

// inventoryExpectedError lists opt-outs where "" reads as omitted and
// omitting the flag is itself an error: "" must give exactly that error.
var inventoryExpectedError = map[string]string{
	"c1i mcp servers register --server-url":        "--server-url is required for --type external",
	"c1i mcp servers test-connection --server-url": "provide the config to probe",
}

// commandForInventoryKey resolves "<path> --<flag>" to a runnable command
// that has the flag: the command itself, or its first runnable descendant for
// a persistent flag on a group.
func commandForInventoryKey(t *testing.T, key string) (*cobra.Command, string) {
	t.Helper()
	i := strings.LastIndex(key, " --")
	path, flag := strings.Fields(key[:i])[1:], key[i+3:]
	c, _, err := rootCmd.Find(path)
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	for !c.Runnable() || c == rootCmd {
		var next *cobra.Command
		for _, sub := range c.Commands() {
			if sub.Runnable() && sub.Name() != "help" && sub.Name() != "completion" {
				next = sub
				break
			}
		}
		if next == nil {
			for _, sub := range c.Commands() {
				if sub.Name() != "help" && sub.Name() != "completion" && !strings.HasPrefix(sub.Name(), "__") {
					next = sub
					break
				}
			}
		}
		if next == nil {
			t.Fatalf("%s: no runnable command under %s", key, c.CommandPath())
		}
		c = next
	}
	return c, flag
}
