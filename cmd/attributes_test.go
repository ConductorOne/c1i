package cmd

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

const (
	attrTypesBody  = `{"list":[{"id":"ty-risk","name":"Risk Level"},{"id":"ty-comp","name":"Compliance Framework"},{"id":"ty-dup","name":"Dup"},{"id":"ty-dup2","name":"Dup"}],"nextPageToken":""}`
	attrValueBody  = `{"id":"v-1","attributeTypeId":"ty-risk","value":"zz-a","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z","deletedAt":null}`
	attrListBody   = `{"list":[` + attrValueBody + `],"nextPageToken":""}`
	attrCreatedEnv = `{"value":` + attrValueBody + `}`
)

// attrRoute answers the types list, and any other request with other.
func attrRoute(other string) func(*http.Request, int) mutReply {
	return func(r *http.Request, _ int) mutReply {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/attributes/types" {
			return mutReply{body: attrTypesBody}
		}
		return mutReply{body: other}
	}
}

func attrRun(t *testing.T, route func(*http.Request, int) mutReply, args ...string) (mutResult, []entOwnerRequest) {
	t.Helper()
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, route)
	r := runCLI(t, func() {
		stubMutationClient(t, s.srv, 0)
		orig := newListClient
		newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
			return client.NewForTesting(s.srv.URL, s.srv.Client(), client.WithMaxRetries(0)), nil
		}
		t.Cleanup(func() { newListClient = orig })
	}, nil, args...)
	return r, s.requests()
}

func TestAttributesTypesWire(t *testing.T) {
	r, sent := attrRun(t, fixedReply(200, attrTypesBody), "attributes", "types")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	if len(sent) != 1 || sent[0].method != http.MethodGet || sent[0].rawPath != "/api/v1/attributes/types" {
		t.Fatalf("requests = %+v", sent)
	}
	if !strings.Contains(r.stdout, `"id":"ty-risk"`) || !strings.Contains(r.stdout, `"name":"Risk Level"`) {
		t.Errorf("stdout = %q", r.stdout)
	}
}

func TestAttributesListResolvesTypeByNameOrID(t *testing.T) {
	for _, typ := range []string{"Risk Level", "ty-risk"} {
		r, sent := attrRun(t, attrRoute(attrListBody), "attributes", "list", "--type", typ)
		if r.err != nil {
			t.Fatalf("%s: exit %d: %v", typ, r.exit, r.err)
		}
		last := sent[len(sent)-1]
		if last.method != http.MethodGet || last.rawPath != "/api/v1/attributes/types/ty-risk/values" {
			t.Errorf("%s: last request = %+v", typ, last)
		}
		if !strings.Contains(r.stdout, `"type_id":"ty-risk"`) || !strings.Contains(r.stdout, `"deleted_at":null`) {
			t.Errorf("%s: stdout = %q", typ, r.stdout)
		}
	}
}

// The server answers an unknown well-formed type id with 200 and an empty
// list; the command must exit 4 instead.
func TestAttributesUnknownTypeExits4(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"list", []string{"attributes", "list", "--type", "nope"}},
		{"search", []string{"attributes", "search", "--type", "nope"}},
		{"create", []string{"attributes", "create", "--type", "nope", "--value", "zz"}},
		{"create dry-run", []string{"attributes", "create", "--type", "nope", "--value", "zz", "--dry-run"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, sent := attrRun(t, attrRoute(`{"list":[]}`), tc.args...)
			if r.exit != 4 {
				t.Fatalf("exit = %d, want 4 (stderr %q)", r.exit, r.stderr)
			}
			for _, s := range sent {
				if s.method != http.MethodGet || s.rawPath != "/api/v1/attributes/types" {
					t.Errorf("sent %+v before refusing", s)
				}
			}
		})
	}
}

