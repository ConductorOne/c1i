package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

const (
	amApp   = "app-1"
	amTypes = `{"list":[` +
		`{"id":"ty-dept","displayName":"Department","appUserProfile":{"appId":"","appUserProfileAttributeKey":""}},` +
		`{"id":"ty-mgr","displayName":"Manager Email","app":{"appId":""}},` +
		`{"id":"ty-cust","displayName":"Custom","customAppUserProfile":{"userProfileAttributeKey":"","appId":"","appUserProfileAttributeKey":""}},` +
		`{"id":"ty-dup-1","displayName":"Twin","appUserProfile":{}},` +
		`{"id":"ty-dup-2","displayName":"Twin","appUserProfile":{}}]}`
	amCreated = `{"attribute":{"userAttributeMapping":{"id":"map-1","appId":"app-1"},"appPath":"","fallbacksPath":""},"expanded":[]}`
)

// amRoute answers the types GET with types and every mapping read with
// current, and any write with amCreated.
func amRoute(types, current string) func(*http.Request, int) mutReply {
	return func(r *http.Request, _ int) mutReply {
		switch {
		case r.Method != http.MethodGet:
			return mutReply{body: amCreated}
		case strings.HasSuffix(r.URL.Path, "/user-attribute-types"):
			return mutReply{body: types}
		}
		return mutReply{body: current}
	}
}

func amRun(t *testing.T, route func(*http.Request, int) mutReply, args ...string) (mutResult, []entOwnerRequest) {
	t.Helper()
	t.Setenv("C1I_URL", mutTestURL)
	s := newMutServer(t, route)
	r := runMutationCLI(t, s.srv, 0, nil, args...)
	return r, s.requests()
}

