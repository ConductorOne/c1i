package cmd

import (
	"strings"
	"testing"
)

// <template-id> on `mcp classifiers templates add-rule` only ever reaches the
// request body; the classifier id is in the path. It is an id, so it gets the
// same refusal as an *-id flag.
func TestDotSegmentTemplateIDRefused(t *testing.T) {
	t.Run("dot", func(t *testing.T) {
		t.Setenv("C1I_URL", mutTestURL)
		srv := newMutServer(t, fixedReply(200, `{"classifier":{"id":"cls-1"}}`))
		res := runMutationCLI(t, srv.srv, 0, nil, "mcp", "classifiers", "templates", "add-rule", "cls-1", "..")
		if res.exit != exitUsage {
			t.Fatalf("exit = %d, err = %v; want %d", res.exit, res.err, exitUsage)
		}
		if res.err == nil || !strings.Contains(res.err.Error(), `argument ".." is not an id`) {
			t.Errorf("err = %v, want the dot-segment positional refusal", res.err)
		}
		if got := srv.requests(); len(got) != 0 {
			t.Errorf("requests sent: %v (run - started a mutation)", got)
		}
	})

	t.Run("dry-run", func(t *testing.T) {
		t.Setenv("C1I_URL", mutTestURL)
		withDryRun(t)
		srv := newMutServer(t, fixedReply(200, `{}`))
		res := runMutationCLI(t, srv.srv, 0, nil, "mcp", "classifiers", "templates", "add-rule", "cls-1", ".")
		if res.exit != exitUsage {
			t.Fatalf("exit = %d, err = %v; want %d", res.exit, res.err, exitUsage)
		}
		if res.stdout != "" {
			t.Errorf("stdout = %q, want no preview", res.stdout)
		}
	})

	t.Run("dotted template id still previews", func(t *testing.T) {
		t.Setenv("C1I_URL", mutTestURL)
		withDryRun(t)
		srv := newMutServer(t, fixedReply(200, `{}`))
		res := runMutationCLI(t, srv.srv, 0, nil, "mcp", "classifiers", "templates", "add-rule", "cls-1", "tmpl.1")
		if res.exit != 0 {
			t.Fatalf("exit = %d, err = %v; want 0", res.exit, res.err)
		}
		if !strings.Contains(res.stdout, `"templateId": "tmpl.1"`) {
			t.Errorf("preview = %q, want the template id in the body", res.stdout)
		}
	})
}