func TestAttributesAmbiguousTypeNameExits2(t *testing.T) {
	r, sent := attrRun(t, attrRoute(attrListBody), "attributes", "list", "--type", "Dup")
	if r.exit != 2 || !strings.Contains(r.stderr, "ty-dup") || !strings.Contains(r.stderr, "ty-dup2") {
		t.Fatalf("exit %d stderr %q", r.exit, r.stderr)
	}
	if len(sent) != 1 {
		t.Errorf("sent %+v", sent)
	}
}

func TestAttributesSearchBody(t *testing.T) {
	r, sent := attrRun(t, attrRoute(attrListBody), "attributes", "search",
		"--type", "Risk Level", "--type", "ty-comp", "--id", "a", "--id", "b", "--exclude-id", "c",
		"--value", "High", "--query", "hi")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	last := sent[len(sent)-1]
	if last.method != http.MethodPost || last.rawPath != "/api/v1/search/attributes" {
		t.Fatalf("last request = %+v", last)
	}
	body := requestBody(t, last.body)
	for k, want := range map[string]string{"attributeTypeIds": "[ty-risk ty-comp]", "ids": "[a b]", "excludeIds": "[c]", "value": "High", "query": "hi"} {
		if got := fmt.Sprint(body[k]); got != want {
			t.Errorf("%s = %s, want %s", k, got, want)
		}
	}
}

func TestAttributesGetUnwrapsAndDeleteIsRaw(t *testing.T) {
	r, sent := attrRun(t, fixedReply(200, attrCreatedEnv), "attributes", "get", "v-1")
	if r.err != nil || sent[0].rawPath != "/api/v1/attributes/v-1" || !strings.Contains(r.stdout, `"id": "v-1"`) || strings.Contains(r.stdout, `"value": {`) {
		t.Fatalf("get: err %v sent %+v stdout %q", r.err, sent, r.stdout)
	}
	r, sent = attrRun(t, fixedReply(200, `{}`), "attributes", "delete", "v-1")
	if r.err != nil || len(sent) != 1 || sent[0].method != http.MethodDelete || sent[0].rawPath != "/api/v1/attribute/v-1" {
		t.Fatalf("delete: err %v sent %+v", r.err, sent)
	}
}

func TestAttributesCreateBodyAndPreview(t *testing.T) {
	r, sent := attrRun(t, attrRoute(attrCreatedEnv), "attributes", "create", "--type", "Risk Level", "--value", "zz-a")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	last := sent[len(sent)-1]
	body := requestBody(t, last.body)
	if last.method != http.MethodPost || last.rawPath != "/api/v1/attributes" || body["attributeTypeId"] != "ty-risk" || body["value"] != "zz-a" || len(body) != 2 {
		t.Fatalf("create sent %+v", last)
	}
	if !strings.Contains(r.stdout, `"id": "v-1"`) || strings.Contains(r.stdout, `"value": {`) {
		t.Errorf("stdout = %q", r.stdout)
	}
	r, sent = attrRun(t, attrRoute(attrCreatedEnv), "attributes", "create", "--type", "ty-risk", "--value", "zz-a", "--dry-run")
	if r.err != nil {
		t.Fatalf("dry-run: %v", r.err)
	}
	for _, s := range sent {
		if s.method != http.MethodGet {
			t.Errorf("dry-run sent %+v", s)
		}
	}
	if !strings.Contains(r.stdout, `"attributeTypeId": "ty-risk"`) {
		t.Errorf("preview = %q", r.stdout)
	}
}

func TestAttributesRequiredFlagsExit2BeforeSending(t *testing.T) {
	for _, args := range [][]string{
		{"attributes", "list"},
		{"attributes", "create", "--value", "zz"},
		{"attributes", "create", "--type", "ty-risk"},
	} {
		r, sent := attrRun(t, fixedReply(200, `{}`), args...)
		if r.exit != 2 || len(sent) != 0 {
			t.Errorf("%v: exit %d, sent %+v", args, r.exit, sent)
		}
	}
}
