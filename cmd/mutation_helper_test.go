package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// Synthetic commands drive the helper's batch, sequence and pre-read paths,
// which no Phase A command exercises.

func newTestBatchCmd() *cobra.Command {
	c := &cobra.Command{
		Use:  "approve <id>...",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ms := make([]mutation, len(args))
			for i, id := range args {
				ms[i] = mutation{Method: http.MethodPost, Path: client.Path("/api/v1/tools/%s", id), Body: map[string]any{"id": id}}
			}
			return sendMutationBatch(cmd, ms, "id", func(i int) string { return args[i] }, withBatchWording("approved", "tool"))
		},
	}
	markMutating(c)
	return c
}

func newTestSequenceCmd() *cobra.Command {
	idOf := func(data []byte) (string, error) {
		var v struct{ ID string }
		if err := json.Unmarshal(data, &v); err != nil || v.ID == "" {
			return "", fmt.Errorf("no id in %s", data)
		}
		return v.ID, nil
	}
	c := &cobra.Command{
		Use: "create",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return sendMutationSequence(cmd, []mutationStep{
				{Build: func([]string) mutation {
					return mutation{Method: http.MethodPost, Path: "/api/v1/types", Body: map[string]any{"n": 1}}
				}, Placeholder: "NEW_TYPE_ID", ResultID: idOf},
				{Build: func(ids []string) mutation {
					return mutation{Method: http.MethodPost, Path: client.Path("/api/v1/types/%s/things", ids[0]), Body: map[string]any{}}
				}, Placeholder: "NEW_THING_ID", ResultID: idOf},
				{Build: func(ids []string) mutation {
					return mutation{Method: http.MethodPost, Path: "/api/v1/ents", Body: map[string]any{"typeId": ids[0], "thingId": ids[1]}}
				}},
			}, "requests are sent in order; NEW_TYPE_ID/NEW_THING_ID stand in for ids the preceding response returns")
		},
	}
	markMutating(c)
	return c
}

func newTestPreReadCmd(onDryRun bool) *cobra.Command {
	c := &cobra.Command{
		Use: "reassign",
		RunE: func(cmd *cobra.Command, _ []string) error {
			body := map[string]any{}
			return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/tasks/t1/action/reassign", Body: body},
				withPreRead(preRead{OnDryRun: onDryRun, Run: func(ctx context.Context, c *client.Client, m *mutation) ([]resolvedID, error) {
					if _, err := c.Get(ctx, "/api/v1/tasks/t1", nil); err != nil {
						return nil, err
					}
					m.Body.(map[string]any)["policyStepId"] = "P1"
					return []resolvedID{{Name: "policyStepId", Label: "policy step", ID: "P1"}}, nil
				}}))
		},
	}
	markMutating(c)
	return c
}

// runHelperCmd runs c directly with format as C1I_ERROR_FORMAT, rendering a
// failure the way Run does.
func runHelperCmd(t *testing.T, c *cobra.Command, srv *mutServer, ctx context.Context, out io.Writer, format string, args ...string) mutResult {
	t.Helper()
	t.Setenv("C1I_URL", mutTestURL)
	t.Setenv("C1I_ERROR_FORMAT", format)
	orig := newClient
	newClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		if srv == nil {
			t.Error("a client was built")
			return nil, &client.AuthError{Err: io.EOF}
		}
		return client.NewForTesting(srv.srv.URL, srv.srv.Client(), client.WithMaxRetries(0)), nil
	}
	t.Cleanup(func() { newClient = orig })
	var stdout, stderr bytes.Buffer
	if out == nil {
		out = &stdout
	}
	c.SetOut(out)
	c.SetErr(&stderr)
	c.SetContext(ctx)
	err := c.RunE(c, args)
	if err != nil {
		writeError(&stderr, err, format, c)
	}
	return mutResult{stdout: stdout.String(), stderr: stderr.String(), exit: exitCode(err), err: err}
}

