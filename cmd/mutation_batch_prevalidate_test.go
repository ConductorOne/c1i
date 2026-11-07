package cmd

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

func newTestBatchCmdWith(body func(id string) any, opts ...mutationOpt) *cobra.Command {
	c := &cobra.Command{
		Use:  "approve <id>...",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ms := make([]mutation, len(args))
			for i, id := range args {
				ms[i] = mutation{Method: http.MethodPost, Path: client.Path("/api/v1/tools/%s", id), Body: body(id)}
			}
			return sendMutationBatch(cmd, ms, "id", func(i int) string { return args[i] }, append(opts, withBatchWording("approved", "tool"))...)
		},
	}
	markMutating(c)
	return c
}

// wantR17 asserts R17's shape: nothing sent or printed, a per-id error for
// bad, then a summary with every argument not attempted.
func wantR17(t *testing.T, r mutResult, s *mutServer, exit int, bad string, args ...string) {
	t.Helper()
	if r.exit != exit || r.stdout != "" || len(s.requests()) != 0 {
		t.Errorf("exit %d stdout %q requests %d; want %d, nothing printed or sent", r.exit, r.stdout, len(s.requests()), exit)
	}
	objs := r.stderrObjects(t)
	if len(objs) != 2 || objs[0]["id"] != bad || objs[0]["applied"] != false {
		t.Fatalf("stderr = %q, want a per-id error for %q and a summary", r.stderr, bad)
	}
	sum := objs[1]
	if sum["applied"] != false || sum["succeeded"] != float64(0) || sum["failed"] != float64(0) || fmt.Sprint(sum["notAttempted"]) != fmt.Sprint(args) {
		t.Errorf("summary = %v", sum)
	}
}

func TestBatchPreReadFailureReportsR17(t *testing.T) {
	s := newMutServer(t, func(r *http.Request, _ int) mutReply { return toolReply(r, 200) })
	lookup := preRead{Run: func(_ context.Context, _ *client.Client, m *mutation) ([]resolvedID, error) {
		if strings.HasSuffix(m.Path, "/b") {
			return nil, fmt.Errorf("API error: %w", &client.APIError{StatusCode: 404, Method: http.MethodGet, Path: "/api/v1/tools/b"})
		}
		return nil, nil
	}}
	c := newTestBatchCmdWith(func(id string) any { return map[string]any{"id": id} }, withPreRead(lookup))
	r := runHelperCmd(t, c, s, context.Background(), nil, "json", "a", "b", "c")
	wantR17(t, r, s, exitNotFound, "b", "a", "b", "c")
}

// R27a: a body encoding to null is a programming error (exit 1), not usage.
func TestBatchNullBodyExitsOne(t *testing.T) {
	s := newMutServer(t, func(r *http.Request, _ int) mutReply { return toolReply(r, 200) })
	c := newTestBatchCmdWith(func(string) any { return (*struct{})(nil) })
	r := runHelperCmd(t, c, s, context.Background(), nil, "json", "a", "b")
	wantR17(t, r, s, exitError, "a", "a", "b")
}
