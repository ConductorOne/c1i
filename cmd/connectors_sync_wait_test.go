package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func fastConnectorSyncWait(t *testing.T) {
	t.Helper()
	orig := connectorSyncWaitInterval
	connectorSyncWaitInterval = 5 * time.Millisecond
	t.Cleanup(func() { connectorSyncWaitInterval = orig })
}

// connSyncEnvelope builds the connector GET envelope one status read answers.
// step and objectsSynced stand in for the status fields c1i does not read, so
// a test can tell the server's whole object from a re-encoded subset.
func connSyncEnvelope(status, completedAt, lastError, errorKind string) string {
	return fmt.Sprintf(`{"connectorView":{"connector":{"id":"cn1","status":{"status":%q,"completedAt":%q,"lastError":%q,"errorKind":%q,"step":"STEP_DONE","objectsSynced":1234}}},"expanded":[]}`,
		status, completedAt, lastError, errorKind)
}

// connSyncRoute answers the force_sync POST with {} and each GET with the
// next of envs (the last one repeats). The first GET is the --wait baseline
// pre-read; later ones are the wait's own polls.
func connSyncRoute(envs ...string) func(*http.Request, int) mutReply {
	gets := 0
	return func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodPost {
			return mutReply{body: `{}`}
		}
		env := envs[min(gets, len(envs)-1)]
		gets++
		return mutReply{body: env}
	}
}

