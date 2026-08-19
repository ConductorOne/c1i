package cmd

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// This file pins the "id addressing a single existing resource is a
// positional argument, not a flag" convention introduced when every command
// that previously took --id / --task-id / --connector-id / --app-entitlement-id
// / --app-user-id / --catalog-id / --user-id was switched to a first
// positional argument. Parent/scope ids and list filters were left as flags.
//
// Every assertion below resolves real *cobra.Command values from rootCmd and
// inspects their Use/Args/Flags. Nothing here ever calls RunE, so no test
// touches auth, the network, or any other side effect.

// findCommand walks a path of command names (each segment matched against
// cobra's Name(), the first whitespace-delimited token of Use) starting at
// root and returns the resolved *cobra.Command. It fails the test immediately
// if any segment doesn't resolve, so a typo'd path can't silently pass by
// matching the wrong (or no) command.
func findCommand(t *testing.T, root *cobra.Command, path ...string) *cobra.Command {
	t.Helper()
	cur := root
	for i, seg := range path {
		var next *cobra.Command
		for _, c := range cur.Commands() {
			if c.Name() == seg {
				next = c
				break
			}
		}
		if next == nil {
			t.Fatalf("command path %q: no child %q under %q", strings.Join(path, " "), seg, strings.Join(path[:i], " "))
		}
		cur = next
	}
	return cur
}

// argsAccepts reports whether cmd's positional-argument validator accepts n
// arguments, WITHOUT ever invoking RunE. cobra.Command.ValidateArgs is the
// public entry point cobra itself calls before Run/RunE; when Args is unset
// it falls back to cobra.ArbitraryArgs (see (*cobra.Command).ValidateArgs in
// github.com/spf13/cobra@v1.10.2/command.go), so calling ValidateArgs here
// mirrors real cobra behavior exactly rather than trying to compare the Args
// func by identity (which isn't possible - it's a func value).
func argsAccepts(cmd *cobra.Command, n int) bool {
	args := make([]string, n)
	for i := range args {
		args[i] = "x"
	}
	return cmd.ValidateArgs(args) == nil
}

// hasFlag reports whether name is registered on cmd, either as a flag
// declared directly on cmd or inherited (persistent) from an ancestor.
// InheritedFlags merges parent persistent flags into cmd's own flag set as a
// side effect (see (*cobra.Command).mergePersistentFlags), so checking Flags()
// after calling it covers both cases.
func hasFlag(cmd *cobra.Command, name string) bool {
	if cmd.Flags().Lookup(name) != nil {
		return true
	}
	return cmd.InheritedFlags().Lookup(name) != nil
}

// positionalSpec is what a command's Use string documents about its
// positional arguments.
type positionalSpec struct {
	required int // count of bare "<name>" tokens
	optional int // count of "[<name>]" tokens
}

var (
	// flagValueHintRe matches a bracketed flag-usage example such as
	// "[--filter <pattern>]" (see `docs endpoints`'s Use string) so it can be
	// stripped before counting positional placeholders - otherwise the
	// "<pattern>" inside it would be misread as an optional positional
	// argument, which it is not: it's documentation for --filter's value.
	flagValueHintRe = regexp.MustCompile(`\[--[\w-]+(?:\s+<[\w-]+>)?\]`)
	optionalPosRe   = regexp.MustCompile(`\[<[\w-]+>\]`)
	requiredPosRe   = regexp.MustCompile(`<[\w-]+>`)
)

// parseUsePositionals derives the positional-argument shape a Use string
// documents, e.g. "get <connector-id>" -> {required:1}, or
// "test-connection [<connector-id>]" -> {optional:1}.
func parseUsePositionals(use string) positionalSpec {
	var rest string
	if i := strings.IndexByte(use, ' '); i >= 0 {
		rest = use[i+1:]
	}
	stripped := flagValueHintRe.ReplaceAllString(rest, "")
	optional := len(optionalPosRe.FindAllString(stripped, -1))
	withoutOptional := optionalPosRe.ReplaceAllString(stripped, "")
	required := len(requiredPosRe.FindAllString(withoutOptional, -1))
	return positionalSpec{required: required, optional: optional}
}

