package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// automationsUpdateServer records the one POST it expects at the escaped path
// for id and fails the test on anything else.
func automationsUpdateServer(t *testing.T, escapedPath string, got *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != escapedPath {
			t.Errorf("request = %s %s, want POST %s", r.Method, r.URL.EscapedPath(), escapedPath)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, got); err != nil {
			t.Errorf("unmarshal body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"automation":{"id":"x"}}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runAutomationsUpdate(t *testing.T, id string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	automationsUpdateCmd.SetOut(&out)
	automationsUpdateCmd.SetContext(context.Background())
	err := automationsUpdateCmd.RunE(automationsUpdateCmd, []string{id})
	return out.String(), err
}

// An explicit --enabled=false and an empty --description are changes, so both
// must reach the body and the mask.
func TestAutomationsUpdateSendsEscapedWrappedPatch(t *testing.T) {
	const id = "auto/id ?#"
	var got map[string]any
	srv := automationsUpdateServer(t, "/api/v1/automations/auto%2Fid%20%3F%23", &got)

	resetCmdFlags(t, automationsUpdateCmd)
	stubCatalogClient(t, srv)
	withCatalogDryRun(t, false)
	t.Setenv("C1I_URL", "https://example.invalid")
	_ = automationsUpdateCmd.Flags().Set("display-name", "renamed")
	_ = automationsUpdateCmd.Flags().Set("description", "")
	_ = automationsUpdateCmd.Flags().Set("enabled", "false")

	if _, err := runAutomationsUpdate(t, id); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	want := map[string]any{
		"automation": map[string]any{"id": id, "displayName": "renamed", "description": "", "enabled": false},
		"updateMask": "displayName,description,enabled",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %#v\nwant %#v", got, want)
	}
}

func TestAutomationsUpdateEnabledAloneMasksOnlyEnabled(t *testing.T) {
	var got map[string]any
	srv := automationsUpdateServer(t, "/api/v1/automations/a1", &got)

	resetCmdFlags(t, automationsUpdateCmd)
	stubCatalogClient(t, srv)
	withCatalogDryRun(t, false)
	t.Setenv("C1I_URL", "https://example.invalid")
	_ = automationsUpdateCmd.Flags().Set("enabled", "true")

	if _, err := runAutomationsUpdate(t, "a1"); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	want := map[string]any{
		"automation": map[string]any{"id": "a1", "enabled": true},
		"updateMask": "enabled",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %#v\nwant %#v", got, want)
	}
}

func TestAutomationsUpdateBodyFileSendsVerbatimWithMask(t *testing.T) {
	var got map[string]any
	srv := automationsUpdateServer(t, "/api/v1/automations/a1", &got)

	resetCmdFlags(t, automationsUpdateCmd)
	stubCatalogClient(t, srv)
	withCatalogDryRun(t, false)
	t.Setenv("C1I_URL", "https://example.invalid")
	path := writeTempJSON(t, "auto.json", `{"id":"other","triggers":[{"userCreated":{"condition":"false"}}]}`)
	_ = automationsUpdateCmd.Flags().Set("body-file", path)
	_ = automationsUpdateCmd.Flags().Set("update-mask", "triggers")

	if _, err := runAutomationsUpdate(t, "a1"); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	want := map[string]any{
		"automation": map[string]any{
			"id":       "a1",
			"triggers": []any{map[string]any{"userCreated": map[string]any{"condition": "false"}}},
		},
		"updateMask": "triggers",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %#v\nwant %#v", got, want)
	}
}

func TestAutomationsUpdateBodyFileMisuseExitsUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request expected, got %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	path := writeTempJSON(t, "auto.json", `{"displayName":"x"}`)

	for _, tc := range []struct {
		name    string
		flags   map[string]string
		wantMsg string
	}{
		{"body-file without mask", map[string]string{"body-file": path}, "--update-mask is required"},
		{"mask without body-file", map[string]string{"update-mask": "displayName"}, "--update-mask requires --body-file"},
		{"body-file with a flag", map[string]string{"body-file": path, "update-mask": "displayName", "enabled": "true"}, "mutually exclusive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetCmdFlags(t, automationsUpdateCmd)
			stubCatalogClient(t, srv)
			withCatalogDryRun(t, false)
			t.Setenv("C1I_URL", "https://example.invalid")
			for k, v := range tc.flags {
				_ = automationsUpdateCmd.Flags().Set(k, v)
			}
			_, err := runAutomationsUpdate(t, "a1")
			if err == nil || exitCode(err) != exitUsage || !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("err = %v (exit %d), want exit %d containing %q", err, exitCode(err), exitUsage, tc.wantMsg)
			}
		})
	}
}

func TestAutomationsUpdateDryRunSendsNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("dry-run must not send, got %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	resetCmdFlags(t, automationsUpdateCmd)
	stubCatalogClient(t, srv)
	withCatalogDryRun(t, true)
	t.Setenv("C1I_URL", "https://example.invalid")
	_ = automationsUpdateCmd.Flags().Set("enabled", "false")

	out, err := runAutomationsUpdate(t, "a1")
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	reqs := previewDoc(t, out)
	body, _ := reqs[0]["body"].(map[string]any)
	if len(reqs) != 1 || reqs[0]["method"] != "POST" || reqs[0]["url"] != "https://example.invalid/api/v1/automations/a1" ||
		body["updateMask"] != "enabled" || body["automation"] == nil {
		t.Errorf("dry-run output = %s", out)
	}
}

// A top-level JSON null used to come back as a nil map and panic on the id
// assignment.
func TestAutomationsUpdateNullBodyFileExitsUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request expected, got %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	resetCmdFlags(t, automationsUpdateCmd)
	stubCatalogClient(t, srv)
	withCatalogDryRun(t, false)
	t.Setenv("C1I_URL", "https://example.invalid")
	_ = automationsUpdateCmd.Flags().Set("body-file", writeTempJSON(t, "null.json", `null`))
	_ = automationsUpdateCmd.Flags().Set("update-mask", "displayName")

	_, err := runAutomationsUpdate(t, "a1")
	if err == nil || exitCode(err) != exitUsage || !strings.Contains(err.Error(), "--body-file") {
		t.Errorf("err = %v (exit %d), want exit %d naming --body-file", err, exitCode(err), exitUsage)
	}
}

func TestAutomationsUpdateUsageErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request expected, got %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	path := writeTempJSON(t, "auto.json", `{"displayName":"x"}`)

	for _, tc := range []struct {
		name    string
		flags   map[string]string
		wantMsg string
	}{
		{"empty display name", map[string]string{"display-name": ""}, "display_name is required"},
		{"whitespace-only mask", map[string]string{"body-file": path, "update-mask": "  "}, "--update-mask is blank"},
		{"empty mask path", map[string]string{"body-file": path, "update-mask": "displayName,,description"}, "empty path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetCmdFlags(t, automationsUpdateCmd)
			stubCatalogClient(t, srv)
			withCatalogDryRun(t, false)
			t.Setenv("C1I_URL", "https://example.invalid")
			for k, v := range tc.flags {
				_ = automationsUpdateCmd.Flags().Set(k, v)
			}
			_, err := runAutomationsUpdate(t, "a1")
			if err == nil || exitCode(err) != exitUsage || !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("err = %v (exit %d), want exit %d containing %q", err, exitCode(err), exitUsage, tc.wantMsg)
			}
		})
	}
}

func TestAutomationsUpdateTrimsMaskPaths(t *testing.T) {
	var got map[string]any
	srv := automationsUpdateServer(t, "/api/v1/automations/a1", &got)

	resetCmdFlags(t, automationsUpdateCmd)
	stubCatalogClient(t, srv)
	withCatalogDryRun(t, false)
	t.Setenv("C1I_URL", "https://example.invalid")
	_ = automationsUpdateCmd.Flags().Set("body-file", writeTempJSON(t, "auto.json", `{"displayName":"x","description":"y"}`))
	_ = automationsUpdateCmd.Flags().Set("update-mask", " displayName, description ")

	if _, err := runAutomationsUpdate(t, "a1"); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if got["updateMask"] != "displayName,description" {
		t.Errorf("updateMask = %#v, want %q", got["updateMask"], "displayName,description")
	}
}

func TestAutomationsDeleteSendsEscapedDelete(t *testing.T) {
	var received bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		if r.Method != http.MethodDelete || r.URL.EscapedPath() != "/api/v1/automations/auto%2Fid%20%3F%23" {
			t.Errorf("request = %s %s, want DELETE escaped automation path", r.Method, r.URL.EscapedPath())
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	stubCatalogClient(t, srv)
	withCatalogDryRun(t, false)
	t.Setenv("C1I_URL", "https://example.invalid")

	var out bytes.Buffer
	automationsDeleteCmd.SetOut(&out)
	automationsDeleteCmd.SetContext(context.Background())
	if err := automationsDeleteCmd.RunE(automationsDeleteCmd, []string{"auto/id ?#"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if !received {
		t.Error("delete did not reach the server")
	}
	if got := out.String(); got != "{}\n" {
		t.Errorf("output = %q, want the API's {}", got)
	}
}

func TestAutomationsDeleteDryRunSendsNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("dry-run must not send, got %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	stubCatalogClient(t, srv)
	withCatalogDryRun(t, true)
	t.Setenv("C1I_URL", "https://example.invalid")

	var out bytes.Buffer
	automationsDeleteCmd.SetOut(&out)
	automationsDeleteCmd.SetContext(context.Background())
	if err := automationsDeleteCmd.RunE(automationsDeleteCmd, []string{"a1"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	reqs := previewDoc(t, out.String())
	if len(reqs) != 1 || reqs[0]["method"] != "DELETE" || reqs[0]["url"] != "https://example.invalid/api/v1/automations/a1" || reqs[0]["body"] != nil {
		t.Errorf("output = %q", out.String())
	}
}
