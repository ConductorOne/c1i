package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// findingRulesInsertServer answers the GET list, the per-rule POST update,
// and the final POST create for one findings rule family, recording every
// request's method/path/body for assertions. updateFails names a rule id
// whose update 500s, so the sequence stops there.
type findingRulesInsertServer struct {
	srv         *httptest.Server
	mu          sync.Mutex
	paths       []string
	bodies      []map[string]any
	updateFails string
}

func newFindingRulesInsertServer(t *testing.T, listPath, listBody string) *findingRulesInsertServer {
	t.Helper()
	s := &findingRulesInsertServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		s.paths = append(s.paths, r.Method+" "+r.URL.Path)
		s.bodies = append(s.bodies, body)
		s.mu.Unlock()

		switch {
		case r.Method == http.MethodGet && r.URL.Path == listPath:
			_, _ = w.Write([]byte(listBody))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/update"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, listPath+"/"), "/update")
			if id == s.updateFails {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"internal error"}`))
				return
			}
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && r.URL.Path == listPath:
			_, _ = w.Write([]byte(`{"routingRule":{"id":"rr-new"}}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *findingRulesInsertServer) requests() ([]string, []map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...), append([]map[string]any(nil), s.bodies...)
}

// findingRulesListBody is three routing rules at priority 1, 2 and 3, each
// carrying createdAt/updatedAt so the insert-at shift's "strip readonly
// fields" behavior has something to strip.
const findingRulesListBody = `{"list":[
	{"id":"rr-1","appId":"app-1","displayName":"Keep","description":"d1","priority":1,"enabled":true,"findingType":"FINDING_TYPE_CUSTOM","condition":"true","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"},
	{"id":"rr-2","appId":"app-1","displayName":"Shift2","description":"d2","priority":2,"enabled":true,"findingType":"FINDING_TYPE_CUSTOM","condition":"true","createdAt":"2026-01-02T00:00:00Z","updatedAt":"2026-01-02T00:00:00Z"},
	{"id":"rr-3","appId":"app-1","displayName":"Shift3","description":"d3","priority":3,"enabled":false,"findingType":"FINDING_TYPE_CUSTOM","condition":"false","createdAt":"2026-01-03T00:00:00Z","updatedAt":"2026-01-03T00:00:00Z"}
]}`

// TestFindingRuleCreateInsertAtShiftsHighestFirstThenCreates pins the whole
// --insert-at sequence: the colliding rules (priority >= 2) move highest
// first so none ever collide mid-move, their createdAt/updatedAt are
// stripped before the round-trip update, and the new rule lands at 2.
func TestFindingRuleCreateInsertAtShiftsHighestFirstThenCreates(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newFindingRulesInsertServer(t, "/api/v1/findings/routing-rules", findingRulesListBody)
	stubNewClient(t, s.srv)

	body := writeTempJSON(t, "body.json", `{"displayName":"New","appId":"app-1"}`)
	r := runCLI(t, nil, nil, "findings", "routing-rules", "create", "--body-file", body, "--insert-at", "2")
	if r.err != nil {
		t.Fatalf("exit %d: %v, stderr=%s", r.exit, r.err, r.stderr)
	}

	paths, bodies := s.requests()
	wantPaths := []string{
		"GET /api/v1/findings/routing-rules",
		"POST /api/v1/findings/routing-rules/rr-3/update",
		"POST /api/v1/findings/routing-rules/rr-2/update",
		"POST /api/v1/findings/routing-rules",
	}
	if len(paths) != len(wantPaths) {
		t.Fatalf("paths = %v, want %v", paths, wantPaths)
	}
	for i, want := range wantPaths {
		if paths[i] != want {
			t.Errorf("request %d path = %q, want %q", i, paths[i], want)
		}
	}

	rr3 := bodies[1]["routingRule"].(map[string]any)
	if rr3["id"] != "rr-3" || rr3["priority"] != float64(4) {
		t.Errorf("rr-3 update body = %v, want id rr-3 priority 4", rr3)
	}
	if _, ok := rr3["createdAt"]; ok {
		t.Errorf("rr-3 update body kept createdAt: %v", rr3)
	}
	if _, ok := rr3["updatedAt"]; ok {
		t.Errorf("rr-3 update body kept updatedAt: %v", rr3)
	}

	rr2 := bodies[2]["routingRule"].(map[string]any)
	if rr2["id"] != "rr-2" || rr2["priority"] != float64(3) {
		t.Errorf("rr-2 update body = %v, want id rr-2 priority 3", rr2)
	}

	created := bodies[3]["routingRule"].(map[string]any)
	if created["priority"] != float64(2) || created["displayName"] != "New" {
		t.Errorf("create body = %v, want priority 2 and the supplied displayName", created)
	}
	if want := "{\n  \"id\": \"rr-new\"\n}\n"; r.stdout != want {
		t.Errorf("stdout = %q, want the unwrapped created rule %q", r.stdout, want)
	}
}

