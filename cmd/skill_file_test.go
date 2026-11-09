package cmd

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestSkillFileDefersToAgentsDoc keeps .claude/commands/c1i.md a pointer to
// cmd/agents.md. Its earlier copies of the exit-code table, global-flag facts
// and command list drifted from the code, so it may carry none of them:
// it must name `docs agents`, hold no markdown table, backtick no more than
// two top-level command names, and stay under 40 lines.
func TestSkillFileDefersToAgentsDoc(t *testing.T) {
	b, err := os.ReadFile("../.claude/commands/c1i.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)

	if !strings.Contains(text, "docs agents") {
		t.Error("c1i.md must point at `go run . docs agents`, the authoritative guide")
	}
	if tableRow := regexp.MustCompile(`(?m)^\s*\|.*\|\s*$`); tableRow.MatchString(text) {
		t.Error("c1i.md contains a markdown table (an exit-code table?); cmd/agents.md owns those")
	}
	var named []string
	for _, c := range rootCmd.Commands() {
		if regexp.MustCompile("`" + regexp.QuoteMeta(c.Name()) + "`").MatchString(text) {
			named = append(named, c.Name())
		}
	}
	if len(named) > 2 {
		t.Errorf("c1i.md enumerates command groups %v; use `--help` instead of listing them", named)
	}
	if n := strings.Count(text, "\n"); n > 40 {
		t.Errorf("c1i.md is %d lines; it is a pointer, not a second copy of agents.md (max 40)", n)
	}
}
