package cmd

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// CI pins each gate tool; CLAUDE.md must run the same version, or a local
// pass proves nothing about CI.
func TestCIToolVersionsMatchClaudeMd(t *testing.T) {
	ci := readRepoFile(t, ".github/workflows/ci.yaml")
	for _, p := range ciToolVersionProblems(ci, readRepoFile(t, "CLAUDE.md")) {
		t.Error(p)
	}
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile("../" + rel)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func ciToolVersionProblems(ci, doc string) (problems []string) {
	tools := []struct{ name, module, ciPattern string }{
		{"golangci-lint", "github.com/golangci/golangci-lint/v2/cmd/golangci-lint", `golangci-lint-action@[^\n]*\n\s*with:\s*\n\s*version:\s*(v[\w.]+)`},
		{"gosec", "github.com/securego/gosec/v2/cmd/gosec", `gosec@(v[\w.]+)`},
		{"gitleaks", "github.com/zricethezav/gitleaks/v8", `gitleaks/v8@(v[\w.]+)`},
		{"govulncheck", "golang.org/x/vuln/cmd/govulncheck", `govulncheck@(v[\w.]+)`},
	}
	for _, tool := range tools {
		m := regexp.MustCompile(tool.ciPattern).FindStringSubmatch(ci)
		if m == nil {
			problems = append(problems, tool.name+": no pinned version found in ci.yaml")
			continue
		}
		found := regexp.MustCompile(regexp.QuoteMeta(tool.module)+`@(\S+)`).FindAllStringSubmatch(doc, -1)
		if len(found) == 0 {
			problems = append(problems, tool.name+": CLAUDE.md does not run "+tool.module+"@"+m[1])
		}
		for _, f := range found {
			if f[1] != m[1] {
				problems = append(problems, tool.name+": CLAUDE.md names "+f[1]+", ci.yaml pins "+m[1])
			}
		}
	}
	// The lint timeout drifts too: a run that passes locally can time out in CI.
	if m := regexp.MustCompile(`args:\s*--timeout=(\S+)`).FindStringSubmatch(ci); m == nil {
		problems = append(problems, "golangci-lint: no --timeout found in ci.yaml")
	} else if d := regexp.MustCompile(`golangci-lint@\S+ run --timeout=(\S+)`).FindStringSubmatch(doc); d == nil || d[1] != m[1] {
		problems = append(problems, "golangci-lint: CLAUDE.md --timeout differs from ci.yaml's "+m[1])
	}
	return problems
}

func TestCIToolVersionCheckCatchesDrift(t *testing.T) {
	ci := readRepoFile(t, ".github/workflows/ci.yaml")
	doc := readRepoFile(t, "CLAUDE.md")
	for _, mutate := range []func(string) string{
		func(d string) string { return regexp.MustCompile(`gosec@v[\w.]+`).ReplaceAllString(d, "gosec@v0.0.1") },
		func(d string) string {
			return regexp.MustCompile(`gitleaks/v8@v[\w.]+`).ReplaceAllString(d, "gitleaks/v8")
		},
		func(d string) string { return strings.Replace(d, "run --timeout=3m", "run --timeout=5m", 1) },
	} {
		if len(ciToolVersionProblems(ci, mutate(doc))) == 0 {
			t.Error("a drifted CLAUDE.md passed the version check")
		}
	}
}
