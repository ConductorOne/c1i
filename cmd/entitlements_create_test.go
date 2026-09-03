package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// newEntitlementCreateCmd builds a throwaway command carrying the real flag
// set, so a test never drifts from what the command actually registers.
func newEntitlementCreateCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "create", RunE: func(*cobra.Command, []string) error { return nil }}
	addEntitlementCreateFlags(cmd)
	cmd.SetArgs(args)
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetContext(context.Background())
	if err := cmd.Execute(); err != nil {
		t.Fatalf("parsing %v: %v", args, err)
	}
	return cmd
}

func mustPlan(t *testing.T, args ...string) *entitlementCreatePlan {
	t.Helper()
	p, err := buildEntitlementCreatePlan(newEntitlementCreateCmd(t, args...))
	if err != nil {
		t.Fatalf("buildEntitlementCreatePlan(%v): %v", args, err)
	}
	return p
}

// TestEntitlementCreatePlanCreatesAllThree pins the default sequence: a
// resource type and resource are created, both named from --display-name
// unless separately overridden, and unset optional fields are omitted rather
// than sent empty.
func TestEntitlementCreatePlanCreatesAllThree(t *testing.T) {
	p := mustPlan(t, "--app-id", "app1", "--display-name", "Payroll admin")

	if p.resourceTypeID != "" || p.resourceID != "" {
		t.Errorf("nothing was supplied to reuse, got type=%q resource=%q", p.resourceTypeID, p.resourceID)
	}
	wantType := map[string]any{"displayName": "Payroll admin", "resourceType": "CUSTOM"}
	if !reflect.DeepEqual(p.resourceTypeBody, wantType) {
		t.Errorf("resource type body = %v, want %v", p.resourceTypeBody, wantType)
	}
	wantResource := map[string]any{"displayName": "Payroll admin"}
	if !reflect.DeepEqual(p.resourceBody, wantResource) {
		t.Errorf("resource body = %v, want %v", p.resourceBody, wantResource)
	}
	wantEnt := map[string]any{"displayName": "Payroll admin"}
	if !reflect.DeepEqual(p.entitlementBody, wantEnt) {
		t.Errorf("entitlement body = %v, want %v", p.entitlementBody, wantEnt)
	}
}

// TestEntitlementCreatePlanOptionalFields pins that every optional flag reaches
// the body it belongs to, under the wire key the API expects.
func TestEntitlementCreatePlanOptionalFields(t *testing.T) {
	p := mustPlan(t,
		"--app-id", "app1", "--display-name", "Payroll admin",
		"--description", "pays people", "--slug", "member", "--alias", "payroll_admin",
		"--owner-id", "u1", "--owner-id", "u2",
		"--resource-type", "profile-type",
		"--resource-type-display-name", "Payroll role",
		"--resource-display-name", "Payroll admins",
	)

	if got := p.resourceTypeBody["displayName"]; got != "Payroll role" {
		t.Errorf("resource type displayName = %v", got)
	}
	// Lower case and "-" are normalized; the API takes only PROFILE_TYPE.
	if got := p.resourceTypeBody["resourceType"]; got != "PROFILE_TYPE" {
		t.Errorf("resourceType = %v, want PROFILE_TYPE", got)
	}
	if got := p.resourceBody["displayName"]; got != "Payroll admins" {
		t.Errorf("resource displayName = %v", got)
	}
	want := map[string]any{
		"displayName":            "Payroll admin",
		"description":            "pays people",
		"slug":                   "member",
		"alias":                  "payroll_admin",
		"appEntitlementOwnerIds": []string{"u1", "u2"},
	}
	if !reflect.DeepEqual(p.entitlementBody, want) {
		t.Errorf("entitlement body = %v, want %v", p.entitlementBody, want)
	}
}

