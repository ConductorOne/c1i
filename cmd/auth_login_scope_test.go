package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/config"
	"github.com/ConductorOne/c1i/internal/keychain"
	"github.com/spf13/cobra"
)

// Role ids in the stub tenant: the id format, but lowercase so the
// placeholder guard doesn't read them as copied tenant data.
const (
	stubAppsRoleID = "apps00000000000000000000000"
	stubUserRoleID = "user00000000000000000000000"
	stubCampRoleID = "camp00000000000000000000000"
)

func scannerOf(s string) *lineReader { return newLineReader(strings.NewReader(s)) }

var menuRoles = []menuRole{
	{roleListItem: roleListItem{ID: "r-apps", DisplayName: "Application Administrator"}, Held: true},
	{roleListItem: roleListItem{ID: "r-aud1", DisplayName: "Auditor"}},
	{roleListItem: roleListItem{ID: "r-aud2", DisplayName: "Auditor"}},
	{roleListItem: roleListItem{ID: "r-logs", DisplayName: "Read-Only to System Logs"}},
}

func menuIDs(roles []menuRole) []string {
	var out []string
	for _, r := range roles {
		out = append(out, r.ID)
	}
	return out
}

func TestParseRoleSelection(t *testing.T) {
	tests := []struct {
		answer  string
		want    []string
		wantErr bool
	}{
		{answer: "0", want: nil},
		{answer: "1,4", want: []string{"r-apps", "r-logs"}},
		{answer: "4 1", want: []string{"r-logs", "r-apps"}},
		{answer: "2, 2", want: []string{"r-aud1"}},
		{answer: "", wantErr: true},
		{answer: "   ", wantErr: true},
		{answer: "0,1", wantErr: true},
		{answer: "5", wantErr: true},
		{answer: "-1", wantErr: true},
		{answer: "1,x", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.answer), func(t *testing.T) {
			got, err := parseRoleSelection(tt.answer, menuRoles)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got %v", menuIDs(got))
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(menuIDs(got), tt.want) {
				t.Errorf("chose %v, want %v", menuIDs(got), tt.want)
			}
		})
	}
}

func TestPromptForRolesRepromptsOnBlankAndInvalid(t *testing.T) {
	var out bytes.Buffer
	authLoginCmd.SetOut(&out)
	authLoginCmd.SetContext(context.Background())
	t.Cleanup(func() { authLoginCmd.SetOut(nil) })

	got, err := promptForRoles(authLoginCmd, scannerOf("\n9\n4\n"), menuRoles)
	if err != nil {
		t.Fatalf("promptForRoles: %v", err)
	}
	if !reflect.DeepEqual(menuIDs(got), []string{"r-logs"}) {
		t.Errorf("chose %v, want [r-logs]", menuIDs(got))
	}
	for _, want := range []string{
		" 0) All of your roles",
		" 1) Application Administrator *",
		" 2) Auditor [r-aud1]\n",
		" 4) Read-Only to System Logs\n",
		"enter at least one number",
		`invalid choice "9"`,
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("menu output missing %q:\n%s", want, out.String())
		}
	}
}

func TestPromptForRolesEOFIsUsageError(t *testing.T) {
	authLoginCmd.SetOut(io.Discard)
	authLoginCmd.SetContext(context.Background())
	t.Cleanup(func() { authLoginCmd.SetOut(nil) })

	_, err := promptForRoles(authLoginCmd, scannerOf(""), menuRoles)
	if code := exitCode(err); code != exitUsage {
		t.Errorf("exit code = %d (%v), want %d", code, err, exitUsage)
	}
}