// TestArgsUseConsistencyAcrossTree walks the ENTIRE real cobra tree rooted at
// rootCmd (not a hardcoded list) and checks that each runnable command's Args
// validator is consistent with what its Use string documents:
//
//   - Use documents N required positionals (e.g. "<connector-id>") => Args
//     must reject N-1 args and accept N. (We don't assert an upper bound here:
//     a couple of pre-existing single-positional commands - e.g.
//     "docs search <query>" - deliberately use cobra.MinimumNArgs(1) to allow
//     trailing words, which is a legitimate variant of "requires 1".)
//   - Use documents an optional positional (e.g. "[<connector-id>]") => Args
//     must accept both 0 and 1 args.
//   - Use documents no positional at all => Args must not itself REQUIRE one.
//     We deliberately do NOT assert the reverse (that Args rejects a stray
//     extra positional) as a tree-wide rule: cobra falls back to
//     cobra.ArbitraryArgs whenever Args is left unset, and the large majority
//     of zero-positional commands in this codebase (migrated and
//     pre-existing alike) leave Args unset. That is a real, pre-existing,
//     tree-wide gap, not something this migration introduced - asserting
//     rejection here would fail on dozens of unrelated commands. See the
//     discrepancy note in the task report for detail.
func TestArgsUseConsistencyAcrossTree(t *testing.T) {
	checked := 0
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, c := range cmd.Commands() {
			if c.Runnable() {
				checked++
				spec := parseUsePositionals(c.Use)
				path := c.CommandPath()
				switch {
				case spec.required > 0:
					if argsAccepts(c, spec.required-1) {
						t.Errorf("%s: Use %q documents %d required positional(s), but Args accepted %d arg(s)",
							path, c.Use, spec.required, spec.required-1)
					}
					if !argsAccepts(c, spec.required) {
						t.Errorf("%s: Use %q documents %d required positional(s), but Args rejected exactly that many",
							path, c.Use, spec.required)
					}
				case spec.optional > 0:
					if !argsAccepts(c, 0) {
						t.Errorf("%s: Use %q documents an optional positional, but Args rejected 0 args",
							path, c.Use)
					}
					if !argsAccepts(c, spec.optional) {
						t.Errorf("%s: Use %q documents an optional positional, but Args rejected %d arg(s)",
							path, c.Use, spec.optional)
					}
				default:
					if !argsAccepts(c, 0) {
						t.Errorf("%s: Use %q documents no positional, but Args requires at least one argument - Use should document it",
							path, c.Use)
					}
				}
			}
			walk(c)
		}
	}
	walk(rootCmd)
	if checked == 0 {
		t.Fatal("walked the command tree but found no runnable commands - rootCmd wiring must be broken")
	}
	t.Logf("checked %d runnable commands", checked)
}

