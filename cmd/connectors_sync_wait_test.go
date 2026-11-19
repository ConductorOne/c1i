package cmd

import (
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
func connSyncEnvelope(status, completedAt, lastError, errorKind string) string {
	return fmt.Sprintf(`{"connectorView":{"connector":{"id":"cn1","status":{"status":%q,"completedAt":%q,"lastError":%q,"errorKind":%q}}},"expanded":[]}`,
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

	t.Run("polls past a stale completedAt and prints the final status as a second document", func(t *testing.T) {
		s := newMutServer(t, connSyncRoute(
			connSyncEnvelope("SYNC_STATUS_DONE", "2024-01-01T00:00:00Z", "", "ERROR_KIND_UNSPECIFIED"), // baseline: already done
			connSyncEnvelope("SYNC_STATUS_RUNNING", "2024-01-01T00:00:00Z", "", "ERROR_KIND_UNSPECIFIED"),
			connSyncEnvelope("SYNC_STATUS_DONE", "2024-01-01T00:05:00Z", "", "ERROR_KIND_UNSPECIFIED"), // this sync's own completion; errorKind is non-empty even on success
		))
		r := runMutationCLI(t, s.srv, 0, nil, "connectors", "sync", "cn1", "--app-id", "a1", "--wait", "--wait-timeout", "2s")
		if r.err != nil {
			t.Fatalf("exit %d err %v stderr %q", r.exit, r.err, r.stderr)
		}
		if !strings.Contains(r.stdout, "{}") {
			t.Errorf("stdout = %q, want the force_sync accept document first", r.stdout)
		}
		if !strings.Contains(r.stdout, `"completedAt": "2024-01-01T00:05:00Z"`) {
			t.Errorf("stdout = %q, want the final status object printed", r.stdout)
		}
		got := s.requests()
		if len(got) != 4 || got[0].method != http.MethodGet || got[1].method != http.MethodPost {
			t.Fatalf("requests = %+v, want baseline GET, POST, then polling GETs", got)
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