// TestPromptsReturnOnCancel pins that Ctrl-C (a canceled context) ends a
// prompt blocked on input instead of hanging until a second Ctrl-C kills it.
func TestPromptsReturnOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	authLoginCmd.SetOut(io.Discard)
	authLoginCmd.SetContext(ctx)
	t.Cleanup(func() { authLoginCmd.SetOut(nil); authLoginCmd.SetContext(context.Background()) })

	blocked, w := io.Pipe() // never written: a read blocks like an idle terminal
	t.Cleanup(func() { _ = w.Close() })
	in := newLineReader(blocked)

	if _, err := promptForRoles(authLoginCmd, in, menuRoles); !errors.Is(err, context.Canceled) {
		t.Errorf("promptForRoles error = %v, want context.Canceled", err)
	}
	if _, err := askToChooseRoles(authLoginCmd, in); !errors.Is(err, context.Canceled) {
		t.Errorf("askToChooseRoles error = %v, want context.Canceled", err)
	}
	if _, err := promptForURL(authLoginCmd, in); !errors.Is(err, context.Canceled) {
		t.Errorf("promptForURL error = %v, want context.Canceled", err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	offerSaveURL(authLoginCmd, in, "https://acme.example.invalid")
	if _, err := os.Stat(home + "/.c1i.yaml"); err == nil {
		t.Error("offerSaveURL saved the URL despite the cancel")
	}
}

func TestAskToChooseRoles(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"\n", false},
		{"1\n", false},
		{"2\n", true},
		{"x\n2\n", true},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.input), func(t *testing.T) {
			authLoginCmd.SetOut(io.Discard)
			authLoginCmd.SetContext(context.Background())
			t.Cleanup(func() { authLoginCmd.SetOut(nil) })
			got, err := askToChooseRoles(authLoginCmd, scannerOf(tt.input))
			if err != nil || got != tt.want {
				t.Errorf("askToChooseRoles = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}

var testCatalog = []roleListItem{
	{ID: "r-owner", Name: roleSuperAdmin, DisplayName: "Super Administrator"},
	{ID: "r-viewer", Name: roleReadOnlySuperAdmin, DisplayName: "Read-Only Administrator"},
	{ID: "r-user", Name: roleBasicUser, DisplayName: "Basic User"},
	{ID: "r-apps", Name: "system:application-admin", DisplayName: "Application Administrator"},
	{ID: "r-camp", Name: "system:campaign-admin", DisplayName: "Campaign Administrator"},
	{ID: "r-logs", Name: "system:system-logs-reader", DisplayName: "Read-Only to System Logs", SystemAPIOnly: true},
}

func TestFilterDelegable(t *testing.T) {
	all := []string{"Application Administrator", "Basic User", "Campaign Administrator", "Read-Only Administrator", "Read-Only to System Logs", "Super Administrator"}
	tests := []struct {
		name string
		held []string
		want []string
	}{
		{
			name: "regular user: held roles plus Basic User and Read-Only Administrator",
			held: []string{"r-apps"},
			want: []string{"Application Administrator", "Basic User", "Read-Only Administrator"},
		},
		{
			name: "a held API-only role is offered",
			held: []string{"r-logs"},
			want: []string{"Basic User", "Read-Only Administrator", "Read-Only to System Logs"},
		},
		{name: "super admin sees every role", held: []string{"r-owner"}, want: all},
		{name: "read-only super admin sees every role", held: []string{"r-viewer"}, want: all},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			held := map[string]bool{}
			for _, id := range tt.held {
				held[id] = true
			}
			var names []string
			for _, r := range filterDelegable(testCatalog, held) {
				names = append(names, r.DisplayName)
				if r.Held != held[r.ID] {
					t.Errorf("%s: Held = %v, want %v", r.DisplayName, r.Held, held[r.ID])
				}
			}
			if !reflect.DeepEqual(names, tt.want) {
				t.Errorf("roles = %v, want %v", names, tt.want)
			}
		})
	}
}

func TestMatchRoles(t *testing.T) {
	held := map[string]bool{"r-apps": true, "r-aud1": true, "r-aud2": true, "r-decoy": true}
	catalog := append(append([]roleListItem{}, testCatalog...),
		roleListItem{ID: "r-aud1", Name: "custom:aud-1", DisplayName: "Auditor", SystemAPIOnly: true},
		roleListItem{ID: "r-aud2", Name: "custom:aud-2", DisplayName: "Auditor", SystemAPIOnly: true},
		roleListItem{ID: "r-decoy", Name: "custom:decoy", DisplayName: "system:user", SystemAPIOnly: true},
	)
	lookup := roleLookup{offered: filterDelegable(catalog, held), catalog: catalog}
	tests := []struct {
		values  []string
		want    []string
		wantErr string
	}{
		{values: []string{"basic-user"}, want: []string{"r-user"}},
		{values: []string{"Basic User"}, want: []string{"r-user"}},
		{values: []string{"BASIC_USER"}, want: []string{"r-user"}},
		{values: []string{"r-apps", "basic-user", "Basic User"}, want: []string{"r-apps", "r-user"}},
		// A role's name and another role's display name collide: refuse to guess.
		{values: []string{"system:user"}, wantErr: "matches several roles"},
		{values: []string{"auditor"}, wantErr: "matches several roles"},
		{values: []string{"campaign-administrator"}, wantErr: "you don't hold Campaign Administrator [r-camp]"},
		{values: []string{"r-camp"}, wantErr: "you don't hold Campaign Administrator"},
		{values: []string{"read-only-to-system-logs"}, wantErr: "you don't hold Read-Only to System Logs"},
		{values: []string{"basic-usr"}, wantErr: "matches no role; choose from: Application Administrator; Auditor; Auditor; Basic User; Read-Only Administrator; system:user"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.values, ","), func(t *testing.T) {
			got, err := matchRoles(tt.values, lookup)
			if tt.wantErr != "" {
				if code := exitCode(err); code != exitUsage || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v (exit %d), want usage error containing %q", err, code, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(menuIDs(got), tt.want) {
				t.Errorf("ids = %v, want %v", menuIDs(got), tt.want)
			}
		})
	}
}

