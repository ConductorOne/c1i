package cmd

import (
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

func TestDecoysCreateUserClientCredential(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, tblDecoy)
	if _, err := runWebhookCmd(t, srv, decoysCreateCmd, nil, map[string]string{
		"user-client-credential-user-id": "u-1",
		"display-name":                   "zz",
		"annotation":                     "env=prod",
	}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if len(*reqs) != 2 {
		t.Fatalf("requests = %d, want 2 (user pre-read + write)", len(*reqs))
	}
	got := (*reqs)[1]
	if got.method != http.MethodPost || got.path != "/api/v1/decoys" {
		t.Errorf("request = %s %s, want POST /api/v1/decoys", got.method, got.path)
	}
	want := map[string]any{
		"userClientCredential": map[string]any{"userId": "u-1"},
		"displayName":          "zz",
		"annotations":          map[string]any{"env": "prod"},
	}
	if !reflect.DeepEqual(got.body, want) {
		t.Errorf("body = %#v\nwant %#v", got.body, want)
	}
}

func TestDecoysCreateAccessToken(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, tblDecoy)
	if _, err := runWebhookCmd(t, srv, decoysCreateCmd, nil, map[string]string{
		"access-token-subject-user-id": "u-2",
		"access-token-expires":         "1h",
	}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	want := map[string]any{"accessToken": map[string]any{"subjectUserId": "u-2", "expiresIn": "3600s"}}
	if len(*reqs) != 2 {
		t.Fatalf("requests = %d, want 2 (user pre-read + write)", len(*reqs))
	}
	if got := (*reqs)[1].body; !reflect.DeepEqual(got, want) {
		t.Errorf("body = %#v\nwant %#v", got, want)
	}
}

func TestDecoysCreateConnectorClient(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, tblDecoy)
	if _, err := runWebhookCmd(t, srv, decoysCreateCmd, nil, map[string]string{"connector-client": "true"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	want := map[string]any{"connectorClient": map[string]any{}}
	if got := (*reqs)[0].body; !reflect.DeepEqual(got, want) {
		t.Errorf("body = %#v\nwant %#v", got, want)
	}
}

func TestDecoysCreateWorkloadFed(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, tblDecoy)
	if _, err := runWebhookCmd(t, srv, decoysCreateCmd, nil, map[string]string{
		"workload-fed-provider-id":               "p-1",
		"workload-fed-condition":                 `claims.sub == "x"`,
		"workload-fed-service-principal-user-id": "u-3",
	}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	want := map[string]any{"workloadFed": map[string]any{
		"providerId":             "p-1",
		"conditionExpression":    `claims.sub == "x"`,
		"servicePrincipalUserId": "u-3",
	}}
	if len(*reqs) != 2 {
		t.Fatalf("requests = %d, want 2 (user pre-read + write)", len(*reqs))
	}
	if got := (*reqs)[1].body; !reflect.DeepEqual(got, want) {
		t.Errorf("body = %#v\nwant %#v", got, want)
	}
}

func TestDecoysCreateUsageErrorsSendNothing(t *testing.T) {
	withCatalogDryRun(t, false)
	cases := []struct {
		name  string
		flags map[string]string
	}{
		{"no selector", map[string]string{"display-name": "zz"}},
		{"two selectors", map[string]string{"user-client-credential-user-id": "u-1", "connector-client": "true"}},
		{"companion mismatch", map[string]string{"connector-client": "true", "access-token-expires": "1h"}},
		{"workload-fed missing condition", map[string]string{"workload-fed-provider-id": "p-1"}},
		{"bad annotation", map[string]string{"connector-client": "true", "annotation": "noequals"}},
		{"bad access-token-expires", map[string]string{"access-token-subject-user-id": "u-1", "access-token-expires": "soon"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, reqs := webhooksServer(t, tblDecoy)
			_, err := runWebhookCmd(t, srv, decoysCreateCmd, nil, tc.flags)
			if code := exitCode(err); code != exitUsage {
				t.Errorf("exit = %d, want %d (err: %v)", code, exitUsage, err)
			}
			if len(*reqs) != 0 {
				t.Errorf("sent %d request(s)", len(*reqs))
			}
		})
	}
}

func TestDecoysCreateBodyFileSentVerbatim(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, tblDecoy)
	if _, err := runWebhookCmd(t, srv, decoysCreateCmd, nil, map[string]string{
		"body-file": mustWriteTempJSON(t, `{"connectorClient":{}}`),
	}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	want := map[string]any{"connectorClient": map[string]any{}}
	if got := (*reqs)[0].body; !reflect.DeepEqual(got, want) {
		t.Errorf("body = %#v\nwant %#v", got, want)
	}

	srv2, reqs2 := webhooksServer(t, tblDecoy)
	_, err := runWebhookCmd(t, srv2, decoysCreateCmd, nil, map[string]string{
		"body-file":        mustWriteTempJSON(t, `{"connectorClient":{}}`),
		"connector-client": "true",
	})
	if code := exitCode(err); code != exitUsage {
		t.Errorf("exit = %d, want %d (err: %v)", code, exitUsage, err)
	}
	if len(*reqs2) != 0 {
		t.Errorf("sent %d request(s)", len(*reqs2))
	}
}

func TestDecoysUpdateMask(t *testing.T) {
	withCatalogDryRun(t, false)
	cases := []struct {
		name  string
		flags map[string]string
		decoy map[string]any
		mask  string
	}{
		{
			name:  "display name and disabled",
			flags: map[string]string{"display-name": "zz", "disabled": "true"},
			decoy: map[string]any{"id": "d-1", "displayName": "zz", "disabled": true},
			mask:  "displayName,disabled",
		},
		{
			name:  "annotation overwrite and delete",
			flags: map[string]string{"annotation": "env=prod", "description": ""},
			decoy: map[string]any{"id": "d-1", "description": "", "annotations": map[string]any{"env": "prod"}},
			mask:  "description,annotations",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, reqs := webhooksServer(t, tblDecoy)
			if _, err := runWebhookCmd(t, srv, decoysUpdateCmd, []string{"d-1"}, tc.flags); err != nil {
				t.Fatalf("RunE: %v", err)
			}
			if len(*reqs) != 1 {
				t.Fatalf("requests = %d, want 1", len(*reqs))
			}
			got := (*reqs)[0]
			if got.method != http.MethodPatch || got.path != "/api/v1/decoys/d-1" {
				t.Errorf("request = %s %s", got.method, got.path)
			}
			want := map[string]any{"decoy": tc.decoy, "updateMask": tc.mask}
			if !reflect.DeepEqual(got.body, want) {
				t.Errorf("body = %#v\nwant %#v", got.body, want)
			}
		})
	}
}

func TestDecoysUpdateNothingToUpdate(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, tblDecoy)
	_, err := runWebhookCmd(t, srv, decoysUpdateCmd, []string{"d-1"}, nil)
	if code := exitCode(err); code != exitUsage {
		t.Errorf("exit = %d, want %d (err: %v)", code, exitUsage, err)
	}
	if len(*reqs) != 0 {
		t.Errorf("sent %d request(s)", len(*reqs))
	}
}

