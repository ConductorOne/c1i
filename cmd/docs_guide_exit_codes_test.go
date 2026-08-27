package cmd

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
)

// Checks the guides' exit-code claims against exitCode(), but only where an
// HTTP status shares the line: a client-side failure has no status to check
// against.
var (
	// Covers all three forms the guides use: "(exit N)", "exits N", "exit N".
	guideExitClaimRe = regexp.MustCompile(`\bexits? (\d)\b`)
	guideStatusRe    = regexp.MustCompile(`\b(4\d\d|5\d\d)\b`)
)

func TestGuideExitCodeClaimsMatchTheTaxonomy(t *testing.T) {
	checked := 0
	for name, body := range docsGuides {
		for _, line := range strings.Split(body, "\n") {
			claim := guideExitClaimRe.FindStringSubmatch(line)
			if claim == nil {
				continue
			}
			status := guideStatusRe.FindString(line)
			if status == "" {
				continue // no status on this line; nothing to check it against
			}
			code, _ := strconv.Atoi(status)
			want := exitCode(&client.APIError{StatusCode: code})
			got, _ := strconv.Atoi(claim[1])
			checked++
			if got != want {
				t.Errorf("guide %q claims (exit %d) beside a %d, but a %d maps to exit %d:\n  %s",
					name, got, code, code, want, strings.TrimSpace(line))
			}
		}
	}
	// Floor measured, not guessed: four such pairs exist today.
	if checked < 4 {
		t.Fatalf("only %d status/exit pairs found across the guides, want at least 4; either the extraction regressed or a guide dropped a pair -- if the latter, lower the floor", checked)
	}
}