// TestFindingRuleCreateInsertAtPartialFailureNamesShiftedRules pins that a
// failure part-way through the shift names exactly which rules already
// moved (rr-3) and stops before creating anything.
func TestFindingRuleCreateInsertAtPartialFailureNamesShiftedRules(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newFindingRulesInsertServer(t, "/api/v1/findings/routing-rules", findingRulesListBody)
	s.updateFails = "rr-2"
	stubNewClient(t, s.srv)

	body := writeTempJSON(t, "body.json", `{"displayName":"New"}`)
	r := runCLI(t, nil, nil, "findings", "routing-rules", "create", "--body-file", body, "--insert-at", "2")
	if r.err == nil {
		t.Fatal("expected the shift to fail")
	}
	if !strings.Contains(r.err.Error(), "rr-2") {
		t.Errorf("error = %q, want it to name the failing rule rr-2", r.err)
	}
	if !strings.Contains(r.err.Error(), "rr-3") {
		t.Errorf("error = %q, want it to name the already-shifted rule rr-3", r.err)
	}
	paths, _ := s.requests()
	if len(paths) != 3 {
		t.Fatalf("requests = %v, want exactly list + 2 updates (no create attempted)", paths)
	}
}

// TestFindingRuleCreateInsertAtRejectsNonPositive pins --insert-at's own
// usage-error floor: it is a 1-indexed order, not an offset, and the bad
// value is refused before any request.
func TestFindingRuleCreateInsertAtRejectsNonPositive(t *testing.T) {
	for _, v := range []string{"0", "-1"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newFindingRulesInsertServer(t, "/api/v1/findings/routing-rules", findingRulesListBody)
			stubNewClient(t, s.srv)
			body := writeTempJSON(t, "body.json", `{"displayName":"New"}`)
			r := runCLI(t, nil, nil, "findings", "routing-rules", "create", "--body-file", body, "--insert-at", v)
			if r.exit != exitUsage {
				t.Errorf("exit = %d (%v), want %d", r.exit, r.err, exitUsage)
			}
			if r.err == nil || !strings.Contains(r.err.Error(), "--insert-at must be at least 1") {
				t.Errorf("error = %v, want the 1-indexed-order validation message", r.err)
			}
			if paths, _ := s.requests(); len(paths) != 0 {
				t.Errorf("requests sent = %v, want none", paths)
			}
		})
	}
}

// TestFindingRuleCreatePlainCollisionErrorSuggestsInsertAt pins that the raw
// order-collision 400 is made actionable: the original server text survives
// and a pointer at --insert-at is appended.
func TestFindingRuleCreatePlainCollisionErrorSuggestsInsertAt(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	srv := newMutServer(t, fixedReply(400, `{"message":"order 2 is already used by another rule"}`))
	stubNewClient(t, srv.srv)
	body := writeTempJSON(t, "body.json", `{"displayName":"New","priority":2}`)
	r := runCLI(t, nil, nil, "findings", "routing-rules", "create", "--body-file", body)
	if r.exit != exitUsage {
		t.Fatalf("exit = %d (%v), want %d", r.exit, r.err, exitUsage)
	}
	if !strings.Contains(r.err.Error(), "order 2 is already used by another rule") {
		t.Errorf("error = %q, lost the server's original text", r.err)
	}
	if !strings.Contains(r.err.Error(), "--insert-at") {
		t.Errorf("error = %q, want it to point at --insert-at", r.err)
	}
}

