package cmd

import (
	"net/http"
	"strings"
	"testing"
)

func TestEntitlementsDeleteSendsDelete(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, `{}`)
	out, err := runWebhookCmd(t, srv, entitlementsDeleteCmd, []string{"ent 1"}, map[string]string{"app-id": "app-1"})
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if len(*reqs) != 1 || (*reqs)[0].method != http.MethodDelete || (*reqs)[0].path != "/api/v1/apps/app-1/entitlements/ent%201" {
		t.Errorf("requests = %+v, want one DELETE /api/v1/apps/app-1/entitlements/ent%%201", *reqs)
	}
	if strings.TrimSpace(out) != "{}" {
		t.Errorf("output = %q, want the raw response", out)
	}
}

func TestEntitlementsDeleteDryRunSendsNothing(t *testing.T) {
	withCatalogDryRun(t, true)
	srv, reqs := webhooksServer(t, `{}`)
	out, err := runWebhookCmd(t, srv, entitlementsDeleteCmd, []string{"ent-1"}, map[string]string{"app-id": "app-1"})
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if len(*reqs) != 0 {
		t.Errorf("dry-run sent %d request(s)", len(*reqs))
	}
	pr := previewDoc(t, out)
	if len(pr) != 1 || pr[0]["method"] != "DELETE" || !strings.HasSuffix(pr[0]["url"].(string), "/api/v1/apps/app-1/entitlements/ent-1") {
		t.Errorf("output = %q", out)
	}
}

func TestEntitlementsDeleteRequiresAppID(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, `{}`)
	_, err := runWebhookCmd(t, srv, entitlementsDeleteCmd, []string{"ent-1"}, nil)
	if err == nil || exitCode(err) != exitUsage {
		t.Fatalf("err = %v, want usage error", err)
	}
	if len(*reqs) != 0 {
		t.Errorf("sent %d request(s)", len(*reqs))
	}
}