func TestConnectorsSyncWait(t *testing.T) {
	fastConnectorSyncWait(t)
	t.Setenv("C1I_URL", mutTestURL)

	t.Run("polls past a stale completedAt and prints the whole final status as the document", func(t *testing.T) {
		s := newMutServer(t, connSyncRoute(
			connSyncEnvelope("SYNC_STATUS_DONE", "2024-01-01T00:00:00Z", "", "ERROR_KIND_UNSPECIFIED"), // baseline: already done
			connSyncEnvelope("SYNC_STATUS_RUNNING", "2024-01-01T00:00:00Z", "", "ERROR_KIND_UNSPECIFIED"),
			connSyncEnvelope("SYNC_STATUS_DONE", "2024-01-01T00:05:00Z", "", "ERROR_KIND_UNSPECIFIED"), // this sync's own completion; errorKind is non-empty even on success
		))
		r := runMutationCLI(t, s.srv, 0, nil, "connectors", "sync", "cn1", "--app-id", "a1", "--wait", "--wait-timeout", "2s")
		if r.err != nil {
			t.Fatalf("exit %d err %v stderr %q", r.exit, r.err, r.stderr)
		}
		// Exactly one JSON document: the empty accept response is replaced,
		// not printed beside the status.
		var got map[string]any
		if err := json.Unmarshal([]byte(r.stdout), &got); err != nil {
			t.Fatalf("stdout is not one JSON document: %v\n%s", err, r.stdout)
		}
		if got["completedAt"] != "2024-01-01T00:05:00Z" {
			t.Errorf("completedAt = %v, want the finished sync's", got["completedAt"])
		}
		// The server's whole .status object, not the fields c1i reads to decide.
		if got["step"] != "STEP_DONE" || got["objectsSynced"] != float64(1234) {
			t.Errorf("status document = %v, want the server's whole .status object", got)
		}
		reqs := s.requests()
		if len(reqs) != 4 || reqs[0].method != http.MethodGet || reqs[1].method != http.MethodPost {
			t.Fatalf("requests = %+v, want baseline GET, POST, then polling GETs", reqs)
		}
		if !strings.Contains(r.stderr, "Connector cn1 sync finished") {
			t.Errorf("stderr = %q, want the success line", r.stderr)
		}
	})

	t.Run("a terminal error status exits 1 with lastError, applied true", func(t *testing.T) {
		s := newMutServer(t, connSyncRoute(
			connSyncEnvelope("SYNC_STATUS_DONE", "2024-01-01T00:00:00Z", "", "ERROR_KIND_UNSPECIFIED"),
			connSyncEnvelope("SYNC_STATUS_ERROR", "2024-01-01T00:05:00Z", "connector unreachable", "ERROR_KIND_CONNECTION"),
		))
		r := runMutationCLI(t, s.srv, 0, nil, "connectors", "sync", "cn1", "--app-id", "a1", "--wait", "--wait-timeout", "2s", "--error-format", "json")
		if r.exit != exitError {
			t.Fatalf("exit %d, want %d (%v)", r.exit, exitError, r.err)
		}
		obj := r.errorObject(t)
		msg, _ := obj["error"].(string)
		if obj["applied"] != true || !strings.Contains(msg, "connector unreachable") || !strings.Contains(msg, "SYNC_STATUS_ERROR") {
			t.Errorf("error object = %v", obj)
		}
	})

	t.Run("a terminal ERROR status with no lastError still fails", func(t *testing.T) {
		s := newMutServer(t, connSyncRoute(
			connSyncEnvelope("SYNC_STATUS_DONE", "2024-01-01T00:00:00Z", "", "ERROR_KIND_UNSPECIFIED"),
			connSyncEnvelope("SYNC_STATUS_ERROR", "2024-01-01T00:05:00Z", "", "ERROR_KIND_UNSPECIFIED"),
		))
		r := runMutationCLI(t, s.srv, 0, nil, "connectors", "sync", "cn1", "--app-id", "a1", "--wait", "--wait-timeout", "2s", "--error-format", "json")
		if r.exit != exitError {
			t.Fatalf("exit %d, want %d (%v)", r.exit, exitError, r.err)
		}
		obj := r.errorObject(t)
		msg, _ := obj["error"].(string)
		if obj["applied"] != true || !strings.Contains(msg, "SYNC_STATUS_ERROR") {
			t.Errorf("error object = %v, want applied true naming SYNC_STATUS_ERROR", obj)
		}
	})

	t.Run("a response without connectorView.connector fails fast", func(t *testing.T) {
		// Baseline first: the read cannot establish a baseline, so nothing is
		// sent -- exit 6, the same contract as any other 200 without its object.
		s := newMutServer(t, connSyncRoute(`{}`))
		r := runMutationCLI(t, s.srv, 0, nil, "connectors", "sync", "cn1", "--app-id", "a1", "--wait", "--wait-timeout", "2s")
		if r.exit != exitServer || r.err == nil || !strings.Contains(r.err.Error(), "connectorView.connector") {
			t.Fatalf("exit %d err %v, want %d naming the missing connector", r.exit, r.err, exitServer)
		}
		if got := s.requests(); len(got) != 1 || got[0].method != http.MethodGet {
			t.Errorf("requests = %+v, want only the baseline GET", got)
		}

		// After the write: the wait cannot decide, so it stops at once -- exit
		// 6 (the response broke the contract), applied true, no timeout loop.
		s = newMutServer(t, connSyncRoute(
			connSyncEnvelope("SYNC_STATUS_DONE", "2024-01-01T00:00:00Z", "", "ERROR_KIND_UNSPECIFIED"),
			`{}`,
		))
		r = runMutationCLI(t, s.srv, 0, nil, "connectors", "sync", "cn1", "--app-id", "a1", "--wait", "--wait-timeout", "2s", "--error-format", "json")
		if r.exit != exitServer || r.errorObject(t)["applied"] != true {
			t.Fatalf("exit %d (%v), want %d applied:true", r.exit, r.err, exitServer)
		}
		msg, _ := r.errorObject(t)["error"].(string)
		if !strings.Contains(msg, "connectorView.connector") {
			t.Errorf("error = %q, want it to name the missing connector", msg)
		}
		if got := s.requests(); len(got) != 3 {
			t.Errorf("requests = %+v, want baseline GET, POST, one poll (no timeout loop)", got)
		}
	})

	t.Run("a never-synced connector with no status is waited on", func(t *testing.T) {
		// The connector holds connectorView.connector but no .status yet, as
		// before its first sync: that is an empty baseline, not a failure, and
		// this sync's completion still ends the wait.
		s := newMutServer(t, connSyncRoute(
			`{"connectorView":{"connector":{"id":"cn1"}}}`,
			connSyncEnvelope("SYNC_STATUS_DONE", "2024-01-01T00:05:00Z", "", "ERROR_KIND_UNSPECIFIED"),
		))
		r := runMutationCLI(t, s.srv, 0, nil, "connectors", "sync", "cn1", "--app-id", "a1", "--wait", "--wait-timeout", "2s")
		if r.err != nil {
			t.Fatalf("exit %d err %v, want the first sync waited on", r.exit, r.err)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(r.stdout), &got); err != nil {
			t.Fatalf("stdout is not one JSON document: %v\n%s", err, r.stdout)
		}
		if got["completedAt"] != "2024-01-01T00:05:00Z" {
			t.Errorf("completedAt = %v, want the first sync's", got["completedAt"])
		}
		reqs := s.requests()
		if len(reqs) != 3 || reqs[0].method != http.MethodGet || reqs[1].method != http.MethodPost {
			t.Errorf("requests = %+v, want baseline GET, POST, one poll", reqs)
		}
	})

	t.Run("a non-JSON accept response is still a mutation error", func(t *testing.T) {
		// --wait replaces the response document, but the response is still
		// validated: a broken 2xx must not pass as success.
		s := newMutServer(t, func(r *http.Request, _ int) mutReply {
			if r.Method == http.MethodPost {
				return mutReply{body: `<html>not JSON</html>`}
			}
			return mutReply{body: connSyncEnvelope("SYNC_STATUS_DONE", "2024-01-01T00:05:00Z", "", "ERROR_KIND_UNSPECIFIED")}
		})
		r := runMutationCLI(t, s.srv, 0, nil, "connectors", "sync", "cn1", "--app-id", "a1", "--wait", "--wait-timeout", "2s", "--error-format", "json")
		if r.exit != exitServer || r.errorObject(t)["applied"] != true {
			t.Fatalf("exit %d (%v), want %d applied:true", r.exit, r.err, exitServer)
		}
		if r.stdout != "" {
			t.Errorf("stdout = %q, want nothing", r.stdout)
		}
	})

	t.Run("a failed status write reports applied", func(t *testing.T) {
		s := newMutServer(t, connSyncRoute(
			connSyncEnvelope("SYNC_STATUS_DONE", "2024-01-01T00:00:00Z", "", "ERROR_KIND_UNSPECIFIED"),
			connSyncEnvelope("SYNC_STATUS_DONE", "2024-01-01T00:05:00Z", "", "ERROR_KIND_UNSPECIFIED"),
		))
		r := runMutationCLI(t, s.srv, 0, failingWriter{}, "connectors", "sync", "cn1", "--app-id", "a1", "--wait", "--wait-timeout", "2s", "--error-format", "json")
		if r.exit != exitError {
			t.Fatalf("exit %d, want %d (%v)", r.exit, exitError, r.err)
		}
		obj := r.errorObject(t)
		msg, _ := obj["error"].(string)
		if obj["applied"] != true || !strings.Contains(msg, "was applied but writing its status failed") {
			t.Errorf("error object = %v, want applied true naming the status write", obj)
		}
	})

	t.Run("a never-synced connector's UNSPECIFIED status still advances", func(t *testing.T) {
		// The shape live at creation: SYNC_STATUS_UNSPECIFIED with null
		// timestamps. It is an empty baseline, and this sync's completion ends
		// the wait.
		s := newMutServer(t, connSyncRoute(
			`{"connectorView":{"connector":{"id":"cn1","status":{"status":"SYNC_STATUS_UNSPECIFIED","completedAt":null,"lastError":"","errorKind":"ERROR_KIND_UNSPECIFIED"}}}}`,
			connSyncEnvelope("SYNC_STATUS_DONE", "2024-01-01T00:05:00Z", "", "ERROR_KIND_UNSPECIFIED"),
		))
		r := runMutationCLI(t, s.srv, 0, nil, "connectors", "sync", "cn1", "--app-id", "a1", "--wait", "--wait-timeout", "2s")
		if r.err != nil {
			t.Fatalf("exit %d err %v, want the first sync waited on", r.exit, r.err)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(r.stdout), &got); err != nil {
			t.Fatalf("stdout is not one JSON document: %v\n%s", err, r.stdout)
		}
		if got["completedAt"] != "2024-01-01T00:05:00Z" {
			t.Errorf("completedAt = %v, want the first sync's", got["completedAt"])
		}
		if reqs := s.requests(); len(reqs) != 3 || reqs[0].method != http.MethodGet || reqs[1].method != http.MethodPost {
			t.Errorf("requests = %+v, want baseline GET, POST, one poll", reqs)
		}
	})

	t.Run("DONE without a completion time keeps waiting", func(t *testing.T) {
		// Live: a connector created with an empty config stays SYNC_STATUS_DONE
		// with null timestamps, because its syncs record NOT_CONNECTED. Ending
		// the wait on that state would be wrong: a connector whose config was
		// just fixed looks the same until its queued sync starts. So this ends
		// at the timeout, and the status error (if any) stays visible through
		// "connectors get".
		doneNoTime := `{"connectorView":{"connector":{"id":"cn1","status":{"status":"SYNC_STATUS_DONE","completedAt":null,"lastError":"","errorKind":"ERROR_KIND_UNSPECIFIED"}}}}`
		s := newMutServer(t, connSyncRoute(doneNoTime, doneNoTime))
		r := runMutationCLI(t, s.srv, 0, nil, "connectors", "sync", "cn1", "--app-id", "a1", "--wait", "--wait-timeout", "200ms", "--error-format", "json")
		if r.exit != exitError || r.errorObject(t)["applied"] != true {
			t.Fatalf("exit %d (%v), want %d applied:true", r.exit, r.err, exitError)
		}
		msg, _ := r.errorObject(t)["error"].(string)
		if !strings.Contains(msg, "timed out") {
			t.Errorf("error = %q, want the timeout to report it", msg)
		}
		if r.stdout != "" {
			t.Errorf("stdout = %q, want no document (the wait did not succeed)", r.stdout)
		}
		if reqs := s.requests(); len(reqs) < 3 {
			t.Errorf("requests = %+v, want the baseline GET, the POST, and at least one poll", reqs)
		}
	})

	t.Run("timeout exits 1, applied true, no error status reached", func(t *testing.T) {
		s := newMutServer(t, connSyncRoute(
			connSyncEnvelope("SYNC_STATUS_DONE", "2024-01-01T00:00:00Z", "", "ERROR_KIND_UNSPECIFIED"),
			connSyncEnvelope("SYNC_STATUS_RUNNING", "2024-01-01T00:00:00Z", "", "ERROR_KIND_UNSPECIFIED"),
		))
		r := runMutationCLI(t, s.srv, 0, nil, "connectors", "sync", "cn1", "--app-id", "a1", "--wait", "--wait-timeout", "50ms", "--error-format", "json")
		if r.exit != exitError || r.errorObject(t)["applied"] != true {
			t.Errorf("exit %d stderr %s, want %d and applied:true", r.exit, r.stderr, exitError)
		}
	})

	t.Run("without --wait nothing is polled and behavior is unchanged", func(t *testing.T) {
		s := newMutServer(t, fixedReply(200, `{}`))
		r := runMutationCLI(t, s.srv, 0, nil, "connectors", "sync", "cn1", "--app-id", "a1")
		if r.err != nil {
			t.Fatalf("exit %d err %v", r.exit, r.err)
		}
		if strings.TrimSpace(r.stdout) != "{}" {
			t.Errorf("stdout = %q, want exactly {}", r.stdout)
		}
		if got := s.requests(); len(got) != 1 || got[0].method != http.MethodPost {
			t.Errorf("requests = %+v, want only the POST", got)
		}
	})

	t.Run("dry run never reads a baseline or polls", func(t *testing.T) {
		withDryRun(t)
		r := runMutationCLI(t, nil, 0, nil, "connectors", "sync", "cn1", "--app-id", "a1", "--wait", "--wait-timeout", "2s")
		if r.err != nil || !strings.Contains(r.stdout, `"dryRun": true`) {
			t.Errorf("exit %d err %v stdout %q", r.exit, r.err, r.stdout)
		}
	})
}

func TestCompletedAtAdvanced(t *testing.T) {
	for _, tc := range []struct {
		name       string
		base, next string
		want       bool
	}{
		{"empty next never advances", "2024-01-01T00:00:00Z", "", false},
		{"non-empty next beats empty base", "", "2024-01-01T00:00:00Z", true},
		{"equal timestamps do not advance", "2024-01-01T00:00:00Z", "2024-01-01T00:00:00Z", false},
		{"a later timestamp advances", "2024-01-01T00:00:00Z", "2024-01-01T00:05:00Z", true},
		{"an earlier timestamp does not advance", "2024-01-01T00:05:00Z", "2024-01-01T00:00:00Z", false},
		{"unparsable but different strings fall back to inequality", "not-a-time", "also-not-a-time", true},
		{"unparsable but equal strings do not advance", "not-a-time", "not-a-time", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := completedAtAdvanced(tc.base, tc.next); got != tc.want {
				t.Errorf("completedAtAdvanced(%q, %q) = %v, want %v", tc.base, tc.next, got, tc.want)
			}
		})
	}
}