func TestRoleIDPattern(t *testing.T) {
	for v, want := range map[string]bool{
		stubUserRoleID:                 true,
		stubUserRoleID[:26]:            false,
		stubUserRoleID + "0":           false,
		"user-0000000000000000000000x": false,
		"basic-user":                   false,
	} {
		if got := roleIDPattern.MatchString(v); got != want {
			t.Errorf("roleIDPattern(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestPrintableStripsControlCharacters(t *testing.T) {
	if got := printable("Auditor\n  7) Basic User\x1b]0;x\x07"); got != "Auditor  7) Basic User]0;x" {
		t.Errorf("printable = %q", got)
	}
}

func TestCredentialName(t *testing.T) {
	user := menuRole{roleListItem: roleListItem{ID: "r-user", DisplayName: "Basic User"}}
	if got := credentialName(nil, nil); got != "Created by c1i" {
		t.Errorf("unscoped = %q", got)
	}
	if got := credentialName([]string{"r-user"}, nil); got != "Created by c1i (scoped)" {
		t.Errorf("bare ids = %q", got)
	}
	if got := credentialName([]string{"r-user"}, []menuRole{user}); got != "Created by c1i (Basic User)" {
		t.Errorf("named = %q", got)
	}
	var long []menuRole
	for i := 0; i < 20; i++ {
		long = append(long, menuRole{roleListItem: roleListItem{ID: fmt.Sprint(i), DisplayName: "Some Long Role Name"}})
	}
	if got := credentialName([]string{"x"}, long); got != "Created by c1i (20 roles)" {
		t.Errorf("long = %q", got)
	}
}

func TestSameRoles(t *testing.T) {
	if !sameRoles([]string{"a", "b"}, []string{"b", "a"}) || !sameRoles(nil, []string{}) {
		t.Error("equal sets reported different")
	}
	if sameRoles([]string{"a"}, nil) || sameRoles([]string{"a"}, []string{"a", "b"}) {
		t.Error("different sets reported equal")
	}
}

func TestStoredClientID(t *testing.T) {
	const base = "https://stored-client-id.example.invalid"
	service := config.KeychainService(base)
	t.Cleanup(func() { _, _ = keychain.Delete(service) })

	if got := storedClientID(base); got != "" {
		t.Fatalf("before store: %q, want empty", got)
	}
	if _, err := keychain.Store(service, "cid-1", "sec"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("C1I_CLIENT_ID", "env-id")
	t.Setenv("C1I_CLIENT_SECRET", "env-sec")
	if got := storedClientID(base); got != "cid-1" {
		t.Errorf("with env credentials set: %q, want the stored cid-1", got)
	}

	const legacyBase = "https://legacy-tenant.conductor.one"
	legacy := config.LegacyKeychainService(legacyBase)
	t.Cleanup(func() { _, _ = keychain.Delete(legacy) })
	if _, err := keychain.Store(legacy, "old-1", "sec"); err != nil {
		t.Fatal(err)
	}
	if got := storedClientID(legacyBase); got != "old-1" {
		t.Errorf("legacy key: %q, want old-1", got)
	}
}

// TestAuthLoginScopeFlagConflictsAreUsageErrors drives RunE: each bad
// combination must fail with exit 2 before any request or URL prompt.
func TestAuthLoginScopeFlagConflictsAreUsageErrors(t *testing.T) {
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = devnull.Close() }()
	saved := os.Stdin
	os.Stdin = devnull
	t.Cleanup(func() { os.Stdin = saved })

	tests := []struct {
		name    string
		flags   map[string][]string
		wantMsg string
	}{
		{"choose and scoped together", map[string][]string{"choose-roles": {"true"}, "scoped-role": {"r-1"}}, "mutually exclusive"},
		{"choose without a terminal", map[string][]string{"choose-roles": {"true"}}, "interactive terminal"},
		{"scoped role with credential login", map[string][]string{"scoped-role": {"r-1"}, "client-id": {"id"}, "client-secret": {"sec"}}, "only to browser login"},
		{"choose-roles=false with half a credential", map[string][]string{"choose-roles": {"false"}, "client-id": {"id"}}, "only to browser login"},
		{"empty scoped role", map[string][]string{"scoped-role": {""}}, "scoped-role"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// No URL anywhere: a misuse must be reported before the URL is needed.
			t.Setenv("C1I_URL", "")
			resetCmdFlags(t, authLoginCmd)
			for name, vals := range tt.flags {
				for _, v := range vals {
					if err := authLoginCmd.Flags().Set(name, v); err != nil {
						t.Fatalf("set --%s: %v", name, err)
					}
				}
			}
			authLoginCmd.SetContext(context.Background())
			err := authLoginCmd.RunE(authLoginCmd, nil)
			if code := exitCode(err); code != exitUsage {
				t.Errorf("exit code = %d (%v), want %d", code, err, exitUsage)
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %v, want it to mention %q", err, tt.wantMsg)
			}
		})
	}
}

// stubTenant answers the device flow, the role lookup, and personal-client
// create/delete, recording personal-client events in order.
type stubTenant struct {
	t          *testing.T
	mu         sync.Mutex
	created    []map[string]any
	events     []string
	rolesFail  bool
	deleteFail bool
	dropScope  bool // the server ignores scopedRoles
	noAccess   bool // introspect is forbidden to the final credential
	authOnly   bool // the final credential keeps only the Auth service
	verifyFail bool // introspect rejects the final credential
	onRoles    func()
}

func (s *stubTenant) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v1/iam/roles" && s.onRoles != nil {
		s.onRoles()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/auth/v1/device_authorization":
		_, _ = fmt.Fprint(w, `{"device_code":"dc","user_code":"ABCD","verification_uri_complete":"https://example.invalid/verify","expires_in":60,"interval":1}`)
	case r.URL.Path == "/auth/v1/token":
		_, _ = fmt.Fprint(w, `{"access_token":"device-tok"}`)
	case r.URL.Path == "/api/v1/auth/introspect":
		final := len(s.created) > 0 && s.created[len(s.created)-1]["displayName"] != helperDisplayName
		switch {
		case s.verifyFail && final:
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = fmt.Fprint(w, `{"code":16,"message":"unauthenticated"}`)
		case s.noAccess && final:
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprint(w, `{"code":7,"message":"Permission denied, missing permission/c1.api.auth.v1.Auth.Introspect"}`)
		case s.authOnly && final:
			_, _ = fmt.Fprint(w, `{"userId":"u-1","roles":["role/c1.api.auth.v1.Auth:reflection"],"permissions":["permission/c1.api.auth.v1.Auth.Introspect"]}`)
		default:
			_, _ = fmt.Fprint(w, `{"userId":"u-1","roles":["role/c1.api.app.v1.Apps:viewer"],"permissions":[]}`)
		}
	case r.URL.Path == "/api/v1/users/u-1":
		_, _ = fmt.Fprintf(w, `{"userView":{"user":{"roleIds":[%q]}}}`, stubAppsRoleID)
	case r.URL.Path == "/api/v1/iam/roles":
		if s.rolesFail {
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprint(w, `{"code":7,"message":"Permission denied"}`)
			return
		}
		// Two pages, so the lookup must paginate to reach Basic User.
		if r.URL.Query().Get("page_token") == "" {
			_, _ = fmt.Fprintf(w, `{"list":[{"id":%q,"name":"system:application-admin","displayName":"Application Administrator"},{"id":%q,"name":"system:campaign-admin","displayName":"Campaign Administrator"}],"nextPageToken":"p2"}`, stubAppsRoleID, stubCampRoleID)
			return
		}
		_, _ = fmt.Fprintf(w, `{"list":[{"id":%q,"name":"system:user","displayName":"Basic User"}]}`, stubUserRoleID)
	case r.URL.Path == "/api/v1/iam/personal_clients" && r.Method == http.MethodPost:
		if got := r.Header.Get("Authorization"); got != "Bearer device-tok" {
			s.t.Errorf("personal_clients create Authorization = %q, want the device token", got)
		}
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		s.created = append(s.created, body)
		n := len(s.created)
		kind := "final"
		if body["displayName"] == helperDisplayName {
			kind = "helper"
		}
		s.events = append(s.events, fmt.Sprintf("create %s pc-%d", kind, n))
		scope := body["scopedRoles"]
		if scope == nil || s.dropScope {
			scope = []any{}
		}
		resp, _ := json.Marshal(map[string]any{
			"client":       map[string]any{"id": fmt.Sprintf("pc-%d", n), "clientId": fmt.Sprintf("pc-%d@tenant/pcc", n), "scopedRoles": scope},
			"clientSecret": fmt.Sprintf("sec-%d", n),
		})
		_, _ = w.Write(resp)
	case strings.HasPrefix(r.URL.Path, "/api/v1/iam/personal_clients/") && r.Method == http.MethodDelete:
		if s.deleteFail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		s.events = append(s.events, "delete "+strings.TrimPrefix(r.URL.Path, "/api/v1/iam/personal_clients/"))
		_, _ = fmt.Fprint(w, `{}`)
	default:
		s.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

type loginRun struct {
	out, errOut string
	err         error
	baseURL     string
}

// runLogin drives browserLogin against tenant. Token minting is https-only, so
// every credential client is a stub client; stdin feeds the prompts.
func runLogin(t *testing.T, tenant *stubTenant, scope loginScope, stdin string, tty bool, seed func(baseURL string)) loginRun {
	t.Helper()
	srv := httptest.NewServer(tenant)
	t.Cleanup(srv.Close)
	if seed != nil {
		seed(srv.URL)
	}

	origCred, origOpen, origTTY := newCredentialClient, openBrowser, isTerminal
	newCredentialClient = func(_ *cobra.Command, _, _, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client(), client.WithMaxRetries(0)), nil
	}
	openBrowser = func(string) error { return nil }
	isTerminal = func() bool { return tty }
	t.Cleanup(func() { newCredentialClient, openBrowser, isTerminal = origCred, origOpen, origTTY })
	t.Cleanup(func() { _, _ = keychain.Delete(config.KeychainService(srv.URL)) })

	var out, errOut bytes.Buffer
	authLoginCmd.SetOut(&out)
	authLoginCmd.SetErr(&errOut)
	t.Cleanup(func() { authLoginCmd.SetOut(nil); authLoginCmd.SetErr(nil) })
	if authLoginCmd.Context() == nil {
		authLoginCmd.SetContext(context.Background())
	}
	err := browserLogin(authLoginCmd, scannerOf(stdin), srv.URL, scope)
	return loginRun{out: out.String(), errOut: errOut.String(), err: err, baseURL: srv.URL}
}

func storedFor(baseURL string) string {
	return keychain.StoredClientID(config.KeychainService(baseURL))
}

func freshContext(t *testing.T) {
	t.Helper()
	authLoginCmd.SetContext(context.Background())
}

func TestLoginScopedRoleIDsSkipLookup(t *testing.T) {
	freshContext(t)
	tenant := &stubTenant{t: t}
	r := runLogin(t, tenant, loginScope{roles: []string{stubAppsRoleID, stubAppsRoleID}}, "", false, nil)
	if r.err != nil {
		t.Fatalf("login: %v\n%s", r.err, r.out)
	}
	want := []map[string]any{{"displayName": "Created by c1i (scoped)", "scopedRoles": []any{stubAppsRoleID}}}
	if !reflect.DeepEqual(tenant.created, want) {
		t.Errorf("created = %#v, want only %#v (no helper, ids deduped)", tenant.created, want)
	}
	if !strings.Contains(r.out, "Credential scoped to: "+stubAppsRoleID) {
		t.Errorf("output missing scope line:\n%s", r.out)
	}
}

func TestLoginScopedRoleNameDeletesHelperFirst(t *testing.T) {
	freshContext(t)
	tenant := &stubTenant{t: t}
	r := runLogin(t, tenant, loginScope{roles: []string{"basic-user"}}, "", false, nil)
	if r.err != nil {
		t.Fatalf("login: %v\n%s", r.err, r.out)
	}
	if want := []string{"create helper pc-1", "delete pc-1", "create final pc-2"}; !reflect.DeepEqual(tenant.events, want) {
		t.Errorf("events = %v, want %v", tenant.events, want)
	}
	if got := tenant.created[0]["expires"]; got != helperLifetime {
		t.Errorf("helper expires = %v, want %s", got, helperLifetime)
	}
	if got := tenant.created[1]; !reflect.DeepEqual(got, map[string]any{"displayName": "Created by c1i (Basic User)", "scopedRoles": []any{stubUserRoleID}}) {
		t.Errorf("final create = %#v", got)
	}
	if !strings.Contains(r.out, "Credential scoped to: Basic User ["+stubUserRoleID+"]") {
		t.Errorf("output missing scope line:\n%s", r.out)
	}
}

func TestLoginScopedRoleRefusals(t *testing.T) {
	for _, tc := range []struct {
		values []string
		want   string
	}{
		{[]string{"basic-usr"}, "matches no role"},
		{[]string{"campaign-administrator"}, "you don't hold Campaign Administrator"},
		// With a name present, ids are checked too.
		{[]string{stubCampRoleID, "basic-user"}, "you don't hold Campaign Administrator"},
	} {
		t.Run(strings.Join(tc.values, ","), func(t *testing.T) {
			freshContext(t)
			tenant := &stubTenant{t: t}
			r := runLogin(t, tenant, loginScope{roles: tc.values}, "", false, nil)
			if code := exitCode(r.err); code != exitUsage || !strings.Contains(r.err.Error(), tc.want) {
				t.Fatalf("err = %v (exit %d), want usage error containing %q", r.err, code, tc.want)
			}
			if want := []string{"create helper pc-1", "delete pc-1"}; !reflect.DeepEqual(tenant.events, want) {
				t.Errorf("events = %v, want only the helper, then deleted", tenant.events)
			}
		})
	}
}

func TestLoginScopedRoleMixedIDAndName(t *testing.T) {
	freshContext(t)
	tenant := &stubTenant{t: t}
	r := runLogin(t, tenant, loginScope{roles: []string{stubAppsRoleID, "basic-user"}}, "", false, nil)
	if r.err != nil {
		t.Fatalf("login: %v", r.err)
	}
	if got := tenant.created[1]["scopedRoles"]; !reflect.DeepEqual(got, []any{stubAppsRoleID, stubUserRoleID}) {
		t.Errorf("scopedRoles = %v, want both", got)
	}
}

func TestLoginChooseRolesDeletesHelperBeforeMenu(t *testing.T) {
	freshContext(t)
	tenant := &stubTenant{t: t}
	// Menu: 1) Application Administrator *  2) Basic User. The unheld Campaign
	// Administrator is left out.
	r := runLogin(t, tenant, loginScope{choose: true}, "2\n", true, nil)
	if r.err != nil {
		t.Fatalf("login: %v\n%s", r.err, r.out)
	}
	if strings.Contains(r.out, "Campaign Administrator") {
		t.Errorf("menu offered an unheld role:\n%s", r.out)
	}
	if want := []string{"create helper pc-1", "delete pc-1", "create final pc-2"}; !reflect.DeepEqual(tenant.events, want) {
		t.Errorf("events = %v, want %v", tenant.events, want)
	}
	if !strings.Contains(r.out, "Credential scoped to: Basic User") {
		t.Errorf("output missing scope line:\n%s", r.out)
	}
}

func TestLoginChooseAllRoles(t *testing.T) {
	freshContext(t)
	tenant := &stubTenant{t: t}
	r := runLogin(t, tenant, loginScope{choose: true}, "0\n", true, nil)
	if r.err != nil {
		t.Fatalf("login: %v\n%s", r.err, r.out)
	}
	if last := tenant.created[len(tenant.created)-1]; last["scopedRoles"] != nil || last["displayName"] != "Created by c1i" {
		t.Errorf("final credential = %#v, want unscoped", last)
	}
	if !strings.Contains(r.out, "Credential has all of your roles.") {
		t.Errorf("output missing scope line:\n%s", r.out)
	}
}

func TestLoginLookupFailureDeletesHelper(t *testing.T) {
	freshContext(t)
	tenant := &stubTenant{t: t, rolesFail: true}
	r := runLogin(t, tenant, loginScope{choose: true}, "1\n", true, nil)
	if code := exitCode(r.err); code != exitAuth {
		t.Errorf("exit code = %d (%v), want %d", code, r.err, exitAuth)
	}
	if want := []string{"create helper pc-1", "delete pc-1"}; !reflect.DeepEqual(tenant.events, want) {
		t.Errorf("events = %v, want %v", tenant.events, want)
	}
}

// TestLoginCtrlCDuringLookupDeletesHelper cancels the context while roles are
// being read, as Ctrl-C would: the helper must still be deleted.
func TestLoginCtrlCDuringLookupDeletesHelper(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	authLoginCmd.SetContext(ctx)
	t.Cleanup(func() { authLoginCmd.SetContext(context.Background()) })
	tenant := &stubTenant{t: t, onRoles: cancel}

	r := runLogin(t, tenant, loginScope{roles: []string{"basic-user"}}, "", false, nil)
	if !errors.Is(r.err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", r.err)
	}
	if want := []string{"create helper pc-1", "delete pc-1"}; !reflect.DeepEqual(tenant.events, want) {
		t.Errorf("events = %v, want the helper deleted despite the cancel", tenant.events)
	}
}

func TestLoginHelperDeleteFailureFailsTheLogin(t *testing.T) {
	freshContext(t)
	tenant := &stubTenant{t: t, deleteFail: true}
	r := runLogin(t, tenant, loginScope{roles: []string{"basic-user"}}, "", false, nil)
	var left *helperLeftError
	if !errors.As(r.err, &left) || exitCode(r.err) == 0 {
		t.Fatalf("err = %v, want a non-zero helperLeftError", r.err)
	}
	if n := strings.Count(r.errOut, "(pc-1)"); n != 2 {
		t.Errorf("warning naming the helper printed %d times, want 2 (at once and at the end):\n%s", n, r.errOut)
	}
	if got := storedFor(r.baseURL); got != "pc-2@tenant/pcc" {
		t.Errorf("stored = %q, want the scoped credential kept", got)
	}
}

func TestLoginRejectsDroppedScope(t *testing.T) {
	freshContext(t)
	tenant := &stubTenant{t: t, dropScope: true}
	r := runLogin(t, tenant, loginScope{roles: []string{stubAppsRoleID}}, "", false, nil)
	if code := exitCode(r.err); code != exitServer || !strings.Contains(r.err.Error(), "not the requested") {
		t.Fatalf("err = %v (exit %d), want exit %d naming the mismatch", r.err, code, exitServer)
	}
	if want := []string{"create final pc-1", "delete pc-1"}; !reflect.DeepEqual(tenant.events, want) {
		t.Errorf("events = %v, want the wrongly scoped credential deleted", tenant.events)
	}
	if got := storedFor(r.baseURL); got != "" {
		t.Errorf("stored = %q, want nothing", got)
	}
}

func TestLoginRejectsNoAccess(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tenant func(*testing.T) *stubTenant
		want   string
	}{
		{"introspect forbidden", func(t *testing.T) *stubTenant { return &stubTenant{t: t, noAccess: true} }, "the credential was deleted"},
		{"only the Auth service left", func(t *testing.T) *stubTenant { return &stubTenant{t: t, authOnly: true} }, "the credential was deleted"},
		{"and it can't delete itself", func(t *testing.T) *stubTenant { return &stubTenant{t: t, noAccess: true, deleteFail: true} }, "(pc-1) could not delete itself"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			freshContext(t)
			r := runLogin(t, tc.tenant(t), loginScope{roles: []string{stubAppsRoleID}}, "", false, nil)
			if code := exitCode(r.err); code != exitUsage || !strings.Contains(r.err.Error(), "no access") || !strings.Contains(r.err.Error(), tc.want) {
				t.Fatalf("err = %v (exit %d), want a usage error containing %q", r.err, code, tc.want)
			}
			if got := storedFor(r.baseURL); got != "" {
				t.Errorf("stored = %q, want nothing", got)
			}
		})
	}
}