func TestAttributeMappingCreateBodyFollowsConfigKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"app_user_profile by name", []string{"--type", "Department", "--key", "dept"},
			`{"appId":"app-1","appUserProfileAttributeKey":"dept","attributeType":{"id":"ty-dept","displayName":"Department","appUserProfile":{"appId":"app-1","appUserProfileAttributeKey":"dept"}}}`},
		{"by id", []string{"--type", "ty-dept", "--key", "dept"},
			`{"appId":"app-1","appUserProfileAttributeKey":"dept","attributeType":{"id":"ty-dept","displayName":"Department","appUserProfile":{"appId":"app-1","appUserProfileAttributeKey":"dept"}}}`},
		{"app", []string{"--type", "Manager Email"},
			`{"appId":"app-1","attributeType":{"id":"ty-mgr","displayName":"Manager Email","app":{"appId":"app-1"}}}`},
		{"custom", []string{"--type", "Custom", "--key", "k", "--custom-key", "mine"},
			`{"appId":"app-1","appUserProfileAttributeKey":"k","attributeType":{"id":"ty-cust","displayName":"Custom","customAppUserProfile":{"appId":"app-1","appUserProfileAttributeKey":"k","userProfileAttributeKey":"mine"}}}`},
		{"fallbacks and profile types", []string{"--type", "Department", "--key", "dept", "--fallbacks-file", writeTempJSON(t, "fb.json", `[{"appUserProfile":{"appId":"app-1","appUserProfileAttributeKey":"alt"}}]`), "--profile-type-id", "p1", "--profile-type-id", "p2"},
			`{"appId":"app-1","appUserProfileAttributeKey":"dept","attributeType":{"id":"ty-dept","displayName":"Department","appUserProfile":{"appId":"app-1","appUserProfileAttributeKey":"dept"}},"fallbacks":[{"appUserProfile":{"appId":"app-1","appUserProfileAttributeKey":"alt"}}],"profileTypeIds":["p1","p2"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, sent := amRun(t, amRoute(amTypes, ""), append([]string{"attribute-mappings", "create", "--app-id", amApp}, tc.args...)...)
			if r.err != nil {
				t.Fatalf("exit %d: %v", r.exit, r.err)
			}
			if len(sent) != 2 || sent[0].method != http.MethodGet || sent[1].method != http.MethodPost || sent[1].rawPath != "/api/v1/user-attribute-mappings" {
				t.Fatalf("requests = %+v, want the types GET then one POST", sent)
			}
			if got := compactRaw(t, sent[1].body); got != compactRaw(t, tc.want) {
				t.Errorf("body = %s\nwant %s", got, compactRaw(t, tc.want))
			}
			var doc map[string]any
			if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || doc["id"] != "map-1" {
				t.Errorf("stdout %q: want the mapping unwrapped with a top-level id (err %v)", r.stdout, err)
			}
		})
	}
}

// Every refusal below must leave nothing but the read of the types behind.
func TestAttributeMappingCreateRefusesBeforeSending(t *testing.T) {
	base := []string{"attribute-mappings", "create", "--app-id", amApp}
	for _, tc := range []struct {
		name     string
		args     []string
		types    string
		wantExit int
		wantErr  string
		reads    int
	}{
		{"--key on an app-level type", []string{"--type", "Manager Email", "--key", "x"}, amTypes, exitUsage, "takes no --key", 1},
		{"--custom-key on an app-level type", []string{"--type", "Manager Email", "--custom-key", "x"}, amTypes, exitUsage, "takes no --key", 1},
		{"profile type without --key", []string{"--type", "Department"}, amTypes, exitUsage, "--key is required", 1},
		{"--custom-key on a profile type", []string{"--type", "Department", "--key", "k", "--custom-key", "c"}, amTypes, exitUsage, "--custom-key applies only", 1},
		{"custom without --custom-key", []string{"--type", "Custom", "--key", "k"}, amTypes, exitUsage, "--custom-key", 1},
		{"unknown type", []string{"--type", "Nope", "--key", "k"}, amTypes, exitNotFound, `no such attribute type`, 1},
		{"ambiguous name", []string{"--type", "Twin", "--key", "k"}, amTypes, exitUsage, "ty-dup-1, ty-dup-2", 1},
		{"types answer with a cursor", []string{"--type", "Department", "--key", "k"}, `{"list":[],"nextPageToken":"more"}`, exitServer, "INCOMPLETE", 1},
		{"no --type", []string{"--key", "k"}, amTypes, exitUsage, `"type" not set`, 0},
		{"--body-file with a flag", []string{"--type", "Department", "--body-file", writeTempJSON(t, "b.json", `{}`)}, amTypes, exitUsage, "mutually exclusive with --app-id", 0},
		{"fallbacks not an array", []string{"--type", "Department", "--key", "k", "--fallbacks-file", writeTempJSON(t, "f.json", `{}`)}, amTypes, exitUsage, "--fallbacks-file", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, dry := range []bool{false, true} {
				args := append(append([]string{}, base...), tc.args...)
				if dry {
					withDryRun(t)
				}
				r, sent := amRun(t, amRoute(tc.types, ""), args...)
				if r.exit != tc.wantExit || r.err == nil || !strings.Contains(r.err.Error(), tc.wantErr) {
					t.Errorf("dry=%v: exit %d (%v), want %d containing %q", dry, r.exit, r.err, tc.wantExit, tc.wantErr)
				}
				if len(sent) != tc.reads {
					t.Errorf("dry=%v: requests = %+v, want %d read(s) and no write", dry, sent, tc.reads)
				}
				for _, q := range sent {
					if q.method != http.MethodGet {
						t.Errorf("dry=%v: sent %s %s", dry, q.method, q.rawPath)
					}
				}
			}
		})
	}
}

func TestAttributeMappingCreateBodyFileIsVerbatim(t *testing.T) {
	body := `{"appId":"app-1","attributeType":{"id":"ty-mgr","app":{"appId":"app-1"}}}`
	r, sent := amRun(t, amRoute(amTypes, ""), "attribute-mappings", "create", "--body-file", writeTempJSON(t, "b.json", body))
	if r.err != nil {
		t.Fatal(r.err)
	}
	if len(sent) != 1 || sent[0].method != http.MethodPost || compactRaw(t, sent[0].body) != compactRaw(t, body) {
		t.Errorf("requests = %+v, want one POST of the file as given", sent)
	}
}

const amCurrent = `{"attribute":{"userAttributeMapping":{"id":"map-1","appId":"app-1",` +
	`"attributeType":{"id":"ty-dept","displayName":"Department","appUserProfile":{"appId":"app-1","appUserProfileAttributeKey":"old"}},` +
	`"fallbacks":[{"appUserProfile":{"appId":"app-1","appUserProfileAttributeKey":"alt"}}],"profileTypeIds":["p1"]}}}`

// The server's PUT replaces fallbacks and profile types left out and ignores
// the mask, so update must send the current ones back.
func TestAttributeMappingUpdateResendsWhatItDoesNotChange(t *testing.T) {
	at := func(key string) string {
		return `{"id":"ty-dept","displayName":"Department","appUserProfile":{"appId":"app-1","appUserProfileAttributeKey":"` + key + `"}}`
	}
	const fb = `[{"appUserProfile":{"appId":"app-1","appUserProfileAttributeKey":"alt"}}]`
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"key", []string{"--key", "new"},
			`{"attributeType":` + at("new") + `,"fallbacks":` + fb + `,"profileTypeIds":["p1"],"updateMask":"attributeType"}`},
		{"fallbacks", []string{"--fallbacks-file", writeTempJSON(t, "fb.json", `[]`)},
			`{"attributeType":` + at("old") + `,"fallbacks":[],"profileTypeIds":["p1"],"updateMask":"fallbacks"}`},
		{"both", []string{"--key", "new", "--fallbacks-file", writeTempJSON(t, "fb2.json", `[]`)},
			`{"attributeType":` + at("new") + `,"fallbacks":[],"profileTypeIds":["p1"],"updateMask":"attributeType,fallbacks"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, sent := amRun(t, amRoute(amTypes, amCurrent), append([]string{"attribute-mappings", "update", "map-1"}, tc.args...)...)
			if r.err != nil {
				t.Fatalf("exit %d: %v", r.exit, r.err)
			}
			if len(sent) != 2 || sent[0].method != http.MethodGet || sent[1].method != http.MethodPut || sent[1].rawPath != "/api/v1/user-attribute-mappings/map-1" {
				t.Fatalf("requests = %+v, want the mapping GET then one PUT", sent)
			}
			if got := compactRaw(t, sent[1].body); got != compactRaw(t, tc.want) {
				t.Errorf("body = %s\nwant %s", got, compactRaw(t, tc.want))
			}
		})
	}
}