// TestMigratedSingleResourceCommandsUsePositionalID pins the specific
// commands PR #50 migrated from an --id-style flag to a first positional
// argument. Each case is resolved from the real cobra tree; nothing here is
// synthesized.
func TestMigratedSingleResourceCommandsUsePositionalID(t *testing.T) {
	type tc struct {
		path         []string // command path segments under rootCmd
		retiredFlag  string   // flag name that must NOT be registered anymore
		optional     bool     // true: positional is optional ("[<...>]" / MaximumNArgs)
		mustKeepFlag []string // scope/parent flags that must remain registered
	}

	cases := []tc{
		{path: []string{"mcp", "servers", "get"}, retiredFlag: "connector-id", mustKeepFlag: []string{"app-id"}},
		{path: []string{"mcp", "servers", "update"}, retiredFlag: "connector-id", mustKeepFlag: []string{"app-id"}},
		{path: []string{"mcp", "servers", "update-credentials"}, retiredFlag: "connector-id", mustKeepFlag: []string{"app-id"}},
		{path: []string{"mcp", "servers", "delete"}, retiredFlag: "connector-id", mustKeepFlag: []string{"app-id"}},
		{path: []string{"mcp", "servers", "resync-tools"}, retiredFlag: "connector-id", mustKeepFlag: []string{"app-id"}},
		{path: []string{"mcp", "servers", "test-connection"}, retiredFlag: "connector-id", optional: true, mustKeepFlag: []string{"app-id"}},
		{path: []string{"mcp", "servers", "catalog", "get"}, retiredFlag: "catalog-id"},

		{path: []string{"mcp", "tools", "get"}, retiredFlag: "id", mustKeepFlag: []string{"app-id", "connector-id"}},
		{path: []string{"mcp", "tools", "approve"}, retiredFlag: "id", mustKeepFlag: []string{"app-id", "connector-id"}},
		{path: []string{"mcp", "tools", "delete"}, retiredFlag: "id", mustKeepFlag: []string{"app-id", "connector-id"}},
		{path: []string{"mcp", "tools", "history"}, retiredFlag: "id", mustKeepFlag: []string{"app-id", "connector-id"}},

		{path: []string{"mcp", "toolsets", "get"}, retiredFlag: "id", mustKeepFlag: []string{"app-id", "connector-id"}},
		{path: []string{"mcp", "toolsets", "update"}, retiredFlag: "id", mustKeepFlag: []string{"app-id", "connector-id"}},
		{path: []string{"mcp", "toolsets", "delete"}, retiredFlag: "id", mustKeepFlag: []string{"app-id", "connector-id"}},
		{path: []string{"mcp", "toolsets", "get-by-entitlement"}, retiredFlag: "app-entitlement-id", mustKeepFlag: []string{"app-id"}},
		{path: []string{"mcp", "toolsets", "requestable-connectors"}, retiredFlag: "user-id"},

		{path: []string{"tasks", "approve"}, retiredFlag: "task-id"},
		{path: []string{"tasks", "deny"}, retiredFlag: "task-id"},
		{path: []string{"tasks", "comment"}, retiredFlag: "task-id"},

		{path: []string{"accounts", "set-owner"}, retiredFlag: "app-user-id", mustKeepFlag: []string{"app-id", "user-id"}},
	}

	for _, c := range cases {
		c := c
		name := strings.Join(c.path, " ")
		t.Run(name, func(t *testing.T) {
			cmd := findCommand(t, rootCmd, c.path...)

			// Use must document exactly one positional (required, or the
			// documented optional form for test-connection).
			spec := parseUsePositionals(cmd.Use)
			if c.optional {
				if spec.optional != 1 || spec.required != 0 {
					t.Errorf("%s: Use %q should document exactly one OPTIONAL positional, got required=%d optional=%d",
						name, cmd.Use, spec.required, spec.optional)
				}
			} else if spec.required != 1 || spec.optional != 0 {
				t.Errorf("%s: Use %q should document exactly one REQUIRED positional, got required=%d optional=%d",
					name, cmd.Use, spec.required, spec.optional)
			}

			// Args must actually enforce that shape: never RunE, just the
			// validator.
			if !c.optional && argsAccepts(cmd, 0) {
				t.Errorf("%s: expected a REQUIRED positional, but Args accepted 0 args", name)
			}
			if !argsAccepts(cmd, 1) {
				t.Errorf("%s: expected Args to accept exactly 1 positional argument, but it rejected 1", name)
			}
			if argsAccepts(cmd, 2) {
				t.Errorf("%s: expected Args to reject a 2nd positional (this addresses one resource by one id), but it accepted 2", name)
			}

			// The retired flag must be gone - own or inherited.
			if hasFlag(cmd, c.retiredFlag) {
				t.Errorf("%s: retired flag --%s is still registered (own or inherited); the id must be positional-only now", name, c.retiredFlag)
			}

			// Parent/scope flags must have survived the migration untouched.
			for _, f := range c.mustKeepFlag {
				if !hasFlag(cmd, f) {
					t.Errorf("%s: expected scope flag --%s to remain registered, but it's gone", name, f)
				}
			}
		})
	}
}

