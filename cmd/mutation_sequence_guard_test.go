package cmd

import (
	"context"
	"net/http"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// A command that sends several requests must refuse bad input before the
// first one, or a later step's rejection leaves the earlier objects behind.
// Multi-request commands are found by behaviour (a valid invocation from
// mutationTable sends more than one write) and by structure (the command's
// code reaches a named call to sendMutationSequence or sendMutationBatch,
// through method values and helpers), so a new one fails here until it has a case.

// seqGuardCase drives one multi-request command, keyed by its command path.
type seqGuardCase struct {
	base       []string // a valid invocation
	writes     int      // mutating requests base sends; more than one
	goodExtras [][]string
	// requires: extra args a flag needs to be valid at all.
	requires map[string][]string
	// preChecked: id flags a read-only existence check resolves, so an unknown
	// one is answered 404 by the stub and must be refused.
	preChecked []string
	// firstUse: id flags with no pre-read, and why an unknown one cannot leave
	// a partial apply.
	firstUse map[string]string
	// badLocal: flag combinations refused on syntax alone (exit 2), each with
	// text its error must contain, so a stale or typo'd row cannot pass.
	badLocal []badLocalRow
	// positional: the command takes ids as positionals (a batch).
	positional bool
}

var seqGuardCases = map[string]seqGuardCase{
	"entitlements create": {
		base:       []string{"--app-id", "app-1", "--display-name", "e"},
		writes:     3,
		goodExtras: [][]string{{"--owner-id", "u-1"}},
		requires:   map[string][]string{"resource-id": {"--resource-type-id", "rt-1"}},
		preChecked: append([]string{"owner-id"}, func() []string {
			var f []string
			for _, k := range entitlementPolicyKinds {
				f = append(f, k.flag)
			}
			return f
		}()...),
		badLocal: []badLocalRow{
			{[]string{"--duration-grant", "-1h"}, "--duration-grant must be a whole-number duration"},
			{[]string{"--duration-grant", "1.5h"}, "--duration-grant must be a whole-number duration"},
			{[]string{"--duration-grant", "soon"}, "--duration-grant must be a whole-number duration"},
			{[]string{"--resource-id", "res-1"}, "--resource-id requires --resource-type-id"},
			{[]string{"--resource-type-id", "rt-1", "--resource-type", "custom"}, "--resource-type only applies when"},
			{[]string{"--resource-type-id", "rt-1", "--resource-type-display-name", "x"}, "--resource-type-display-name only applies when"},
			{[]string{"--resource-type-id", "rt-1", "--resource-id", "res-1", "--resource-display-name", "x"}, "--resource-display-name only applies when"},
		},
		firstUse: map[string]string{
			"app-id":           "first consumed by step 1 (or step 2 when the type is reused)",
			"resource-type-id": "when given, step 1 is skipped and step 2 or 3 is the first request",
			"resource-id":      "when given, steps 1 and 2 are skipped and step 3 is the only request",
		},
	},
	"mcp tools approve": {
		base:       []string{"t1", "t2", "--app-id", "app-1", "--connector-id", "c-1"},
		writes:     2,
		positional: true,
		firstUse: map[string]string{
			"app-id":       "same value in every request of the batch",
			"connector-id": "same value in every request of the batch",
		},
	},
}

// multiRequestExempt are commands that send several requests by design and
// are not built on the mutation helpers, so the client cannot order them.
var multiRequestExempt = map[string]string{
	"auth login": "--scoped-role creates a helper client, then deletes it; a designed flow the client cannot avoid",
	"api":        "the raw escape hatch; --paginate repeats reads",
}

// seqGuardServer answers reads (all 404 when getStatus is, else 200) and
// accepts every write.
func seqGuardServer(t *testing.T, getStatus int) *mutServer {
	t.Helper()
	return newMutServer(t, func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet {
			if getStatus != http.StatusOK {
				return mutReply{status: getStatus, body: `{"code":5,"message":"not found"}`}
			}
			if strings.Contains(r.URL.Path, "/policies/") {
				return mutReply{body: `{"policy":{"policyType":"POLICY_TYPE_GRANT"}}`}
			}
			return mutReply{body: `{"user":{"id":"u"}}`}
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/resource_types"):
			return mutReply{body: `{"appResourceType":{"id":"rt-new"}}`}
		case strings.HasSuffix(r.URL.Path, "/resources"):
			return mutReply{body: `{"appResource":{"id":"res-new"}}`}
		case strings.Contains(r.URL.Path, "/mcp_tools/"):
			return mutReply{body: `{"tool":{"id":"t"}}`}
		}
		return mutReply{body: `{"appEntitlementView":{"appEntitlement":{"id":"e-new"}}}`}
	})
}