func TestAttributeMappingUpdateRefusals(t *testing.T) {
	mgr := `{"attribute":{"userAttributeMapping":{"id":"map-1","attributeType":{"id":"ty-mgr","displayName":"Manager Email","app":{"appId":"app-1"}}}}}`
	for _, tc := range []struct {
		name    string
		args    []string
		current string
		reads   int
		wantErr string
	}{
		{"no field flag", nil, amCurrent, 0, "nothing to update: pass --key, --custom-key, or --fallbacks-file"},
		{"--key on an app-level mapping", []string{"--key", "x"}, mgr, 1, "has no --key to change"},
		{"--custom-key on a profile mapping", []string{"--custom-key", "x"}, amCurrent, 1, "--custom-key applies only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, sent := amRun(t, amRoute(amTypes, tc.current), append([]string{"attribute-mappings", "update", "map-1"}, tc.args...)...)
			if r.exit != exitUsage || r.err == nil || !strings.Contains(r.err.Error(), tc.wantErr) {
				t.Errorf("exit %d (%v), want 2 containing %q", r.exit, r.err, tc.wantErr)
			}
			if len(sent) != tc.reads {
				t.Errorf("requests = %+v, want %d read(s) and no write", sent, tc.reads)
			}
		})
	}
}

func TestAttributeMappingRows(t *testing.T) {
	row := attributeMappingRow(map[string]any{"userAttributeMapping": map[string]any{
		"id": "m1", "appId": "a1", "appUserProfileAttributeKey": "", "displayName": "Manager Email",
		"priority": float64(0), "count": float64(3), "evaluationErrorCount": float64(1),
		"attributeType": map[string]any{"id": "t1", "displayName": "Manager Email", "app": map[string]any{"appId": "a1"}},
	}})
	if row["key"] != nil || row["config_kind"] != "app" || row["count"] != float64(3) || row["evaluation_error_count"] != float64(1) || row["attribute_type_id"] != "t1" {
		t.Errorf("row = %v: want key null, config_kind app and numeric counts", row)
	}
	kinds := map[string]map[string]any{
		"app": {"app": map[string]any{}}, "app_user_profile": {"appUserProfile": map[string]any{}},
		"custom_app_user_profile": {"customAppUserProfile": map[string]any{}}, "cel_expression": {"celExpression": map[string]any{}},
	}
	for want, at := range kinds {
		if got := attributeTypeRow(at)["config_kind"]; got != want {
			t.Errorf("config_kind = %v, want %s", got, want)
		}
	}
	if got := attributeTypeRow(map[string]any{"id": "t"})["config_kind"]; got != nil {
		t.Errorf("a type with no config has config_kind %v, want null", got)
	}
}