func toolReply(r *http.Request, status int) mutReply {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/tools/")
	return mutReply{status: status, body: `{"tool":{"id":"` + id + `","state":"APPROVED"}}`}
}

func TestBatchPartialFailure(t *testing.T) {
	s := newMutServer(t, func(r *http.Request, n int) mutReply {
		if n == 2 {
			return mutReply{status: 404, body: `{"code":5}`}
		}
		return toolReply(r, 200)
	})
	r := runHelperCmd(t, newTestBatchCmd(), s, context.Background(), nil, "json", "a", "b")
	if r.stdout != `{"id":"a","state":"APPROVED"}`+"\n" {
		t.Errorf("stdout = %q, want one compact line for a", r.stdout)
	}
	if r.exit != exitNotFound {
		t.Errorf("exit = %d, want 4", r.exit)
	}
	objs := r.stderrObjects(t)
	if len(objs) != 2 {
		t.Fatalf("stderr = %q, want a per-id error and a summary", r.stderr)
	}
	if per := objs[0]; per["id"] != "b" || per["applied"] != false || per["status"] != float64(404) {
		t.Errorf("per-id error = %v", per)
	}
	sum := objs[1]
	if sum["applied"] != true || sum["succeeded"] != float64(1) || sum["failed"] != float64(1) || fmt.Sprint(sum["notAttempted"]) != "[]" {
		t.Errorf("summary = %v", sum)
	}
	for _, k := range []string{"id", "status", "method", "path", "body"} {
		if _, ok := sum[k]; ok {
			t.Errorf("summary carries %q: %v", k, sum)
		}
	}
}

func TestBatchTextMode(t *testing.T) {
	s := newMutServer(t, func(r *http.Request, n int) mutReply {
		if n == 2 {
			return mutReply{status: 404}
		}
		return toolReply(r, 200)
	})
	r := runHelperCmd(t, newTestBatchCmd(), s, context.Background(), nil, "text", "a", "b")
	want := "tool b: API error: API POST /api/v1/tools/b returned 404\nError: approved 1 of 2 tools; 1 failed: API error: API POST /api/v1/tools/b returned 404\n"
	if r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
}

func TestBatchWriteFailureAborts(t *testing.T) {
	s := newMutServer(t, func(r *http.Request, _ int) mutReply { return toolReply(r, 200) })
	r := runHelperCmd(t, newTestBatchCmd(), s, context.Background(), failingWriter{}, "json", "a", "b")
	if r.exit != exitError || len(s.requests()) != 1 {
		t.Errorf("exit %d after %d requests, want 1 and b never sent", r.exit, len(s.requests()))
	}
	objs := r.stderrObjects(t)
	if len(objs) != 2 || objs[0]["id"] != "a" || objs[0]["applied"] != true {
		t.Fatalf("stderr = %q", r.stderr)
	}
	if sum := objs[1]; sum["applied"] != true || fmt.Sprint(sum["notAttempted"]) != "[b]" {
		t.Errorf("summary = %v", sum)
	}
}

func TestBatchNonJSONContinues(t *testing.T) {
	s := newMutServer(t, func(r *http.Request, n int) mutReply {
		if n == 1 {
			return mutReply{body: `<html>`}
		}
		return toolReply(r, 200)
	})
	r := runHelperCmd(t, newTestBatchCmd(), s, context.Background(), nil, "json", "a", "b")
	if r.stdout != `{"id":"b","state":"APPROVED"}`+"\n" || r.exit != exitServer {
		t.Errorf("exit %d stdout %q, want 6 and b's line", r.exit, r.stdout)
	}
	if per := r.stderrObjects(t)[0]; per["id"] != "a" || per["applied"] != true {
		t.Errorf("per-id error = %v", per)
	}
}