// TestFindingRuleCreateInsertAtDryRunReadsListButSendsNothing pins that a
// --dry-run preview reflects the real current rule list (the GET happens for
// real) but previews every shift and the create without sending any of them.
func TestFindingRuleCreateInsertAtDryRunReadsListButSendsNothing(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newFindingRulesInsertServer(t, "/api/v1/findings/routing-rules", findingRulesListBody)
	stubNewClient(t, s.srv)

	body := writeTempJSON(t, "body.json", `{"displayName":"New"}`)
	r := runCLI(t, nil, nil, "findings", "routing-rules", "create", "--body-file", body, "--insert-at", "2", "--dry-run")
	if r.err != nil {
		t.Fatalf("exit %d: %v, stderr=%s", r.exit, r.err, r.stderr)
	}
	reqs := previewDoc(t, r.stdout)
	if len(reqs) != 3 {
		t.Fatalf("previewed %d requests, want 3 (two shifts + the create):\n%s", len(reqs), r.stdout)
	}
	wantURLs := []string{
		mutTestURL + "/api/v1/findings/routing-rules/rr-3/update",
		mutTestURL + "/api/v1/findings/routing-rules/rr-2/update",
		mutTestURL + "/api/v1/findings/routing-rules",
	}
	for i, want := range wantURLs {
		if reqs[i]["url"] != want || reqs[i]["method"] != "POST" {
			t.Errorf("request %d = %v %v, want POST %s", i, reqs[i]["method"], reqs[i]["url"], want)
		}
	}
	// Only the real pre-send list read should have reached the server.
	paths, _ := s.requests()
	if len(paths) != 1 || paths[0] != "GET /api/v1/findings/routing-rules" {
		t.Errorf("requests sent during --dry-run = %v, want only the list read", paths)
	}
}

// TestFindingRuleCreateInsertAtOverridesBodyFilePriority pins that
// --insert-at wins over a priority already in --body-file, per the
// documented override.
func TestFindingRuleCreateInsertAtOverridesBodyFilePriority(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	s := newFindingRulesInsertServer(t, "/api/v1/findings/routing-rules", `{"list":[]}`)
	stubNewClient(t, s.srv)

	body := writeTempJSON(t, "body.json", `{"displayName":"New","priority":99}`)
	r := runCLI(t, nil, nil, "findings", "routing-rules", "create", "--body-file", body, "--insert-at", "1")
	if r.err != nil {
		t.Fatalf("exit %d: %v, stderr=%s", r.exit, r.err, r.stderr)
	}
	_, bodies := s.requests()
	created := bodies[1]["routingRule"].(map[string]any)
	if created["priority"] != float64(1) {
		t.Errorf("created priority = %v, want --insert-at's 1 to win over the body's 99", created["priority"])
	}
}

// TestFindingRuleInsertAtSharedWithTransformationRules pins that
// transformation-rules shares the same --insert-at machinery over its own
// evaluationOrder field, including the shift's wire key.
func TestFindingRuleInsertAtSharedWithTransformationRules(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	listBody := `{"list":[{"id":"tr-1","displayName":"Keep","evaluationOrder":1},{"id":"tr-2","displayName":"Shift","evaluationOrder":2}]}`
	s := newFindingRulesInsertServer(t, "/api/v1/findings/transformation-rules", listBody)
	stubNewClient(t, s.srv)

	body := writeTempJSON(t, "body.json", `{"displayName":"New"}`)
	r := runCLI(t, nil, nil, "findings", "transformation-rules", "create", "--body-file", body, "--insert-at", "2")
	if r.err != nil {
		t.Fatalf("exit %d: %v, stderr=%s", r.exit, r.err, r.stderr)
	}
	paths, bodies := s.requests()
	wantPaths := []string{
		"GET /api/v1/findings/transformation-rules",
		"POST /api/v1/findings/transformation-rules/tr-2/update",
		"POST /api/v1/findings/transformation-rules",
	}
	if len(paths) != len(wantPaths) {
		t.Fatalf("paths = %v, want %v", paths, wantPaths)
	}
	for i, want := range wantPaths {
		if paths[i] != want {
			t.Errorf("request %d path = %q, want %q", i, paths[i], want)
		}
	}
	shifted := bodies[1]["transformationRule"].(map[string]any)
	if shifted["id"] != "tr-2" || shifted["evaluationOrder"] != float64(3) {
		t.Errorf("tr-2 update body = %v, want id tr-2 evaluationOrder 3", shifted)
	}
	created := bodies[2]["transformationRule"].(map[string]any)
	if created["evaluationOrder"] != float64(2) {
		t.Errorf("created evaluationOrder = %v, want 2", created["evaluationOrder"])
	}
}