func TestDecoysDeleteSendsDelete(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, `{}`)
	out, err := runWebhookCmd(t, srv, decoysDeleteCmd, []string{"d-1"}, nil)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if out != "{}\n" {
		t.Errorf("output = %q, want the API's {}", out)
	}
	if len(*reqs) != 1 || (*reqs)[0].method != http.MethodDelete || (*reqs)[0].path != "/api/v1/decoys/d-1" {
		t.Errorf("requests = %#v", *reqs)
	}
}

func TestDecoysRotateSendsPost(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, tblDecoy)
	out, err := runWebhookCmd(t, srv, decoysRotateCmd, []string{"d-1"}, nil)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if len(*reqs) != 1 || (*reqs)[0].method != http.MethodPost || (*reqs)[0].path != "/api/v1/decoys/d-1/rotate" {
		t.Errorf("requests = %#v", *reqs)
	}
	if !strings.Contains(out, `"clientSecret": "s-1"`) {
		t.Errorf("output missing vended secret: %s", out)
	}
}

func TestDecoysRow(t *testing.T) {
	got := decoyRow(decoyListItem{
		ID:          "d-1",
		DisplayName: "zz",
		Kind:        "DECOY_KIND_USER_CLIENT_CREDENTIAL",
		Disabled:    false,
		CreatedAt:   "2026-01-01T00:00:00Z",
	})
	want := map[string]any{
		"id":                          "d-1",
		"display_name":                "zz",
		"description":                 nil,
		"kind":                        "DECOY_KIND_USER_CLIENT_CREDENTIAL",
		"disabled":                    false,
		"annotations":                 map[string]string(nil),
		"created_at":                  "2026-01-01T00:00:00Z",
		"updated_at":                  nil,
		"last_used_at":                nil,
		"material_fingerprint_sha256": nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("row = %#v\nwant %#v", got, want)
	}
}

func TestDecoysSearchBody(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, `{"list":[{"id":"d-1","displayName":"zz"}],"nextPageToken":""}`)
	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = orig })
	out, err := runPairs(t, srv, decoysSearchCmd, nil,
		"query", "zz",
		"kind", "user-client-credential",
		"status", "active",
		"annotation-key", "env",
		"has-been-used", "true",
	)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if len(*reqs) != 1 || (*reqs)[0].path != "/api/v1/decoys/search" {
		t.Fatalf("requests = %#v", *reqs)
	}
	want := map[string]any{
		"query":          "zz",
		"kinds":          []any{"DECOY_KIND_USER_CLIENT_CREDENTIAL"},
		"statuses":       []any{"DECOY_STATUS_FILTER_ACTIVE"},
		"annotationKeys": []any{"env"},
		"hasBeenUsed":    true,
		"pageSize":       float64(50),
	}
	if got := (*reqs)[0].body; !reflect.DeepEqual(got, want) {
		t.Errorf("body = %#v\nwant %#v", got, want)
	}
	if !strings.Contains(out, `"id":"d-1"`) {
		t.Errorf("row missing: %s", out)
	}
}

func TestDecoysListRoute(t *testing.T) {
	withCatalogDryRun(t, false)
	srv, reqs := webhooksServer(t, `{"list":[{"id":"d-1","displayName":"zz"}],"nextPageToken":""}`)
	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = orig })
	out, err := runWebhookCmd(t, srv, decoysListCmd, nil, nil)
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if len(*reqs) != 1 || (*reqs)[0].method != http.MethodGet || (*reqs)[0].path != "/api/v1/decoys" {
		t.Errorf("requests = %#v", *reqs)
	}
	if !strings.Contains(out, `"id":"d-1"`) {
		t.Errorf("row missing: %s", out)
	}
}

// mustWriteTempJSON writes content to a temp file and returns its path.
func mustWriteTempJSON(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/body.json"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	return path
}
