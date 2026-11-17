package cmd

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// dotIDExemptions names an id argument that legitimately takes "." with the
// reason, keyed "<command path> --flag" or "<command path> <positional>". None
// exists; an entry is a deliberate edit.
var dotIDExemptions = map[string]string{}

var testIDSuffixes = []string{"-id", "-ids", "-uid", "-uids"}

func hasTestIDSuffix(name string) bool {
	return slices.ContainsFunc(testIDSuffixes, func(s string) bool { return strings.HasSuffix(name, s) })
}

// anyPositionalRe matches each documented positional ("<x>", "[<x>]", "[x]") of
// a Use string, so a position's index counts the non-id ones too.
var anyPositionalRe = regexp.MustCompile(`<([\w-]+)>|\[([\w-]+)\]`)

// idPositionalIndexes returns, per 0-based position, the documented id
// positionals of use.
func idPositionalIndexes(use string) []int {
	rest := ""
	if i := strings.IndexByte(use, ' '); i >= 0 {
		rest = flagValueHintRe.ReplaceAllString(use[i+1:], "")
	}
	var out []int
	for i, m := range anyPositionalRe.FindAllStringSubmatch(rest, -1) {
		if m[1] != "" && hasTestIDSuffix(m[1]) {
			out = append(out, i)
		}
	}
	return out
}

// TestEveryIDArgumentRefusesDotBeforeAnyRequest walks every runnable command
// and, for each id flag and id positional, passes "." with everything else
// valid: exit 2 with the dot-segment refusal and no request, real and
// --dry-run. A new id argument is covered by its name alone.
func TestEveryIDArgumentRefusesDotBeforeAnyRequest(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	checked := 0
	var ln refusalListener
	for _, c := range blankCheckCommands() {
		targets := mutatingIDArgs(c)
		for _, tg := range targets {
			for _, v := range []string{".", "..", " .. "} {
				for _, dry := range []bool{false, true} {
					checked++
					name := fmt.Sprintf("%s %s %q dry=%v", c.CommandPath(), tg.label, v, dry)
					if reason, ok := dotIDExemptions[c.CommandPath()+" "+tg.label]; ok && reason != "" {
						continue
					}
					t.Run(name, func(t *testing.T) {
						clearViperOverrides(t, "dry_run") // another test's override would hide --dry-run
						args := requiredInvocation(t, c, "x", func(n string) bool { return n == tg.flag }, "string", "stringArray")
						if tg.flag != "" {
							args = append(args, "--"+tg.flag, v)
						}
						if tg.pos >= 0 {
							// requiredInvocation fills the required positionals; an
							// optional one (or the Nth) is set at its slot.
							at := len(strings.Fields(strings.TrimPrefix(c.CommandPath(), rootCmd.Name()+" "))) + tg.pos
							if at < len(args) && !strings.HasPrefix(args[at], "--") {
								args[at] = v
							} else {
								args = slices.Insert(args, min(at, len(args)), v)
							}
						}
						if dry {
							args = append(args, "--dry-run")
						}
						srv, requests := ln.next(t, fixedReply(200, "{}"))
						res := runMutationCLI(t, srv, 0, nil, args...)
						if res.exit != exitUsage {
							t.Fatalf("args %v: exit = %d (err %v), want %d", args, res.exit, res.err, exitUsage)
						}
						// Only the central rule's refusal counts: the later path guard
						// would mean the id reached request building.
						if res.err == nil || !strings.Contains(res.err.Error(), "is not an id") {
							t.Errorf("args %v: err = %v, want a dot-segment id refusal", args, res.err)
						}
						if got := requests(); len(got) != 0 {
							t.Errorf("args %v: requests sent: %v", args, got)
						}
					})
				}
			}
		}
	}
	if checked < 100 {
		t.Fatalf("checked only %d id arguments; the walk is not seeing the tree", checked)
	}
}