func TestBatchPreValidation(t *testing.T) {
	s := newMutServer(t, func(r *http.Request, _ int) mutReply { return toolReply(r, 200) })
	r := runHelperCmd(t, newTestBatchCmd(), s, context.Background(), nil, "json", "a", "", "c")
	if r.exit != exitUsage || r.stdout != "" || len(s.requests()) != 0 {
		t.Errorf("exit %d stdout %q requests %d; want 2, nothing printed or sent", r.exit, r.stdout, len(s.requests()))
	}
	objs := r.stderrObjects(t)
	if len(objs) != 2 || objs[0]["id"] != "" || objs[0]["applied"] != false {
		t.Fatalf("stderr = %q", r.stderr)
	}
	sum := objs[1]
	if sum["applied"] != false || sum["succeeded"] != float64(0) || sum["failed"] != float64(0) || fmt.Sprint(sum["notAttempted"]) != "[a  c]" {
		t.Errorf("summary = %v", sum)
	}
}

func TestBatchCancelStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := newMutServer(t, func(r *http.Request, n int) mutReply {
		if n == 2 {
			cancel()
			<-r.Context().Done()
		}
		return toolReply(r, 200)
	})
	r := runHelperCmd(t, newTestBatchCmd(), s, ctx, nil, "json", "a", "b", "c")
	if r.stdout != `{"id":"a","state":"APPROVED"}`+"\n" {
		t.Errorf("stdout = %q, want only a", r.stdout)
	}
	objs := r.stderrObjects(t)
	if len(objs) != 2 || objs[0]["id"] != "b" || objs[0]["applied"] != "unknown" {
		t.Fatalf("stderr = %q, want b unknown (its headers were written)", r.stderr)
	}
	sum := objs[1]
	if fmt.Sprint(sum["notAttempted"]) != "[c]" || sum["status"] != nil || r.exit != exitError {
		t.Errorf("summary = %v exit %d", sum, r.exit)
	}
	if len(s.requests()) != 2 {
		t.Errorf("server saw %d requests, want 2", len(s.requests()))
	}
}

// Every line of a batch carries the id it addressed.
func TestBatchLinesCarryAddressedID(t *testing.T) {
	s := newMutServer(t, func(r *http.Request, _ int) mutReply { return toolReply(r, 200) })
	args := []string{"a", "b/c"}
	r := runHelperCmd(t, newTestBatchCmd(), s, context.Background(), nil, "json", args...)
	lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
	if len(lines) != len(args) {
		t.Fatalf("stdout = %q", r.stdout)
	}
	for i, l := range lines {
		var v map[string]any
		if err := json.Unmarshal([]byte(l), &v); err != nil || v["id"] != args[i] {
			t.Errorf("line %d = %s, want id %q", i, l, args[i])
		}
	}
}

func TestBatchPreview(t *testing.T) {
	withDryRun(t)
	r := runHelperCmd(t, newTestBatchCmd(), nil, context.Background(), nil, "text", "a", "b")
	var doc struct {
		DryRun   bool
		Requests []previewRequest
	}
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || !doc.DryRun || len(doc.Requests) != 2 || doc.Requests[1].URL != mutTestURL+"/api/v1/tools/b" {
		t.Errorf("preview = %q (%v)", r.stdout, err)
	}
}

func TestSequenceFailureAtLastStep(t *testing.T) {
	s := newMutServer(t, func(_ *http.Request, n int) mutReply {
		if n == 3 {
			return mutReply{status: 500}
		}
		return mutReply{body: fmt.Sprintf(`{"id":"x%d"}`, n)}
	})
	r := runHelperCmd(t, newTestSequenceCmd(), s, context.Background(), nil, "json", []string{}...)
	if r.exit != exitServer || r.stdout != "" {
		t.Errorf("exit %d stdout %q", r.exit, r.stdout)
	}
	obj := r.errorObject(t)
	if obj["applied"] != true {
		t.Errorf("applied = %v, want true: steps 1 and 2 were applied", obj["applied"])
	}
	steps, _ := json.Marshal(obj["steps"])
	want := `[{"applied":true,"method":"POST","path":"/api/v1/types"},{"applied":true,"method":"POST","path":"/api/v1/types/x1/things"},{"applied":"unknown","method":"POST","path":"/api/v1/ents"}]`
	if string(steps) != want {
		t.Errorf("steps = %s, want %s", steps, want)
	}
	if got := s.requests()[2].body; got != `{"thingId":"x2","typeId":"x1"}` {
		t.Errorf("step 3 body = %s", got)
	}
}