// TestEntitlementCreatePlanReusesSuppliedIDs pins that a supplied id skips
// exactly the step that would have created that object, and no more.
func TestEntitlementCreatePlanReusesSuppliedIDs(t *testing.T) {
	t.Run("resource type only", func(t *testing.T) {
		p := mustPlan(t, "--app-id", "app1", "--display-name", "e", "--resource-type-id", "rt1")
		if p.resourceTypeBody != nil {
			t.Errorf("resource type body = %v, want nil (reusing rt1)", p.resourceTypeBody)
		}
		if p.resourceBody == nil {
			t.Error("resource body is nil; a resource must still be created")
		}
	})
	t.Run("both", func(t *testing.T) {
		p := mustPlan(t, "--app-id", "app1", "--display-name", "e", "--resource-type-id", "rt1", "--resource-id", "r1")
		if p.resourceTypeBody != nil || p.resourceBody != nil {
			t.Errorf("bodies = %v/%v, want both nil (entitlement only)", p.resourceTypeBody, p.resourceBody)
		}
		body := p.fullEntitlementBody(p.resourceTypeID, p.resourceID)
		if body["appResourceTypeId"] != "rt1" || body["appResourceId"] != "r1" {
			t.Errorf("entitlement body = %v, want the supplied ids", body)
		}
	})
}

// TestEntitlementCreatePlanUsageErrors pins the combinations refused before any
// request is sent. Each would otherwise either 400 on the server or silently
// create something the caller did not ask for.
func TestEntitlementCreatePlanUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			"resource id without its type",
			[]string{"--app-id", "app1", "--display-name", "e", "--resource-id", "r1"},
			"--resource-id requires --resource-type-id",
		},
		{
			// --resource-type has a default, so an explicit "" is a mistake.
			// It used to reach the wire as an empty enum.
			"explicitly empty resource type",
			[]string{"--app-id", "app1", "--display-name", "e", "--resource-type", ""},
			"flag --resource-type requires a non-empty value",
		},
		{
			// With --resource-type-id set, the reuse rejection is the more
			// useful message: the flag does not belong at all.
			"empty resource type alongside a reused type",
			[]string{"--app-id", "app1", "--display-name", "e", "--resource-type-id", "rt1", "--resource-type", ""},
			"--resource-type only applies when this command creates that object",
		},
		{
			"resource type kind alongside a reused type",
			[]string{"--app-id", "app1", "--display-name", "e", "--resource-type-id", "rt1", "--resource-type", "ROLE"},
			"--resource-type only applies",
		},
		{
			"resource type name alongside a reused type",
			[]string{"--app-id", "app1", "--display-name", "e", "--resource-type-id", "rt1", "--resource-type-display-name", "x"},
			"--resource-type-display-name only applies",
		},
		{
			"resource name alongside a reused resource",
			[]string{"--app-id", "app1", "--display-name", "e", "--resource-type-id", "rt1", "--resource-id", "r1", "--resource-display-name", "x"},
			"--resource-display-name only applies",
		},
		{
			"empty resource type id",
			[]string{"--app-id", "app1", "--display-name", "e", "--resource-type-id", ""},
			"flag --resource-type-id requires a non-empty value",
		},
		{
			"empty resource id",
			[]string{"--app-id", "app1", "--display-name", "e", "--resource-type-id", "rt1", "--resource-id", ""},
			"flag --resource-id requires a non-empty value",
		},
		{
			// Both display-name overrides would otherwise fall back to
			// --display-name and mis-name the object.
			"empty resource type display name",
			[]string{"--app-id", "app1", "--display-name", "e", "--resource-type-display-name", ""},
			"flag --resource-type-display-name requires a non-empty value",
		},
		{
			"empty resource display name",
			[]string{"--app-id", "app1", "--display-name", "e", "--resource-display-name", ""},
			"flag --resource-display-name requires a non-empty value",
		},
		{
			// An unset shell variable: the entitlement would otherwise be
			// created ownerless at exit 0, and an async owner read cannot tell
			// that apart from "not provisioned yet".
			"empty owner id",
			[]string{"--app-id", "app1", "--display-name", "e", "--owner-id", ""},
			"--owner-id values must be non-empty",
		},
		{
			"empty owner id alongside a real one",
			[]string{"--app-id", "app1", "--display-name", "e", "--owner-id", "", "--owner-id", "u1"},
			"--owner-id values must be non-empty",
		},
		{
			"whitespace-only owner id",
			[]string{"--app-id", "app1", "--display-name", "e", "--owner-id", "  "},
			"--owner-id values must be non-empty",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildEntitlementCreatePlan(newEntitlementCreateCmd(t, tc.args...))
			if err == nil {
				t.Fatalf("expected a usage error for %v", tc.args)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
			if code := exitCode(err); code != exitUsage {
				t.Errorf("exit code = %d, want %d", code, exitUsage)
			}
		})
	}
}

