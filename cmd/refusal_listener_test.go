package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// refusalListener backs a tree walk over inputs that must be refused before
// any request. The first case gets a live listener that must stay silent;
// every later one gets a nil server, whose client stub fails the test if a
// client is built.
type refusalListener struct{ live bool }

func (r *refusalListener) next(t *testing.T, route func(*http.Request, int) mutReply) (*httptest.Server, func() []entOwnerRequest) {
	t.Helper()
	if r.live {
		return nil, func() []entOwnerRequest { return nil }
	}
	r.live = true
	s := newMutServer(t, route)
	return s.srv, s.requests
}

// recordingReporter captures what a stub reports instead of failing the test.
type recordingReporter struct {
	errors   int
	cleanups []func()
}

func (r *recordingReporter) Helper()           {}
func (r *recordingReporter) Error(args ...any) { r.errors++ }
func (r *recordingReporter) Cleanup(f func())  { r.cleanups = append(r.cleanups, f) }
func (r *recordingReporter) runCleanups() {
	for i := len(r.cleanups) - 1; i >= 0; i-- {
		r.cleanups[i]()
	}
}

// With a nil server, building a client must be reported as a test failure;
// with a live one it must not. Drives the real stub against a fake reporter.
func TestNilServerFailsWhenAClientIsBuilt(t *testing.T) {
	nilRec := &recordingReporter{}
	defer nilRec.runCleanups()
	stubMutationClient(nilRec, nil, 0)
	_, _ = newClient(nil, mutTestURL)
	if nilRec.errors != 1 {
		t.Fatalf("nil-server stub reported %d failures on a client build, want 1", nilRec.errors)
	}

	s := newMutServer(t, fixedReply(200, "{}"))
	liveRec := &recordingReporter{}
	defer liveRec.runCleanups()
	stubMutationClient(liveRec, s.srv, 0)
	if _, err := newClient(nil, mutTestURL); err != nil {
		t.Fatal(err)
	}
	if liveRec.errors != 0 {
		t.Fatalf("live-server stub reported %d failures, want 0", liveRec.errors)
	}
}