func TestHasNoAccess(t *testing.T) {
	forbidden := &client.APIError{StatusCode: http.StatusForbidden}
	for _, tc := range []struct {
		body string
		err  error
		want bool
	}{
		{"", forbidden, true},
		{"", &client.APIError{StatusCode: http.StatusUnauthorized}, false},
		{`{"roles":["role/c1.api.auth.v1.Auth:reflection"]}`, nil, true},
		{`{"roles":["role/c1.api.auth.v1.Auth:reflection","role/c1.api.app.v1.Apps:viewer"]}`, nil, false},
	} {
		if got := hasNoAccess([]byte(tc.body), tc.err); got != tc.want {
			t.Errorf("hasNoAccess(%s, %v) = %v, want %v", tc.body, tc.err, got, tc.want)
		}
	}
}

func TestLoginVerifyFailureKeepsPreviousCredential(t *testing.T) {
	freshContext(t)
	seed := func(baseURL string) {
		if _, err := keychain.Store(config.KeychainService(baseURL), "old-1@tenant/pcc", "old-sec"); err != nil {
			t.Fatal(err)
		}
	}
	r := runLogin(t, &stubTenant{t: t, verifyFail: true}, loginScope{}, "", false, seed)
	if r.err == nil || !strings.Contains(r.err.Error(), "pc-1") {
		t.Fatalf("err = %v, want a verification failure naming the new credential", r.err)
	}
	if got := storedFor(r.baseURL); got != "old-1@tenant/pcc" {
		t.Errorf("stored = %q, want the previous credential kept", got)
	}
}