// entitlementCreateServer is a stub of the three create endpoints. It records
// each (path, body) it receives and answers with ids the next call must pick
// up, so a break in the chain shows up as a wrong path or a wrong id.
type entitlementCreateServer struct {
	srv     *httptest.Server
	paths   []string
	bodies  []map[string]any
	failEnt bool
	failRes bool
}

func newEntitlementCreateServer(t *testing.T, failEnt bool) *entitlementCreateServer {
	t.Helper()
	s := &entitlementCreateServer{failEnt: failEnt}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding body for %s: %v", r.URL.Path, err)
		}
		s.paths = append(s.paths, r.URL.Path)
		s.bodies = append(s.bodies, body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/resource_types"):
			_, _ = w.Write([]byte(`{"appResourceType":{"id":"rt-new"},"expanded":[]}`))
		case strings.HasSuffix(r.URL.Path, "/resources"):
			if s.failRes {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"code":3,"message":"nope"}`))
				return
			}
			_, _ = w.Write([]byte(`{"appResource":{"id":"res-new"}}`))
		case strings.HasSuffix(r.URL.Path, "/entitlements"):
			if s.failEnt {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"code":3,"message":"nope"}`))
				return
			}
			_, _ = w.Write([]byte(`{"appEntitlementView":{"appEntitlement":{"id":"ent-new"}}}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// TestEntitlementCreateRunChainsIDs drives the wired end-to-end path: each
// response's id must address the next request, and the entitlement body must
// carry both ids the server requires.
func TestEntitlementCreateRunChainsIDs(t *testing.T) {
	s := newEntitlementCreateServer(t, false)
	p := mustPlan(t, "--app-id", "app1", "--display-name", "Payroll admin")

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetContext(context.Background())

	if err := p.run(cmd, client.NewForTesting(s.srv.URL, s.srv.Client())); err != nil {
		t.Fatalf("run: %v", err)
	}

	wantPaths := []string{
		"/api/v1/apps/app1/resource_types",
		"/api/v1/apps/app1/resource_types/rt-new/resources",
		"/api/v1/apps/app1/entitlements",
	}
	if !reflect.DeepEqual(s.paths, wantPaths) {
		t.Errorf("paths = %v, want %v", s.paths, wantPaths)
	}
	ent := s.bodies[2]
	if ent["appResourceTypeId"] != "rt-new" || ent["appResourceId"] != "res-new" {
		t.Errorf("entitlement body = %v, want the ids from the two preceding responses", ent)
	}
	if !strings.Contains(out.String(), `"id": "ent-new"`) {
		t.Errorf("output = %q, want the raw entitlement response", out.String())
	}
}

// TestEntitlementCreateRunReportsPartialCreates pins that a failure part-way
// through names what already exists and how to reuse it -- the objects are not
// rolled back, so an unnamed id would be an orphan the caller can't find.
func TestEntitlementCreateRunReportsPartialCreates(t *testing.T) {
	s := newEntitlementCreateServer(t, true)
	p := mustPlan(t, "--app-id", "app1", "--display-name", "Payroll admin")

	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetContext(context.Background())

	err := p.run(cmd, client.NewForTesting(s.srv.URL, s.srv.Client()))
	if err == nil {
		t.Fatal("expected the entitlement create to fail")
	}
	for _, want := range []string{"--resource-type-id rt-new", "--resource-id res-new"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %q", err, want)
		}
	}
	// The 400 must still classify as a usage error, not be masked by the wrap.
	if code := exitCode(err); code != exitUsage {
		t.Errorf("exit code = %d, want %d", code, exitUsage)
	}
}

// TestEntitlementCreateRunReusedObjectsAreNotClaimed pins that the failure
// message only names objects THIS run created: reporting a caller-supplied id
// as "already created" would invite them to delete their own resource type.
func TestEntitlementCreateRunReusedObjectsAreNotClaimed(t *testing.T) {
	s := newEntitlementCreateServer(t, true)
	p := mustPlan(t, "--app-id", "app1", "--display-name", "e", "--resource-type-id", "rt-mine", "--resource-id", "res-mine")

	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetContext(context.Background())

	err := p.run(cmd, client.NewForTesting(s.srv.URL, s.srv.Client()))
	if err == nil {
		t.Fatal("expected the entitlement create to fail")
	}
	if strings.Contains(err.Error(), "already created") {
		t.Errorf("error = %q, but this run created nothing", err)
	}
	if len(s.paths) != 1 || s.paths[0] != "/api/v1/apps/app1/entitlements" {
		t.Errorf("paths = %v, want the entitlement create only", s.paths)
	}
}

// TestEntitlementCreateDryRunPreviewsEveryRequest pins that a dry run shows all
// three writes, not just the first -- the other two are writes the caller would
// otherwise not see coming.
func TestEntitlementCreateDryRunPreviewsEveryRequest(t *testing.T) {
	p := mustPlan(t, "--app-id", "app1", "--display-name", "Payroll admin")

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	if err := p.previewRequests(cmd); err != nil {
		t.Fatalf("printDryRun: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"[dry-run] POST /api/v1/apps/app1/resource_types\n",
		"[dry-run] POST /api/v1/apps/app1/resource_types/" + newResourceTypeIDPlaceholder + "/resources\n",
		"[dry-run] POST /api/v1/apps/app1/entitlements\n",
		`"appResourceId": "` + newResourceIDPlaceholder + `"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("dry run output missing %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "[dry-run] POST"); n != 3 {
		t.Errorf("previewed %d requests, want 3:\n%s", n, got)
	}

	// A fully-specified run has nothing to stand in for, so it must not print
	// placeholders at all.
	out.Reset()
	single := mustPlan(t, "--app-id", "app1", "--display-name", "e", "--resource-type-id", "rt1", "--resource-id", "r1")
	if err := single.previewRequests(cmd); err != nil {
		t.Fatalf("printDryRun: %v", err)
	}
	if strings.Contains(out.String(), "NEW_APP_RESOURCE") {
		t.Errorf("placeholder leaked into a fully-specified preview:\n%s", out.String())
	}
}

// TestCreatedObjectIDRejectsUnusableResponse pins that a 200 carrying no id is
// a server failure (exit 6), not a success that sends the next request to a
// path with an empty segment.
func TestCreatedObjectIDRejectsUnusableResponse(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"not json", `<html>nope</html>`},
		{"key missing", `{"expanded":[]}`},
		{"id empty", `{"appResourceType":{"id":""}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := createdObjectID([]byte(tc.data), "appResourceType")
			if err == nil {
				t.Fatalf("id = %q, want an error", id)
			}
			var nonJSON *nonJSONResponseError
			if !errors.As(err, &nonJSON) {
				t.Errorf("error %v is not a *nonJSONResponseError, so it would exit 1", err)
			}
			if code := exitCode(err); code != exitServer {
				t.Errorf("exit code = %d, want %d", code, exitServer)
			}
		})
	}
}

// TestResourceTypeKindsAreDocumented holds the one enum list to every place
// that restates it. The command deliberately does NOT validate --resource-type
// against this list -- the server stays authoritative if the enum grows -- but
// the help text and docs freeze it either way, and a contract restated in four
// places drifts unless something fails when it does.
func TestResourceTypeKindsAreDocumented(t *testing.T) {
	docs := map[string]string{
		"entitlements create --help": entitlementsCreateCmd.Long,
		"--resource-type flag usage": entitlementsCreateCmd.Flags().Lookup("resource-type").Usage,
		"README.md":                  readDocFile(t, "../README.md"),
		"guideConfigureNewApp":       guideConfigureNewApp,
	}
	for name, doc := range docs {
		for _, kind := range resourceTypeKinds {
			if !strings.Contains(doc, kind) {
				t.Errorf("%s does not name the %q resource type kind", name, kind)
			}
		}
	}

	// The default must be one of them, or --help promises a value that 400s.
	def := entitlementsCreateCmd.Flags().Lookup("resource-type").DefValue
	if !slices.Contains(resourceTypeKinds, def) {
		t.Errorf("--resource-type defaults to %q, which is not in resourceTypeKinds", def)
	}
}

// TestEntitlementCreateDryRunBannerNamesOnlyUsedPlaceholders pins that the
// banner announces exactly the stand-ins the preview goes on to use. Naming
// one that never appears is the one place a dry run could overclaim.
func TestEntitlementCreateDryRunBannerNamesOnlyUsedPlaceholders(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    []string
		notWant []string
	}{
		{
			"creates both",
			[]string{"--app-id", "app1", "--display-name", "e"},
			[]string{newResourceTypeIDPlaceholder, newResourceIDPlaceholder, "stand in for ids"},
			[]string{"stands in for an id"},
		},
		{
			"reuses the resource type",
			[]string{"--app-id", "app1", "--display-name", "e", "--resource-type-id", "rt1"},
			[]string{newResourceIDPlaceholder, "stands in for an id"},
			[]string{newResourceTypeIDPlaceholder},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&out)
			if err := mustPlan(t, tc.args...).previewRequests(cmd); err != nil {
				t.Fatalf("previewRequests: %v", err)
			}
			banner, _, _ := strings.Cut(out.String(), "\n")
			for _, w := range tc.want {
				if !strings.Contains(banner, w) {
					t.Errorf("banner %q does not name %s, which the preview uses", banner, w)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(banner, w) {
					t.Errorf("banner %q claims %q, which does not describe this preview", banner, w)
				}
				// A placeholder must not appear anywhere in a preview whose
				// id was supplied, banner or request.
				if strings.HasPrefix(w, "NEW_") && strings.Contains(out.String(), w) {
					t.Errorf("preview used %s despite the id being supplied:\n%s", w, out.String())
				}
			}
		})
	}
}

// TestEntitlementCreateDurationGrantWireKey pins the flag-name-to-field
// mapping: --duration-grant is the one flag whose wire key differs from its
// name, and a mismatch would be silently dropped by the server.
func TestEntitlementCreateDurationGrantWireKey(t *testing.T) {
	p := mustPlan(t, "--app-id", "app1", "--display-name", "e", "--duration-grant", "3600s")
	if got := p.entitlementBody["durationGrant"]; got != "3600s" {
		t.Errorf("durationGrant = %v, want 3600s (body: %v)", got, p.entitlementBody)
	}
	if _, ok := p.entitlementBody["duration-grant"]; ok {
		t.Error("the flag name leaked into the body as a wire key")
	}

	// Omitted means standing access; an empty key would pick the wrong arm of
	// the max_grant_duration oneof.
	bare := mustPlan(t, "--app-id", "app1", "--display-name", "e")
	if _, ok := bare.entitlementBody["durationGrant"]; ok {
		t.Errorf("durationGrant sent without the flag: %v", bare.entitlementBody)
	}
}

// TestNormalizeResourceType pins case/separator normalization, and that an
// unknown value is passed through for the server to rule on rather than being
// rejected against a list that can go stale.
func TestNormalizeResourceType(t *testing.T) {
	cases := map[string]string{
		"custom":        "CUSTOM",
		"CUSTOM":        "CUSTOM",
		"profile-type":  "PROFILE_TYPE",
		"profile_type":  "PROFILE_TYPE",
		"something_new": "SOMETHING_NEW",
	}
	for in, want := range cases {
		if got := normalizeResourceType(in); got != want {
			t.Errorf("normalizeResourceType(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestEntitlementCreateRetryRemediationIsAccepted pins that the retry the
// failure message hands back is a command that works: the ids it names make
// the create-only flags of the original invocation a usage error, so the
// message has to name those for dropping too.
func TestEntitlementCreateRetryRemediationIsAccepted(t *testing.T) {
	s := newEntitlementCreateServer(t, true)
	args := []string{
		"--app-id", "app1", "--display-name", "e",
		"--resource-type", "ROLE",
		"--resource-type-display-name", "rt name",
		"--resource-display-name", "res name",
	}

	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetContext(context.Background())
	runErr := mustPlan(t, args...).run(cmd, client.NewForTesting(s.srv.URL, s.srv.Client()))
	if runErr == nil {
		t.Fatal("expected the entitlement create to fail")
	}

	add, drop := parseRemediation(t, runErr.Error())
	retry := append(withoutFlags(args, drop), add...)
	if _, err := buildEntitlementCreatePlan(newEntitlementCreateCmd(t, retry...)); err != nil {
		t.Fatalf("the remediation %q produces %v, so the retry it advises exits 2", runErr, err)
	}
}

// parseRemediation pulls the flags to add and the flags to drop out of the
// "(already created: ...)" tail.
func parseRemediation(t *testing.T, msg string) (add, drop []string) {
	t.Helper()
	_, rest, ok := strings.Cut(msg, "re-run with ")
	if !ok {
		t.Fatalf("error %q names no retry", msg)
	}
	rest, _, ok = strings.Cut(rest, " to reuse instead of duplicating)")
	if !ok {
		t.Fatalf("error %q has no remediation tail", msg)
	}
	addPart, dropPart, hasDrop := strings.Cut(rest, ", dropping ")
	if hasDrop {
		drop = strings.Fields(strings.TrimSuffix(dropPart, ","))
	}
	return strings.Fields(addPart), drop
}

// withoutFlags removes each named flag and its value from an argument list of
// "--flag value" pairs.
func withoutFlags(args, remove []string) []string {
	var kept []string
	for i := 0; i+1 < len(args); i += 2 {
		if !slices.Contains(remove, args[i]) {
			kept = append(kept, args[i], args[i+1])
		}
	}
	return kept
}

// TestResourceTypeSingletonIsDocumented holds the server's own error string in
// every doc that promises the restriction, so it stays greppable from both
// directions. Reproduced live: a second ROLE, GROUP or VAULT resource type on
// entitlementCreateDocs are the four sources that carry this command's
// behavioural claims. The other guards in this file deliberately use narrower
// sets -- the kinds list also checks the flag's own usage string, and the
// remediation quote lives in only two docs -- so those keep their own.
func entitlementCreateDocs(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"entitlements create --help":   entitlementsCreateCmd.Long,
		"README.md":                    readDocFile(t, "../README.md"),
		"cmd/agents.md (embedded)":     agentsTemplate,
		"docs guide configure-new-app": guideConfigureNewApp,
	}
}

// one app 500s, while a second CUSTOM one succeeds.
func TestResourceTypeSingletonIsDocumented(t *testing.T) {
	const serverError = "app resource type already exists"
	docs := entitlementCreateDocs(t)
	for name, doc := range docs {
		if !strings.Contains(doc, serverError) {
			t.Errorf("%s does not quote the server's %q error", name, serverError)
		}
	}
}

// TestRemediationStringIsDocumentedVerbatim pins the docs that quote the
// partial-failure message to what createdSoFar actually emits. The message
// changed to name the flags a retry must drop, and the guide kept quoting the
// old form in the same commit — a retry copied from it would be refused.
func TestRemediationStringIsDocumentedVerbatim(t *testing.T) {
	// Built by the real planner, so a regression that stopped populating
	// typeOnlyFlags fails here instead of leaving a hand-made plan green.
	plan := mustPlan(t, "--app-id", "a", "--display-name", "e",
		"--resource-type-display-name", "rt")
	msg := strings.TrimSpace(plan.createdSoFar("<id>", ""))
	if !strings.Contains(msg, "dropping --resource-type-display-name") {
		t.Fatalf("createdSoFar produced %q, which names no flag to drop; "+
			"this guard would then pass on any doc", msg)
	}
	docs := map[string]string{
		"entitlements create --help":   entitlementsCreateCmd.Long,
		"docs guide configure-new-app": guideConfigureNewApp,
	}
	for name, doc := range docs {
		// Verbatim, not a keyword: renaming the flag must break the docs that
		// quote it, which a check for the word "dropping" alone would not.
		if !strings.Contains(flatten(doc), msg) {
			t.Errorf("%s does not quote the partial-failure message verbatim.\nwant: %s", name, msg)
		}
	}
}

// TestRemediationOnResourceFailureKeepsResourceName covers the step-2 failure:
// the resource type exists, the resource does not. The retry must drop the
// type-only flags but KEEP --resource-display-name, since the object it names
// was never created. Getting that wrong is silent — the resource would be
// created under --display-name instead — where the step-3 case fails loudly.
func TestRemediationOnResourceFailureKeepsResourceName(t *testing.T) {
	s := newEntitlementCreateServer(t, false)
	s.failRes = true

	args := []string{
		"--app-id", "app1", "--display-name", "e",
		"--resource-type", "ROLE",
		"--resource-type-display-name", "rt name",
		"--resource-display-name", "res name",
	}
	cmd := &cobra.Command{}
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetContext(context.Background())
	runErr := mustPlan(t, args...).run(cmd, client.NewForTesting(s.srv.URL, s.srv.Client()))
	if runErr == nil {
		t.Fatal("expected the resource create to fail")
	}
	msg := runErr.Error()

	// The advised retry must also be accepted, same as the step-3 case.
	add, drop := parseRemediation(t, msg)
	retry := append(withoutFlags(args, drop), add...)
	if _, err := buildEntitlementCreatePlan(newEntitlementCreateCmd(t, retry...)); err != nil {
		t.Fatalf("the step-2 remediation %q produces %v, so the retry it advises exits 2", msg, err)
	}
	if !strings.Contains(msg, "--resource-type-id") {
		t.Errorf("message does not name the created resource type: %q", msg)
	}
	if strings.Contains(msg, "--resource-id") {
		t.Errorf("message names a resource id, but the resource was never created: %q", msg)
	}
	if strings.Contains(msg, "--resource-display-name") {
		t.Errorf("message tells the caller to drop --resource-display-name, but the resource "+
			"it names does not exist yet; the retry would create it under --display-name: %q", msg)
	}
}

// reuseDropClauses are the canonical instructions, required in every doc that
// advises reusing an object. Fixed clauses, not pattern-matched prose: earlier
// heuristic versions were each satisfiable by the wrong text.
var reuseDropClauses = map[string]string{
	"--resource-type-id": "drop both --resource-type and --resource-type-display-name",
	"--resource-id":      "drop --resource-display-name",
}

// reusePairs ties each id flag to the list the code actually refuses beside it.
var reusePairs = map[string][]string{
	"--resource-type-id": typeCreateOnlyFlags,
	"--resource-id":      resourceCreateOnlyFlags,
}

// TestReuseAdviceIsDocumented holds every doc that advises reusing an existing
// object to the full list of flags the code refuses alongside its id. Advice
// naming only one of two refused flags exited 2, which is what this prevents.
func TestReuseAdviceIsDocumented(t *testing.T) {
	for idFlag, flags := range reusePairs {
		clause, ok := reuseDropClauses[idFlag]
		if !ok {
			t.Fatalf("%s refuses flags but has no documented clause", idFlag)
		}
		for _, f := range flags {
			// docMentionsFlag, not Contains: --resource-type is a prefix of
			// --resource-type-display-name, so a substring check here is
			// satisfied by the other flag in the very same clause.
			if !docMentionsFlag(clause, f) {
				t.Fatalf("the clause for %s omits --%s, which the code refuses", idFlag, f)
			}
		}
	}

	docs := entitlementCreateDocs(t)
	for name, doc := range docs {
		// Backticks stripped so a doc can format flags as code, which is these
		// files' own convention; requiring the bare form banned correct markdown.
		flat := strings.ReplaceAll(flatten(doc), "`", "")
		for idFlag, clause := range reuseDropClauses {
			if !docMentionsFlag(flat, strings.TrimPrefix(idFlag, "--")) {
				t.Errorf("%s never names %s, so its reuse advice cannot be found", name, idFlag)
				continue
			}
			at := strings.Index(flat, clause)
			if at < 0 {
				t.Errorf("%s does not carry the reuse clause for %s.\nwant: %s", name, idFlag, clause)
				continue
			}
			// A nearby negation inverts the instruction while satisfying it.
			lead := strings.ToLower(flat[max(0, at-24):at])
			if strings.Contains(lead, "not ") || strings.Contains(lead, "never ") {
				t.Errorf("%s negates the reuse clause for %s: %q", name, idFlag, flat[max(0, at-24):at+len(clause)])
			}
		}
	}
}