func (m *mutServer) mutations() int {
	n := 0
	for _, r := range m.requests() {
		if r.method != http.MethodGet {
			n++
		}
	}
	return n
}

// seqRun runs path+args once for real and once under --dry-run, against
// separate stubs, and returns both.
func seqRun(t *testing.T, path, args []string, getStatus int) (real, dry mutResult, rs, ds *mutServer) {
	t.Helper()
	t.Setenv("C1I_URL", mutTestURL)
	argv := append(append([]string(nil), path...), args...)
	rs = seqGuardServer(t, getStatus)
	real = runMutationCLI(t, rs.srv, 0, nil, argv...)
	withDryRun(t)
	ds = seqGuardServer(t, getStatus)
	dry = runMutationCLI(t, ds.srv, 0, nil, argv...)
	return real, dry, rs, ds
}

func errText(r mutResult) string {
	if r.err == nil {
		return ""
	}
	return r.err.Error()
}

func TestMultiRequestCommandsHaveAGuardCase(t *testing.T) {
	ix := loadSrcIndex(t, nil)
	structural := map[string]bool{}
	for _, l := range leafCommands(t, ix) {
		h := ix.closureOf(l.root).helpers
		if h["sendMutationSequence"] || h["sendMutationBatch"] {
			structural[strings.TrimPrefix(l.path, "c1i ")] = true
		}
	}
	// Behaviour: a valid invocation of every mutating command, counting writes.
	behavioural := map[string]int{}
	for _, row := range mutationTable {
		t.Setenv("C1I_URL", mutTestURL)
		s := newMutServer(t, tableRoute(row.body))
		runMutationCLI(t, s.srv, 0, nil, row.argv(t)...)
		behavioural[row.cmd] = s.mutations()
	}
	for path, n := range behavioural {
		if _, ok := seqGuardCases[path]; n > 1 && !ok && multiRequestExempt[path] == "" {
			t.Errorf("%s sent %d writes: add a seqGuardCases entry, or name it in multiRequestExempt with why", path, n)
		}
	}
	for path := range structural {
		if _, ok := seqGuardCases[path]; !ok {
			t.Errorf("%s reaches sendMutationSequence/Batch but has no seqGuardCases entry", path)
		}
	}
	var names []string
	for p := range seqGuardCases {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		if !structural[p] && behavioural[p] < 2 {
			t.Errorf("seqGuardCases has %q, which neither reaches a multi-request helper nor sends several writes", p)
		}
	}
	for p, why := range multiRequestExempt {
		c, _, err := rootCmd.Find(strings.Fields(p))
		if err != nil || c == rootCmd || c.CommandPath() != "c1i "+p || why == "" {
			t.Errorf("multiRequestExempt %q: not a command, or no reason", p)
		}
		if _, ok := seqGuardCases[p]; ok {
			t.Errorf("%q is both exempt and cased", p)
		}
	}
}