func TestSequencePreview(t *testing.T) {
	withDryRun(t)
	for _, format := range []string{"text", "json"} {
		r := runHelperCmd(t, newTestSequenceCmd(), nil, context.Background(), nil, format, []string{}...)
		if !strings.Contains(r.stdout, `"url": "`+mutTestURL+`/api/v1/types/NEW_TYPE_ID/things"`) ||
			!strings.Contains(r.stdout, `"thingId": "NEW_THING_ID"`) || strings.Count(r.stdout, `"method"`) != 3 {
			t.Errorf("%s preview = %s", format, r.stdout)
		}
		if format == "text" {
			want := "dry run: nothing was sent\nrequests are sent in order; NEW_TYPE_ID/NEW_THING_ID stand in for ids the preceding response returns\n"
			if r.stderr != want {
				t.Errorf("stderr = %q, want %q", r.stderr, want)
			}
			continue
		}
		objs := r.stderrObjects(t)
		if len(objs) != 2 || objs[1]["kind"] != noticePlaceholders || fmt.Sprint(objs[1]["placeholders"]) != "[NEW_TYPE_ID NEW_THING_ID]" {
			t.Errorf("stderr = %q", r.stderr)
		}
	}
}

func TestPreReadResolvesOnStderr(t *testing.T) {
	s := newMutServer(t, func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet {
			return mutReply{body: `{}`}
		}
		return mutReply{body: `{"taskView":{"task":{"id":"t1"}}}`}
	})
	r := runHelperCmd(t, newTestPreReadCmd(true), s, context.Background(), nil, "text", []string{}...)
	if r.stdout != "{\n  \"id\": \"t1\"\n}\n" || r.stderr != "Resolved policy step: P1\n" {
		t.Errorf("stdout %q stderr %q", r.stdout, r.stderr)
	}
	if got := s.requests(); len(got) != 2 || got[1].body != `{"policyStepId":"P1"}` {
		t.Errorf("requests = %+v", got)
	}
	r = runHelperCmd(t, newTestPreReadCmd(true), s, context.Background(), nil, "json", []string{}...)
	if want := `{"id":"P1","kind":"resolved","name":"policyStepId","notice":"Resolved policy step: P1"}` + "\n"; r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
}

func TestPreReadDryRun(t *testing.T) {
	withDryRun(t)
	s := newMutServer(t, fixedReply(200, `{}`))
	r := runHelperCmd(t, newTestPreReadCmd(true), s, context.Background(), nil, "text", []string{}...)
	if !strings.Contains(r.stdout, `"policyStepId": "P1"`) || len(s.requests()) != 1 {
		t.Errorf("OnDryRun read: stdout %q, %d requests", r.stdout, len(s.requests()))
	}
	r = runHelperCmd(t, newTestPreReadCmd(false), nil, context.Background(), nil, "text", []string{}...)
	if r.err != nil || strings.Contains(r.stdout, "policyStepId") {
		t.Errorf("skipped read: stdout %q err %v", r.stdout, r.err)
	}
}

