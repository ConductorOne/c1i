package cmd

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// --dry-run must refuse exactly what a real run refuses. The walk below holds
// every mutating command to that for each id argument it takes, using the
// mutationTable rows as the known-valid invocation (and their `reads` as the
// pre-read metadata, which TestMutationTable checks against the wire).

// badIDTokens are refused (exit 2, nothing sent) on any id argument.
var badIDTokens = []string{"", " ", ".", "..", " .. ", "%2E"}

const unknownID = "zz-unknown-id"

// idArg is one id-valued argument of a command: a positional slot (pos >= 0)
// or a flag.
type idArg struct {
	label, flag string
	pos         int
}

// mutatingIDArgs returns c's id positionals and its string/stringArray id
// flags, whether or not a row's invocation sets them.
func mutatingIDArgs(c *cobra.Command) []idArg {
	var out []idArg
	c.Flags().VisitAll(func(f *pflag.Flag) {
		if hasTestIDSuffix(f.Name) && (f.Value.Type() == "string" || f.Value.Type() == "stringArray") {
			out = append(out, idArg{label: "--" + f.Name, flag: f.Name, pos: -1})
		}
	})
	for _, i := range idPositionalIndexes(c.Use) {
		out = append(out, idArg{label: fmt.Sprintf("positional #%d", i), pos: i})
	}
	return out
}

// flagArgName returns the flag name of "--name" or "--name=v", or "".
func flagArgName(a string) string {
	if !strings.HasPrefix(a, "--") {
		return ""
	}
	name, _, _ := strings.Cut(a[2:], "=")
	return name
}

// withIDArg returns tail (the invocation after the command path) with id set to
// v: a positional replaced in its slot, a flag's occurrences replaced by one.
func withIDArg(t *testing.T, tail []string, id idArg, v string) []string {
	t.Helper()
	if id.pos >= 0 {
		if id.pos >= len(tail) || flagArgName(tail[id.pos]) != "" {
			t.Fatalf("positional #%d is not first in %v", id.pos, tail)
		}
		out := slices.Clone(tail)
		out[id.pos] = v
		return out
	}
	var out []string
	for i := 0; i < len(tail); i++ {
		if flagArgName(tail[i]) == id.flag {
			if !strings.Contains(tail[i], "=") {
				i++
			}
			continue
		}
		out = append(out, tail[i])
	}
	return append(out, "--"+id.flag, v)
}

// rowIDArgsReplaced replaces, by v, each id argument the row's own invocation
// sets, and returns the new tail and how many it replaced.
func rowIDArgsReplaced(t *testing.T, c *cobra.Command, tail []string, v string) ([]string, int) {
	t.Helper()
	n := 0
	for _, id := range mutatingIDArgs(c) {
		set := id.pos >= 0 && id.pos < len(tail) && flagArgName(tail[id.pos]) == ""
		for _, a := range tail {
			if id.pos < 0 && flagArgName(a) == id.flag {
				set = true
			}
		}
		if set {
			tail = withIDArg(t, tail, id, v)
			n++
		}
	}
	return tail, n
}

// mutatingParityGaps names each mutating command in cmds that has no table row,
// and so no parity coverage.
func mutatingParityGaps(cmds []*cobra.Command, rows []mutationRow) []string {
	have := map[string]bool{}
	for _, r := range rows {
		have["c1i "+r.cmd] = true
	}
	var out []string
	for _, c := range cmds {
		if isMutating(c) && !have[c.CommandPath()] {
			out = append(out, c.CommandPath()+" is mutating but has no mutationTable row, so dry-run parity is untested")
		}
	}
	return out
}

func TestDryRunParityCoversEveryMutatingCommand(t *testing.T) {
	if gaps := mutatingParityGaps(blankCheckCommands(), mutationTable); len(gaps) > 0 {
		t.Error(strings.Join(gaps, "\n"))
	}
}

// A planted mutating command with no row must be reported, one with a row not.
func TestDryRunParityGuardSelfTest(t *testing.T) {
	planted := &cobra.Command{Use: "planted-zz <thing-id>", Run: func(*cobra.Command, []string) {}}
	markMutating(planted)
	rootCmd.AddCommand(planted)
	t.Cleanup(func() { rootCmd.RemoveCommand(planted) })
	if gaps := mutatingParityGaps(blankCheckCommands(), mutationTable); len(gaps) != 1 || !strings.Contains(gaps[0], "planted-zz") {
		t.Errorf("gaps = %v, want exactly the planted command", gaps)
	}
	rows := append(slices.Clone(mutationTable), mutationRow{cmd: "planted-zz"})
	if gaps := mutatingParityGaps(blankCheckCommands(), rows); len(gaps) != 0 {
		t.Errorf("a row did not cover the command: %v", gaps)
	}
}