// TestCollectionCommandsKeepFlagIDsAndNoPositional guards against
// over-correction: collection/create/relationship commands were NOT part of
// the id->positional migration (they don't address one existing resource by
// its own id) and must keep their ids as flags, with no positional declared
// in Use.
func TestCollectionCommandsKeepFlagIDsAndNoPositional(t *testing.T) {
	cases := []struct {
		path  []string
		flags []string
	}{
		{path: []string{"mcp", "tools", "list"}, flags: []string{"app-id", "connector-id"}},
		{path: []string{"mcp", "tools", "search"}, flags: []string{"app-id", "connector-id"}},
		{path: []string{"mcp", "toolsets", "list"}, flags: []string{"app-id", "connector-id"}},
		{path: []string{"mcp", "toolsets", "create"}, flags: []string{"app-id", "connector-id"}},
		{path: []string{"mcp", "bindings", "create"}, flags: []string{"app-id", "connector-id", "toolset-id"}},
		{path: []string{"mcp", "servers", "list"}, flags: []string{"app-id"}},
		{path: []string{"mcp", "servers", "register"}, flags: []string{"app-id"}},
		{path: []string{"tasks", "list"}, flags: nil},
		{path: []string{"accounts", "list"}, flags: []string{"app-id"}},
	}

	for _, c := range cases {
		c := c
		name := strings.Join(c.path, " ")
		t.Run(name, func(t *testing.T) {
			cmd := findCommand(t, rootCmd, c.path...)

			spec := parseUsePositionals(cmd.Use)
			if spec.required > 0 || spec.optional > 0 {
				t.Errorf("%s: Use %q declares a positional argument, but this collection/create/relationship command should keep ids as flags",
					name, cmd.Use)
			}

			for _, f := range c.flags {
				if !hasFlag(cmd, f) {
					t.Errorf("%s: expected flag --%s to remain registered (own or inherited), but it's gone", name, f)
				}
			}
		})
	}
}

// TestStrayPositionalRejectedOnCollectionCommand exercises defect 2
// end-to-end: a runnable command that leaves Args unset falls back to
// cobra.ArbitraryArgs, so `c1i mcp servers list somejunk` used to silently
// ignore the stray argument and exit 0. attachSubcommandGuards closes that
// gap tree-wide by defaulting a nil Args to cobra.NoArgs; production picks
// this up via Run() (see cmd/errors.go), and tests call it explicitly here
// since they drive rootCmd.ExecuteContext directly, bypassing Run().
//
// This drives the real rootCmd tree (not a synthetic command) and asserts
// the actual process exit code via exitCode(), not just "an error occurred" —
// a stray positional must be a usage error (2), not a generic one (1).
func TestStrayPositionalRejectedOnCollectionCommand(t *testing.T) {
	attachSubcommandGuards(rootCmd)

	cases := [][]string{
		// --app-id is supplied so this doesn't fail for the unrelated reason
		// of a missing required flag (requireNonEmpty) — that would return
		// exitUsage too, but for the wrong reason, masking whether the stray
		// positional itself was actually rejected before RunE ever ran.
		{"mcp", "servers", "list", "somejunk", "--app-id", "app_fake"},
		{"users", "list", "somejunk"},
	}
	for _, args := range cases {
		args := args
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("C1I_URL", "https://example.invalid")
			var out bytes.Buffer
			rootCmd.SetOut(&out)
			rootCmd.SetErr(&out)
			rootCmd.SetArgs(args)

			err := rootCmd.ExecuteContext(context.Background())
			if err == nil {
				t.Fatalf("expected the stray positional %q to be rejected, got nil error (silently accepted)", args[len(args)-1])
			}
			if got, want := exitCode(err), exitUsage; got != want {
				t.Errorf("exitCode(%v) = %d, want %d (exitUsage)", err, got, want)
			}
		})
	}
}

// TestLegitimatePositionalStillAccepted proves the tree-wide guard above
// never clobbers a command that legitimately takes a positional:
// `users get <id>` already declares cobra.ExactArgs(1) (Args is non-nil), so
// attachSubcommandGuards must leave it untouched — it accepts exactly one
// argument, rejects zero, and rejects two.
func TestLegitimatePositionalStillAccepted(t *testing.T) {
	attachSubcommandGuards(rootCmd)

	cmd := findCommand(t, rootCmd, "users", "get")
	if cmd.Args == nil {
		t.Fatal("users get: Args is nil - it should already declare cobra.ExactArgs(1) before the tree-wide guard ever runs")
	}
	if argsAccepts(cmd, 0) {
		t.Error("users get: expected 0 positionals to be rejected (id is required)")
	}
	if !argsAccepts(cmd, 1) {
		t.Error("users get: expected exactly 1 positional to be accepted")
	}
	if argsAccepts(cmd, 2) {
		t.Error("users get: expected 2 positionals to be rejected")
	}
}