func TestLoginFirstLoginAsksThenMenu(t *testing.T) {
	freshContext(t)
	r := runLogin(t, &stubTenant{t: t}, loginScope{}, "2\n2\n", true, nil)
	if r.err != nil {
		t.Fatalf("login: %v\n%s", r.err, r.out)
	}
	for _, want := range []string{"What access should it have?", "Choose the roles", "Credential scoped to: Basic User"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("output missing %q:\n%s", want, r.out)
		}
	}
	if strings.Contains(r.out, "was not revoked") {
		t.Errorf("first login printed a previous-credential note:\n%s", r.out)
	}
}

func TestLoginNoPromptWhenExplicitlyDeclined(t *testing.T) {
	freshContext(t)
	r := runLogin(t, &stubTenant{t: t}, loginScope{noPrompt: true}, "", true, nil)
	if r.err != nil || strings.Contains(r.out, "What access should it have?") {
		t.Errorf("err = %v; --choose-roles=false still asked:\n%s", r.err, r.out)
	}
}

func TestLoginReloginSkipsPromptAndNamesPrevious(t *testing.T) {
	freshContext(t)
	seed := func(baseURL string) {
		if _, err := keychain.Store(config.KeychainService(baseURL), "old-1@tenant/pcc", "old-sec"); err != nil {
			t.Fatal(err)
		}
	}
	r := runLogin(t, &stubTenant{t: t}, loginScope{}, "", true, seed)
	if r.err != nil {
		t.Fatalf("login: %v", r.err)
	}
	if strings.Contains(r.out, "What access should it have?") {
		t.Errorf("re-login asked the first-login question:\n%s", r.out)
	}
	if !strings.Contains(r.out, "The previous credential (old-1@tenant/pcc) was not revoked.") {
		t.Errorf("re-login did not name the previous credential:\n%s", r.out)
	}
}