// TestDryRunParityBadIDs: every mutating command, every id argument, every bad
// token: a real run and a --dry-run both exit 2 with no request sent.
func TestDryRunParityBadIDs(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	checked := 0
	for _, row := range mutationTable {
		leaf, _, err := rootCmd.Find(strings.Fields(row.cmd))
		if err != nil || !isMutating(leaf) {
			t.Fatalf("%s: not a mutating command (%v)", row.cmd, err)
		}
		for _, id := range mutatingIDArgs(leaf) {
			for _, tok := range badIDTokens {
				for _, dry := range []bool{false, true} {
					checked++
					t.Run(fmt.Sprintf("%s %s %q dry=%v", row.cmd, id.label, tok, dry), func(t *testing.T) {
						clearViperOverrides(t, "dry_run")
						full := row.argv(t)
						n := len(strings.Fields(row.cmd))
						args := append(slices.Clone(full[:n]), withIDArg(t, full[n:], id, tok)...)
						if dry {
							args = append(args, "--dry-run")
						}
						s := newMutServer(t, tableRoute(row.body))
						r := runMutationCLI(t, s.srv, 0, nil, args...)
						if r.exit != exitUsage {
							t.Fatalf("args %v: exit %d (%v), want %d", args, r.exit, r.err, exitUsage)
						}
						if r.stdout != "" {
							t.Errorf("args %v: printed %q", args, r.stdout)
						}
						if got := s.requests(); len(got) != 0 {
							t.Errorf("args %v: requests sent: %v", args, got)
						}
					})
				}
			}
		}
	}
	if checked < 1000 {
		t.Fatalf("checked only %d cases; the walk is not seeing the tree", checked)
	}
}

// TestDryRunParityUnknownID: with every id the invocation sets replaced by an
// id that does not exist, a command with a pre-read exits 4 in both modes; one
// without exits 4 on the write (real) and previews, sending nothing (dry-run).
func TestDryRunParityUnknownID(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	missing := func(*http.Request, int) mutReply {
		return mutReply{status: http.StatusNotFound, body: `{"code":5,"message":"not found"}`}
	}
	checked := 0
	for _, row := range mutationTable {
		leaf, _, err := rootCmd.Find(strings.Fields(row.cmd))
		if err != nil {
			t.Fatal(err)
		}
		t.Run(row.cmd, func(t *testing.T) {
			full := row.argv(t)
			n := len(strings.Fields(row.cmd))
			tail, set := rowIDArgsReplaced(t, leaf, full[n:], unknownID)
			if set == 0 {
				t.Skip("takes no id")
			}
			checked++
			args := append(slices.Clone(full[:n]), tail...)

			clearViperOverrides(t, "dry_run")
			real := newMutServer(t, missing)
			rr := runMutationCLI(t, real.srv, 0, nil, args...)
			if rr.exit != exitNotFound {
				t.Errorf("real run: exit %d (%v), want %d", rr.exit, rr.err, exitNotFound)
			}
			if w := nonGETRequests(real); row.reads > 0 && len(w) != 0 {
				t.Errorf("real run wrote after a failed pre-read: %+v", w)
			}

			dry := newMutServer(t, missing)
			dr := runMutationCLI(t, dry.srv, 0, nil, append(slices.Clone(args), "--dry-run")...)
			if w := nonGETRequests(dry); len(w) != 0 {
				t.Errorf("dry run sent %+v", w)
			}
			if row.reads > 0 {
				if dr.exit != exitNotFound || dr.stdout != "" || dr.stderr != rr.stderr {
					t.Errorf("dry run: exit %d stdout %q stderr %q, want exit %d, no preview, stderr %q", dr.exit, dr.stdout, dr.stderr, exitNotFound, rr.stderr)
				}
				return
			}
			if dr.err != nil || !strings.Contains(dr.stdout, `"dryRun": true`) {
				t.Errorf("dry run: exit %d (%v) stdout %q, want a preview", dr.exit, dr.err, dr.stdout)
			}
			if got := dry.requests(); len(got) != 0 {
				t.Errorf("dry run with no pre-read sent %+v", got)
			}
		})
	}
	if checked < 60 {
		t.Fatalf("checked only %d commands", checked)
	}
}
