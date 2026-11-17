package cmd

import (
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// userRefExemptions are the user-id flags and positionals of mutating commands
// that deliberately skip existingUsersCheck, keyed "<command path> --flag" or
// "<command path> <positional>". A read command's filter is not listed: it is
// out of scope by construction, since an unknown value returns no rows.
var userRefExemptions = map[string]string{
	"c1i apps remove-owner --user-id":         "removal never pre-checks a reference; the server decides (documented --dry-run exception)",
	"c1i entitlements remove-owner --user-id": "removal never pre-checks a reference; the server decides (documented --dry-run exception)",

	// A service principal is a C1 user, but these address it as the scope the
	// request acts on, not as a reference being added; the server decides.
	"c1i service-principals bindings add --service-principal-id":       "the scope the binding is added to, not an added reference; the server decides",
	"c1i service-principals bindings delete --service-principal-id":    "removal never pre-checks a reference; the server decides (documented --dry-run exception)",
	"c1i service-principals credentials revoke --service-principal-id": "removal never pre-checks a reference; the server decides (documented --dry-run exception)",
	"c1i service-principals credentials update --service-principal-id": "the scope whose credential is updated, not an added reference; the server decides",
	"c1i service-principals credentials create <sp-id>":                "the scope the credential is created under, not an added reference; the server decides",
	"c1i service-principals update <sp-id>":                            "the command's own resource id, not a reference; the server decides",
	"c1i service-principals delete <sp-id>":                            "the command's own resource id; removal never pre-checks",
}

// Detection is by name and by usage text. Ids inside a --*-file/--body-file
// JSON body are out of scope: no flag names them, so this walk cannot see them
// (ledger C272).
var (
	userRefNameRE  = regexp.MustCompile(`(^|-)(user|owner|assignee|approver|reviewer|requester|delegate|manager|member|subject|principal)(-ids?)?$`)
	userRefUsageRE = regexp.MustCompile(`(?i)\b(c1 )?(user|member)('s)? (ids?|identifiers?)\b|\b(user|member)('s)? ids?\b|\b(ids?|identifier)s? (of|for) (the|a) (user|member)\b|^(user|member|principal)\b`)
	userRefPosRE   = regexp.MustCompile(`<([a-z-]*(user|member|owner|assignee|approver|subject|principal|sp)-ids?)>`)
)

// isUserRefFlag reports whether f carries a C1 user id, by name or by what its
// usage says it is. An app user (account) id is not one.
func isUserRefFlag(f *pflag.Flag) bool {
	if strings.HasPrefix(f.Name, "app-user") {
		return false
	}
	return userRefNameRE.MatchString(f.Name) || userRefUsageRE.MatchString(f.Usage)
}

// userRefs lists every user-id flag (own and inherited) and positional on a
// mutating command, keyed as in userRefExemptions.
func userRefs() map[string]*cobra.Command {
	out := map[string]*cobra.Command{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if isMutating(c) {
			visit := func(f *pflag.Flag) {
				if isUserRefFlag(f) {
					out[c.CommandPath()+" --"+f.Name] = c
				}
			}
			c.LocalFlags().VisitAll(visit)
			c.InheritedFlags().VisitAll(visit)
			for _, m := range userRefPosRE.FindAllStringSubmatch(c.Use, -1) {
				if !strings.HasPrefix(m[1], "app-user") {
					out[c.CommandPath()+" <"+m[1]+">"] = c
				}
			}
		}
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(rootCmd)
	return out
}

// checkedUserRefs is every "<path> --flag" a command marked with
// markUserRefChecked.
func checkedUserRefs() map[string]bool {
	out := map[string]bool{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, f := range strings.Split(c.Annotations[userRefCheckedAnnotation], ",") {
			if f != "" {
				out[c.CommandPath()+" --"+f] = true
			}
		}
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(rootCmd)
	return out
}

// TestEveryUserRefFlagIsCheckedOrExempt: a mutating command's user id must be
// looked up before the send (markUserRefChecked, backed by an end-to-end case
// below) or be exempted with a reason. The API stores an unknown id as an
// owner or approver no user matches, at exit 0.
func TestEveryUserRefFlagIsCheckedOrExempt(t *testing.T) {
	refs := userRefs()
	checked := checkedUserRefs()
	if len(refs) < 10 {
		t.Fatalf("found only %d user references (%v); the detection broke", len(refs), refs)
	}
	for key, c := range refs {
		reason, exempt := userRefExemptions[key]
		switch {
		case checked[key] && exempt:
			t.Errorf("%s is both checked and exempt", key)
		case !checked[key] && !exempt:
			if strings.Contains(key, " <") {
				t.Errorf("%s is a positional user id; positionals cannot be marked checked, so add it to userRefExemptions with a reason", key)
			} else {
				t.Errorf("%s carries a user id but runs no existingUsersCheck: add withPreRead(existingUsersCheck(...)) and markUserRefChecked(%s), or add it to userRefExemptions with a reason", key, c.Name())
			}
		case exempt && strings.TrimSpace(reason) == "":
			t.Errorf("%s is exempt without a reason", key)
		}
	}
	for key := range checked {
		if _, ok := refs[key]; !ok {
			t.Errorf("%s is marked checked but is not a user-id flag on a mutating command", key)
		}
	}
	for key := range userRefExemptions {
		if _, ok := refs[key]; !ok {
			t.Errorf("exemption %q matches no user-id flag; delete it", key)
		}
	}
}

// userRefCases is one invocation per checked flag, with the flag's value last
// so the test can swap it.
var userRefCases = map[string][]string{
	"c1i accounts set-owner --user-id":         {"accounts", "set-owner", "au1", "--app-id", "app1", "--user-id"},
	"c1i apps add-owner --user-id":             {"apps", "add-owner", "app1", "--user-id"},
	"c1i apps set-owners --user-id":            {"apps", "set-owners", "app1", "--user-id"},
	"c1i entitlements add-owner --user-id":     {"entitlements", "add-owner", "ent1", "--app-id", "app1", "--user-id"},
	"c1i entitlements set-owners --user-id":    {"entitlements", "set-owners", "ent1", "--app-id", "app1", "--user-id"},
	"c1i entitlements create --owner-id":       {"entitlements", "create", "--app-id", "app1", "--display-name", "e", "--owner-id"},
	"c1i requests create grant --user-id":      {"requests", "create", "grant", "--app-id", "app1", "--entitlement-id", "ent1", "--user-id"},
	"c1i requests create revoke --user-id":     {"requests", "create", "revoke", "--app-id", "app1", "--entitlement-id", "ent1", "--user-id"},
	"c1i grants add --user-id":                 {"grants", "add", "--app-id", "app1", "--entitlement-id", "ent1", "--user-id"},
	"c1i mcp servers register --user-id":       {"mcp", "servers", "register", "--app-id", "app1", "--type", "external", "--display-name", "Srv", "--server-url", "https://mcp.example.com/sse", "--auth", "none", "--user-id"},
	"c1i tasks reassign --to-user-id":          {"tasks", "reassign", "t1", "--policy-step-id", "s1", "--to-user-id"},
	"c1i apps managed-state promote --user-id": {"apps", "managed-state", "promote", "res1", "--app-id", "app1", "--resource-type-id", "rt1", "--user-id"},
}

// TestUserRefChecksRefuseUnknownUsers: for every checked flag, an unknown user
// exits 4 with no mutating request, and --dry-run refuses identically. A
// known user is not blocked: a real run sends, a preview does not.
func TestUserRefChecksRefuseUnknownUsers(t *testing.T) {
	checked := checkedUserRefs()
	var keys []string
	for k := range checked {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, ok := userRefCases[k]; !ok {
			t.Errorf("%s has no end-to-end case in userRefCases", k)
		}
	}
	for k := range userRefCases {
		if !checked[k] {
			t.Errorf("userRefCases has %q, which is not marked checked", k)
		}
	}

	type outcome struct {
		exit      int
		stdout    string
		stderr    string
		userGets  []string
		mutations []string
	}
	run := func(t *testing.T, key string, userStatus int, dry bool) outcome {
		t.Helper()
		t.Setenv("C1I_URL", mutTestURL)
		// Another test's viper override would beat the --dry-run flag.
		clearViperOverrides(t, "dry_run")
		viper.Set("dry_run", dry)
		s := newMutServer(t, func(r *http.Request, _ int) mutReply {
			if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/users/") {
				if userStatus == http.StatusNotFound {
					return mutReply{status: userStatus, body: `{"code":5,"message":"not found"}`}
				}
				return mutReply{body: `{"user":{"id":"u"}}`}
			}
			return mutReply{body: `{}`}
		})
		args := append(append([]string{}, userRefCases[key]...), "zz-unknown-user-id")
		if dry {
			args = append(args, "--dry-run")
		}
		r := runMutationCLI(t, s.srv, 0, nil, args...)
		o := outcome{exit: r.exit, stdout: r.stdout, stderr: r.stderr}
		for _, rq := range s.requests() {
			switch {
			case rq.method == http.MethodGet && strings.HasPrefix(rq.rawPath, "/api/v1/users/"):
				o.userGets = append(o.userGets, strings.TrimPrefix(rq.rawPath, "/api/v1/users/"))
			case rq.method != http.MethodGet:
				o.mutations = append(o.mutations, rq.method+" "+rq.rawPath)
			}
		}
		return o
	}

	for _, k := range keys {
		if _, ok := userRefCases[k]; !ok {
			continue
		}
		t.Run(strings.TrimPrefix(k, "c1i "), func(t *testing.T) {
			real := run(t, k, http.StatusNotFound, false)
			dry := run(t, k, http.StatusNotFound, true)
			for name, o := range map[string]outcome{"real": real, "dry-run": dry} {
				if o.exit != exitNotFound {
					t.Errorf("%s: exit %d, want %d (stderr %q)", name, o.exit, exitNotFound, o.stderr)
				}
				if !strings.Contains(o.stderr, "zz-unknown-user-id") || !strings.Contains(o.stderr, "no such user") {
					t.Errorf("%s: stderr %q does not name the unknown user", name, o.stderr)
				}
				if len(o.mutations) != 0 {
					t.Errorf("%s: sent %v despite an unknown user", name, o.mutations)
				}
				if len(o.userGets) == 0 || o.userGets[0] != "zz-unknown-user-id" {
					t.Errorf("%s: user lookups %v, want one for the unknown id", name, o.userGets)
				}
				if o.stdout != "" {
					t.Errorf("%s: stdout %q, want nothing", name, o.stdout)
				}
			}
			if real.exit != dry.exit || real.stderr != dry.stderr {
				t.Errorf("--dry-run differs from a real run: %d %q vs %d %q", dry.exit, dry.stderr, real.exit, real.stderr)
			}

			okReal := run(t, k, http.StatusOK, false)
			okDry := run(t, k, http.StatusOK, true)
			if len(okReal.mutations) == 0 {
				t.Errorf("a known user was blocked: exit %d, stderr %q", okReal.exit, okReal.stderr)
			}
			if len(okDry.mutations) != 0 || okDry.exit != 0 {
				t.Errorf("preview for a known user: exit %d, sent %v, stderr %q", okDry.exit, okDry.mutations, okDry.stderr)
			}
		})
	}
}

// TestUserRefCommandsShareTheHelpSentence: every marked command carries the
// one shared sentence, so the contract cannot drift per command.
func TestUserRefCommandsShareTheHelpSentence(t *testing.T) {
	seen := map[string]bool{}
	for key := range checkedUserRefs() {
		seen[strings.SplitN(key, " --", 2)[0]] = true
	}
	if len(seen) == 0 {
		t.Fatal("no marked commands")
	}
	for path := range seen {
		c, _, err := rootCmd.Find(strings.Fields(strings.TrimPrefix(path, "c1i ")))
		if err != nil || c.CommandPath() != path {
			t.Fatalf("cannot find %s: %v", path, err)
		}
		if strings.Count(c.Long, userRefHelp) != 1 {
			t.Errorf("%s: help does not carry the shared sentence exactly once", path)
		}
	}
}

var staleExit2RE = regexp.MustCompile("(?i)(unknown (user|one)|no such policy)[^.]*exits? `?2\\b|exits? `?2\\b[^.]*(unknown (user|one)|no such policy)")

// TestDocsDoNotClaimExit2ForUnknownRefs: an unknown user or policy exits 4; a
// sentence tying one to exit 2 contradicts the contract.
func TestDocsDoNotClaimExit2ForUnknownRefs(t *testing.T) {
	for _, f := range []string{"../README.md", "agents.md", "../CLAUDE.md"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(b), "\n", " ")
		if m := staleExit2RE.FindString(text); m != "" {
			t.Errorf("%s: %q ties an unknown reference to exit 2", f, m)
		}
		if strings.Contains(text, "`entitlements add-owner` excepted") {
			t.Errorf("%s: entitlements add-owner is checked, not excepted", f)
		}
	}
}

// TestCommandHelpDoesNotClaimExit2ForUnknownRefs is the same guard over every
// command's help text.
func TestCommandHelpDoesNotClaimExit2ForUnknownRefs(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		text := strings.ReplaceAll(c.Short+" "+c.Long, "\n", " ")
		if m := staleExit2RE.FindString(text); m != "" {
			t.Errorf("%s: help %q ties an unknown reference to exit 2", c.CommandPath(), m)
		}
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk(rootCmd)
}

// TestUserLookupFailures: a miss is one concise line in text with the request
// in the JSON error, and a 200 that is not JSON is a C1 fault (exit 6), not
// "the user exists".
func TestUserLookupFailures(t *testing.T) {
	args := []string{"apps", "add-owner", "app1", "--user-id", "zz-x"}
	t.Setenv("C1I_URL", mutTestURL)

	miss := newMutServer(t, fixedReply(http.StatusNotFound, `{"code":5,"message":"not found"}`))
	r := runMutationCLI(t, miss.srv, 0, nil, args...)
	if r.exit != exitNotFound || strings.TrimSpace(r.stderr) != `Error: --user-id "zz-x": no such user` {
		t.Errorf("text: exit %d stderr %q", r.exit, r.stderr)
	}
	r = runMutationCLI(t, miss.srv, 0, nil, append(args, "--error-format", "json")...)
	obj := r.errorObject(t)
	if obj["error"] != `--user-id "zz-x": no such user` || obj["status"] != float64(404) || obj["method"] != "GET" || obj["path"] != "/api/v1/users/zz-x" || obj["body"] == nil {
		t.Errorf("json error = %v, want the concise text plus the lookup's status, method, path and body", obj)
	}

	bad := newMutServer(t, fixedReply(http.StatusOK, `<html>not json</html>`))
	r = runMutationCLI(t, bad.srv, 0, nil, args...)
	if r.exit != exitServer {
		t.Errorf("non-JSON 200: exit %d (%v), want %d", r.exit, r.err, exitServer)
	}
	for _, q := range bad.requests() {
		if q.method != http.MethodGet {
			t.Errorf("sent %s %s after an unusable lookup", q.method, q.rawPath)
		}
	}
}