// R22: the secret wording is added only when the response carries one.
func TestSecretWording(t *testing.T) {
	if got := secretWording(secretAutomationWebhook, []byte(`{"webhookHmacSecret":"","webhookCapabilityUrl":""}`)); got != "" {
		t.Errorf("empty webhook fields got wording %q", got)
	}
	if got := secretWording(secretAutomationWebhook, []byte(`{"webhookHmacSecret":"s"}`)); !strings.Contains(got, "re-read the automation") {
		t.Errorf("webhook secret wording = %q", got)
	}
	if got := secretWording(secretSPCredential, []byte(`{}`)); !strings.Contains(got, "revoke the credential") {
		t.Errorf("credential wording = %q", got)
	}
}

// The unwrap rules: byID hoists the object holding the id, named the named
// key, raw nothing; a collision leaves the body unchanged.
func TestUnwrapRules(t *testing.T) {
	cases := []struct {
		name string
		rule unwrapRule
		in   string
		want string
	}{
		{"zero is byID(id)", unwrapRule{}, `{"app":{"id":"a"},"expanded":[]}`, `{"id":"a","expanded":[]}`},
		{"byID connectorId", byID("connectorId"), `{"mcpServer":{"connectorId":"c"},"accessProfilesAttached":1}`, `{"connectorId":"c","accessProfilesAttached":1}`},
		{"named config", named("config"), `{"config":{"minCohortSize":3}}`, `{"minCohortSize":3}`},
		{"raw", raw(), `{"finding":{"id":"f","taskId":""},"taskId":"t"}`, `{"finding":{"id":"f","taskId":""},"taskId":"t"}`},
		{"collision is unchanged", byID("id"), `{"finding":{"id":"f","taskId":""},"taskId":"t"}`, `{"finding":{"id":"f","taskId":""},"taskId":"t"}`},
	}
	for _, tc := range cases {
		if got := string(tc.rule.apply([]byte(tc.in))); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
}

// secretBearing adds its wording to a failed stdout write.
func TestSecretBearingWriteFailure(t *testing.T) {
	c := &cobra.Command{Use: "create", RunE: func(cmd *cobra.Command, _ []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/creds", Body: map[string]any{}}, secretBearing(secretSPCredential))
	}}
	markMutating(c)
	s := newMutServer(t, fixedReply(200, `{"credential":{"id":"c1"},"clientSecret":"placeholder"}`))
	r := runHelperCmd(t, c, s, context.Background(), failingWriter{}, "json", []string{}...)
	obj := r.errorObject(t)
	if r.exit != exitError || obj["applied"] != true || !strings.HasSuffix(obj["error"].(string), "revoke the credential and create a new one") {
		t.Errorf("exit %d error %v", r.exit, obj)
	}
}

func TestAPIPreview(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
		exit int
	}{
		{"DELETE has no body", []string{"api", "--method", "DELETE", "--path", "/api/v1/x"},
			"{\n  \"dryRun\": true,\n  \"requests\": [\n    {\n      \"method\": \"DELETE\",\n      \"url\": \"" + mutTestURL + "/api/v1/x\"\n    }\n  ]\n}\n", 0},
		{"query and body as sent", []string{"api", "--path", "/api/v1/x", "--query", "a=1", "--body", `{"b":1,"a":2}`},
			"{\n  \"dryRun\": true,\n  \"requests\": [\n    {\n      \"method\": \"POST\",\n      \"url\": \"" + mutTestURL + "/api/v1/x?a=1\",\n      \"body\": {\n        \"a\": 2,\n        \"b\": 1\n      }\n    }\n  ]\n}\n", 0},
		{"fields", []string{"api", "--method", "POST", "--path", "/api/v1/x", "--fields", "x"}, "", exitUsage},
		{"bad path", []string{"api", "--method", "DELETE", "--path", "/api/v1/x/.."}, "", exitUsage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDryRun(t)
			t.Setenv("C1I_URL", mutTestURL)
			r := runMutationCLI(t, nil, 0, nil, tc.args...)
			if r.exit != tc.exit || r.stdout != tc.want {
				t.Errorf("exit %d stdout %q, want %d %q (%v)", r.exit, r.stdout, tc.exit, tc.want, r.err)
			}
		})
	}
}
