package cmd

import (
	"net/http"
	"strings"
	"testing"
)

func TestReportsUpdateSendsFlatBodyWithoutMask(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, fixedReply(200, tblReport))
	r := runMutationCLI(t, s.srv, 0, nil, "reports", "update", "rp-1", "--display-name", "N", "--prompt", "P")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	got := s.requests()[0]
	if got.rawPath != "/api/v1/reporting/reports/rp-1" || compactRaw(t, got.body) != `{"displayName":"N","prompt":"P"}` {
		t.Errorf("request = %+v, want a flat body with no updateMask (the API has none)", got)
	}
	for _, args := range [][]string{{"reports", "update", "rp-1"}, {"reports", "update", "rp-1", "--description", ""}} {
		s = newMutServer(t, fixedReply(200, tblReport))
		if r := runMutationCLI(t, s.srv, 0, nil, args...); r.exit != exitUsage || len(s.requests()) != 0 {
			t.Errorf("%v: exit %d with %d requests, want 2 and none", args, r.exit, len(s.requests()))
		}
	}
}

func TestReportsRunSendsEmptyBody(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, fixedReply(200, tblRun))
	if r := runMutationCLI(t, s.srv, 0, nil, "reports", "run", "rp-1"); r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	if got := s.requests()[0]; got.method != http.MethodPost || got.rawPath != "/api/v1/reporting/reports/rp-1/run" || compactRaw(t, got.body) != `{}` {
		t.Errorf("request = %+v", got)
	}
}

func TestReportsReadPaths(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	for _, tc := range []struct {
		args []string
		path string
		body string
	}{
		{[]string{"reports", "program", "rp-1", "--program-id", "pg-1"}, "/api/v1/reporting/reports/rp-1/program?program_id=pg-1", `{"programId":"pg-1","files":{"a":"b"}}`},
		{[]string{"reports", "provenance", "run-1", "--report-id", "rp-1"}, "/api/v1/reporting/reports/rp-1/runs/run-1/provenance", `{"programId":"pg-1","sources":[]}`},
	} {
		s := newMutServer(t, fixedReply(200, tc.body))
		r := runListCLI(t, s.srv, tc.args...)
		if r.err != nil {
			t.Fatalf("%v: exit %d: %v", tc.args, r.exit, r.err)
		}
		if got := s.requests()[0]; got.method != http.MethodGet || !strings.HasSuffix(got.rawPath+"", strings.SplitN(tc.path, "?", 2)[0]) {
			t.Errorf("%v: request %+v, want GET %s", tc.args, got, tc.path)
		}
	}
	// A required flag is enforced by cobra before any request.
	for _, args := range [][]string{{"reports", "program", "rp-1"}, {"reports", "provenance", "run-1"}} {
		if r := runListCLI(t, nil, args...); r.exit != exitUsage {
			t.Errorf("%v: exit %d, want 2", args, r.exit)
		}
	}
}