// seqGuardIDFlags returns the id-valued string flags of c, from the tree.
func seqGuardIDFlags(c *cobra.Command) []string {
	var out []string
	c.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if (f.Value.Type() == "string" || f.Value.Type() == "stringArray") && strings.HasSuffix(f.Name, "-id") {
			out = append(out, f.Name)
		}
	})
	sort.Strings(out)
	return out
}

func TestSequenceCommandsRefuseBadInputBeforeStepOne(t *testing.T) {
	for name, tc := range seqGuardCases {
		t.Run(name, func(t *testing.T) {
			path := strings.Fields(name)
			cmd, _, err := rootCmd.Find(path)
			if err != nil || cmd.CommandPath() != "c1i "+name {
				t.Fatalf("%q is not a command", name)
			}
			if tc.writes < 2 {
				t.Fatalf("writes = %d: a case is for a command that sends several", tc.writes)
			}

			// Positive control: the valid invocations succeed, sending the
			// stated writes for real and none under --dry-run. Without it a
			// broken base would make every refusal below pass for the wrong
			// reason.
			for _, extra := range append([][]string{nil}, tc.goodExtras...) {
				t.Run("valid "+strings.Join(extra, " "), func(t *testing.T) {
					args := append(append([]string(nil), tc.base...), extra...)
					real, dry, rs, ds := seqRun(t, path, args, http.StatusOK)
					if real.exit != 0 || rs.mutations() != tc.writes {
						t.Errorf("real: exit %d (%v), %d writes; want exit 0 and %d", real.exit, real.err, rs.mutations(), tc.writes)
					}
					if dry.exit != 0 || ds.mutations() != 0 {
						t.Errorf("dry-run: exit %d (%v), %d writes; want exit 0 and none", dry.exit, dry.err, ds.mutations())
					}
				})
			}

			for _, f := range seqGuardIDFlags(cmd) {
				if !slices.Contains(tc.preChecked, f) && tc.firstUse[f] == "" {
					t.Errorf("--%s: name it in preChecked (a read-only existence check) or firstUse (why an unknown one applies nothing)", f)
				}
			}

			type scenario struct {
				label, flag, value string
				getStatus          int
				want               []int
			}
			refuse := func(t *testing.T, sc scenario, args []string) {
				real, dry, rs, ds := seqRun(t, path, args, sc.getStatus)
				for mode, r := range map[string]mutResult{"real": real, "dry-run": dry} {
					srv := map[string]*mutServer{"real": rs, "dry-run": ds}[mode]
					if n := srv.mutations(); n != 0 {
						t.Errorf("%s %v: %d write(s) sent, want 0 (exit %d: %v)", mode, args, n, r.exit, r.err)
					}
					if !slices.Contains(sc.want, r.exit) {
						t.Errorf("%s %v: exit %d (%v), want one of %v", mode, args, r.exit, r.err, sc.want)
					}
					// The refusal names the input, so it is not some other error.
					if sc.flag != "" && !strings.Contains(errText(r), sc.flag) {
						t.Errorf("%s %v: error %q lacks %q", mode, args, errText(r), sc.flag)
					}
				}
				if real.exit != dry.exit || errText(real) != errText(dry) {
					t.Errorf("%v: real run exit %d %q, dry-run exit %d %q: --dry-run must refuse exactly what a real run refuses",
						args, real.exit, errText(real), dry.exit, errText(dry))
				}
			}
			valueScenarios := func(flag string) []scenario {
				f := ""
				if flag != "" {
					f = "--" + flag
				}
				out := []scenario{
					{"blank", f, "", 200, []int{exitUsage}},
					{"space", f, " ", 200, []int{exitUsage}},
					{"dot", f, ".", 200, []int{exitUsage}},
					{"dotdot", f, "..", 200, []int{exitUsage}},
					{"padded dotdot", f, " .. ", 200, []int{exitUsage}},
				}
				if slices.Contains(tc.preChecked, flag) {
					out = append(out, scenario{"unknown", f, "no-such-id", http.StatusNotFound, []int{exitUsage, exitNotFound}})
				}
				return out
			}
			for _, flag := range seqGuardIDFlags(cmd) {
				for _, sc := range valueScenarios(flag) {
					t.Run("--"+flag+" "+sc.label, func(t *testing.T) {
						args := append(append([]string(nil), tc.base...), tc.requires[flag]...)
						refuse(t, sc, append(args, "--"+flag, sc.value))
					})
				}
			}

			// Every other string flag: whitespace always, "" unless it may be empty.
			cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
				if (f.Value.Type() != "string" && f.Value.Type() != "stringArray") || slices.Contains(seqGuardIDFlags(cmd), f.Name) {
					return
				}
				vals := map[string]string{"space": " "}
				if len(f.Annotations[emptyAllowedAnnotation]) == 0 {
					vals["blank"] = ""
				}
				for label, v := range vals {
					t.Run("--"+f.Name+" "+label, func(t *testing.T) {
						args := append(append([]string(nil), tc.base...), tc.requires[f.Name]...)
						refuse(t, scenario{label, "--" + f.Name, v, 200, []int{exitUsage}}, append(args, "--"+f.Name, v))
					})
				}
			})
			for _, row := range tc.badLocal {
				t.Run("badLocal "+strings.Join(row.args, " "), func(t *testing.T) {
					refuse(t, scenario{"bad", row.want, "", 200, []int{exitUsage}}, append(append([]string(nil), tc.base...), row.args...))
				})
			}
			if tc.positional {
				// A good id first: nothing may be sent for it either.
				for _, sc := range valueScenarios("")[:5] {
					t.Run("positional "+sc.label, func(t *testing.T) {
						refuse(t, sc, append(append([]string(nil), tc.base...), sc.value))
					})
				}
			}
		})
	}
}