func TestKeepMappingsOfApp(t *testing.T) {
	items := []map[string]any{
		{"userAttributeMapping": map[string]any{"id": "a", "appId": "x"}},
		{"userAttributeMapping": map[string]any{"id": "b", "appId": "y"}},
	}
	got, err := keepMappingsOfApp(items, "x")
	if err != nil || len(got) != 1 || optStr(attributeMappingOf(got[0]), "id") != "a" {
		t.Errorf("got %v, %v; want only item a", got, err)
	}
	if _, err := keepMappingsOfApp([]map[string]any{{"userAttributeMapping": map[string]any{"id": "c"}}}, "x"); err == nil {
		t.Error("an item with no appId must be refused")
	}
}

// A type c1i did not find in a 200 answer is not a 404: the JSON error must
// not claim a method, path or status.
func TestAttributeMappingUnknownTypeJSONErrorHasNoFabricatedStatus(t *testing.T) {
	r, _ := amRun(t, amRoute(amTypes, ""), "attribute-mappings", "create", "--app-id", amApp, "--type", "Nope", "--key", "k", "--error-format", "json")
	if r.exit != exitNotFound {
		t.Fatalf("exit %d, want 4", r.exit)
	}
	obj := r.errorObject(t)
	for _, k := range []string{"status", "method", "path", "body"} {
		if _, ok := obj[k]; ok {
			t.Errorf("error object has %q: %v", k, obj)
		}
	}
	if obj["applied"] != false {
		t.Errorf("applied = %v, want false", obj["applied"])
	}
}

func amListPages(t *testing.T, appID string, flags map[string]string, pages map[string]string) (string, int, error) {
	t.Helper()
	reqs := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs++
		_, _ = w.Write([]byte(pages[r.URL.Query().Get("page_token")]))
	}))
	t.Cleanup(srv.Close)
	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = orig })
	t.Setenv("C1I_URL", "https://example.invalid")
	cmd := attributeMappingsListCmd
	resetCmdFlags(t, cmd)
	flags["app-id"] = appID
	for n, v := range flags {
		if err := cmd.Flags().Set(n, v); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	restoreCmdWriters(t)
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetContext(context.Background())
	t.Cleanup(func() { cmd.SetOut(nil); cmd.SetErr(nil) })
	err := cmd.RunE(cmd, nil)
	return out.String(), reqs, err
}

func amItem(id, app string) string {
	return `{"userAttributeMapping":{"id":"` + id + `","appId":"` + app + `"}}`
}

func TestAttributeMappingsListAppFilterAcrossPages(t *testing.T) {
	pages := map[string]string{
		"":   `{"list":[` + amItem("a1", "x") + `,` + amItem("b1", "y") + `],"nextPageToken":"t2"}`,
		"t2": `{"list":[` + amItem("c1", "y") + `],"nextPageToken":"t3"}`,
		"t3": `{"list":[` + amItem("d1", "x") + `,` + amItem("e1", "x") + `]}`,
	}
	ids := func(out string) string {
		var got []string
		for _, l := range strings.Fields(out) {
			var row map[string]any
			_ = json.Unmarshal([]byte(l), &row)
			got = append(got, row["id"].(string))
		}
		return strings.Join(got, ",")
	}
	out, reqs, err := amListPages(t, "x", map[string]string{}, pages)
	if err != nil || ids(out) != "a1,d1,e1" || reqs != 3 {
		t.Errorf("all pages: ids %s, %d requests, err %v; want a1,d1,e1 over 3 requests", ids(out), reqs, err)
	}
	out, _, err = amListPages(t, "x", map[string]string{"limit": "2"}, pages)
	if err != nil || ids(out) != "a1,d1" {
		t.Errorf("--limit 2: ids %s, err %v; want a1,d1", ids(out), err)
	}
	out, _, err = amListPages(t, "zzz", map[string]string{}, pages)
	if err != nil || strings.TrimSpace(out) != "" {
		t.Errorf("no match: out %q err %v; want nothing at exit 0", out, err)
	}
}

func TestAttributeMappingsListAppFilterRefusesUnrecognizedShape(t *testing.T) {
	pages := map[string]string{"": `{"list":[{"userAttributeMapping":{"id":"a1"}}]}`}
	out, _, err := amListPages(t, "x", map[string]string{}, pages)
	if err == nil || exitCode(err) != exitServer || out != "" {
		t.Errorf("out %q err %v exit %d; want exit 6 and no rows", out, err, exitCode(err))
	}
}