// A later step whose body cannot be sent (unencodable, or null) is refused
// before step 1 is sent. --dry-run never sends, so it passes on any code; its
// check is that it refuses with the same exit code and text as the real run.
func TestSequenceRefusesLaterStepBodyBeforeStepOne(t *testing.T) {
	for name, body := range map[string]any{
		"unencodable": make(chan int),
		"null":        (*int)(nil),
	} {
		t.Run(name, func(t *testing.T) {
			run := func(dry bool) (mutResult, *mutServer) {
				s := newMutServer(t, fixedReply(200, `{"thing":{"id":"x"}}`))
				if dry {
					withDryRun(t)
				}
				c := &cobra.Command{Use: "seq", RunE: func(cmd *cobra.Command, _ []string) error {
					return sendMutationSequence(cmd, []mutationStep{
						{
							Build: func([]string) mutation {
								return mutation{Method: http.MethodPost, Path: "/api/v1/a", Body: map[string]any{}}
							},
							Placeholder: "NEW_ID",
							ResultID:    func([]byte) (string, error) { return "x", nil },
						},
						{Build: func([]string) mutation { return mutation{Method: http.MethodPost, Path: "/api/v1/b", Body: body} }},
					}, "")
				}}
				markMutating(c)
				return runHelperCmd(t, c, s, context.Background(), nil, "json"), s
			}
			real, rs := run(false)
			dry, ds := run(true)
			if rs.mutations() != 0 || ds.mutations() != 0 {
				t.Errorf("writes sent: real %d, dry-run %d, want none", rs.mutations(), ds.mutations())
			}
			if real.err == nil || real.exit != exitError {
				t.Errorf("real: exit %d (%v), want %d", real.exit, real.err, exitError)
			}
			if got := real.errorObject(t)["applied"]; got != false {
				t.Errorf("real: applied = %v, want false", got)
			}
			if dry.exit != real.exit || errText(dry) != errText(real) {
				t.Errorf("dry-run exit %d %q, real exit %d %q: they must match", dry.exit, errText(dry), real.exit, errText(real))
			}
		})
	}
}

type badLocalRow struct {
	args []string
	want string // substring the refusal must contain
}
