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

	got, err := promptForRoles(authLoginCmd, strings.NewReader("\n9\n4\n"), menuRoles)
	if err != nil {
		t.Fatalf("promptForRoles: %v", err)
	}
	if !reflect.DeepEqual(menuIDs(got), []string{"r-logs"}) {
		t.Errorf("chose %v, want [r-logs]", menuIDs(got))
	}
	for _, want := range []string{
		" 0) Full permissions",
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

// TestPromptsReturnOnCancel pins that Ctrl-C (a canceled context) ends a
// prompt blocked on input, so the helper credential's deferred delete runs.
func TestPromptsReturnOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	authLoginCmd.SetOut(io.Discard)
	authLoginCmd.SetContext(ctx)
	t.Cleanup(func() { authLoginCmd.SetOut(nil); authLoginCmd.SetContext(context.Background()) })

	blocked, w := io.Pipe() // never written: a read blocks like an idle terminal
	t.Cleanup(func() { _ = w.Close() })

	if _, err := promptForRoles(authLoginCmd, blocked, menuRoles); !errors.Is(err, context.Canceled) {
		t.Errorf("promptForRoles error = %v, want context.Canceled", err)
	}
	if _, err := askToChooseRoles(authLoginCmd, blocked); !errors.Is(err, context.Canceled) {
		t.Errorf("askToChooseRoles error = %v, want context.Canceled", err)
	}
	if _, err := promptForURL(authLoginCmd, blocked); !errors.Is(err, context.Canceled) {
		t.Errorf("promptForURL error = %v, want context.Canceled", err)
	}

	// offerSaveURL reads os.Stdin; a cancel must save nothing.
	home := t.TempDir()
	t.Setenv("HOME", home)
	r, w2, err := os.Pipe() // never written, like an idle terminal
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = saved; _ = r.Close(); _ = w2.Close() })
	if err := offerSaveURL(authLoginCmd, "https://acme.example.invalid"); !errors.Is(err, context.Canceled) {
		t.Errorf("offerSaveURL error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(home + "/.c1i.yaml"); err == nil {
		t.Error("offerSaveURL saved the URL despite the cancel")
	}
}

func TestPromptForRolesEOFIsUsageError(t *testing.T) {
	authLoginCmd.SetOut(io.Discard)
	authLoginCmd.SetContext(context.Background())
	t.Cleanup(func() { authLoginCmd.SetOut(nil) })

	_, err := promptForRoles(authLoginCmd, strings.NewReader(""), menuRoles)
	if code := exitCode(err); code != exitUsage {
		t.Errorf("exit code = %d (%v), want %d", code, err, exitUsage)
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
			t.Cleanup(func() { authLoginCmd.SetOut(nil) })
			authLoginCmd.SetContext(context.Background())
			got, err := askToChooseRoles(authLoginCmd, strings.NewReader(tt.input))
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
			name: "regular user: held roles plus the personal-client page's set",
			held: []string{"r-apps"},
			want: []string{"Application Administrator", "Basic User", "Read-Only Administrator", "Read-Only to System Logs"},
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
			got := filterDelegable(testCatalog, held)
			var names []string
			for _, r := range got {
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
	if got := storedClientID(base); got != "cid-1" {
		t.Errorf("after store: %q, want cid-1", got)
	}
	t.Setenv("C1I_CLIENT_ID", "env-id")
	t.Setenv("C1I_CLIENT_SECRET", "env-sec")
	if got := storedClientID(base); got != "cid-1" {
		t.Errorf("with env credentials set: %q, want the stored cid-1", got)
	}
}

func TestPreviousCredentialNote(t *testing.T) {
	if got := previousCredentialNote("happy-otter-1@tenant/pcc"); !strings.Contains(got, "c1i did not revoke the previous credential (happy-otter-1@tenant/pcc); revoke it under your personal clients") {
		t.Errorf("personal client note = %q", got)
	}
	if got := previousCredentialNote("sp-cred@tenant/spc"); strings.Contains(got, "personal clients") {
		t.Errorf("non-personal-client note names personal clients: %q", got)
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
		{"display name with half a credential", map[string][]string{"display-name": {"x"}, "client-id": {"id"}}, "only to browser login"},
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
// create/delete, recording what it saw.
type stubTenant struct {
	t          *testing.T
	mu         sync.Mutex
	created    []map[string]any
	deleted    []string
	rolesFail  bool
	deleteFail bool
	// finalStatus, when set, fails every create except the helper's.
	finalStatus int
}

func (s *stubTenant) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/auth/v1/device_authorization":
		_, _ = fmt.Fprint(w, `{"device_code":"dc","user_code":"ABCD","verification_uri_complete":"https://example.invalid/verify","expires_in":60,"interval":1}`)
	case r.URL.Path == "/auth/v1/token":
		_, _ = fmt.Fprint(w, `{"access_token":"device-tok"}`)
	case r.URL.Path == "/api/v1/auth/introspect":
		_, _ = fmt.Fprint(w, `{"userId":"u-1"}`)
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
			_, _ = fmt.Fprintf(w, `{"list":[{"id":%q,"name":"system:application-admin","displayName":"Application Administrator"}],"nextPageToken":"p2"}`, stubAppsRoleID)
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
		if s.finalStatus != 0 && body["displayName"] != helperDisplayName {
			w.WriteHeader(s.finalStatus)
			_, _ = fmt.Fprint(w, `{"code":16,"message":"denied"}`)
			return
		}
		n := len(s.created)
		_, _ = fmt.Fprintf(w, `{"client":{"id":"pc-%d","clientId":"pc-%d@tenant/pcc"},"clientSecret":"sec-%d"}`, n, n, n)
	case strings.HasPrefix(r.URL.Path, "/api/v1/iam/personal_clients/") && r.Method == http.MethodDelete:
		if s.deleteFail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		s.deleted = append(s.deleted, strings.TrimPrefix(r.URL.Path, "/api/v1/iam/personal_clients/"))
		_, _ = fmt.Fprint(w, `{}`)
	default:
		s.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

// runBrowserLogin drives loginWithBrowser against tenant with stdin as the
// menu input. Token minting is https-only, so both the helper credential's
// client and the post-login verify go through stub clients.
func runBrowserLogin(t *testing.T, tenant *stubTenant, flags map[string]string, stdin string) (string, string, error) {
	t.Helper()
	srv := httptest.NewServer(tenant)
	t.Cleanup(srv.Close)

	stubGetClient(t, srv)
	origHelper := newHelperClient
	newHelperClient = func(_ *cobra.Command, _, _, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client(), client.WithMaxRetries(0)), nil
	}
	origOpen := openBrowser
	openBrowser = func(string) error { return nil }
	t.Cleanup(func() { newHelperClient, openBrowser = origHelper, origOpen })
	t.Cleanup(func() { _, _ = keychain.Delete(config.KeychainService(srv.URL)) })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.WriteString(stdin)
	_ = w.Close()
	saved := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = saved; _ = r.Close() })

	resetCmdFlags(t, authLoginCmd)
	for k, v := range flags {
		if err := authLoginCmd.Flags().Set(k, v); err != nil {
			t.Fatalf("set --%s: %v", k, err)
		}
	}
	var out, errOut bytes.Buffer
	authLoginCmd.SetOut(&out)
	authLoginCmd.SetErr(&errOut)
	t.Cleanup(func() { authLoginCmd.SetOut(nil); authLoginCmd.SetErr(nil) })
	authLoginCmd.SetContext(context.Background())

	scope, err := loginScopeFromFlags(authLoginCmd)
	if err != nil {
		t.Fatalf("loginScopeFromFlags: %v", err)
	}
	err = loginWithBrowser(authLoginCmd, srv.URL, scope)
	return out.String(), errOut.String(), err
}

// Role ids in the stub tenant: the id format, but lowercase so the
// placeholder guard doesn't read them as copied tenant data.
const (
	stubAppsRoleID = "apps00000000000000000000000"
	stubUserRoleID = "user00000000000000000000000"
)

func TestAuthLoginScopedRoleIDSkipsRoleLookup(t *testing.T) {
	tenant := &stubTenant{t: t}
	out, _, err := runBrowserLogin(t, tenant, map[string]string{"scoped-role": stubAppsRoleID, "display-name": "laptop"}, "")
	if err != nil {
		t.Fatalf("login: %v\n%s", err, out)
	}
	want := []map[string]any{{"displayName": "laptop", "scopedRoles": []any{stubAppsRoleID}}}
	if !reflect.DeepEqual(tenant.created, want) {
		t.Errorf("created = %#v, want only %#v (no helper credential)", tenant.created, want)
	}
	if !strings.Contains(out, "Credential scoped to role ids: "+stubAppsRoleID) {
		t.Errorf("output missing scope line:\n%s", out)
	}
}

func TestAuthLoginChooseRolesUsesAndDeletesHelper(t *testing.T) {
	tenant := &stubTenant{t: t}
	// Menu: 1) Application Administrator *  2) Basic User.
	out, _, err := runBrowserLogin(t, tenant, map[string]string{"choose-roles": "true"}, "2\n")
	if err != nil {
		t.Fatalf("login: %v\n%s", err, out)
	}
	want := []map[string]any{
		{"displayName": helperDisplayName},
		{"displayName": "Created by c1i", "scopedRoles": []any{stubUserRoleID}},
	}
	if !reflect.DeepEqual(tenant.created, want) {
		t.Errorf("created = %#v, want %#v", tenant.created, want)
	}
	if !reflect.DeepEqual(tenant.deleted, []string{"pc-1"}) {
		t.Errorf("deleted = %v, want the helper [pc-1]", tenant.deleted)
	}
	if !strings.Contains(out, "Credential scoped to: Basic User") {
		t.Errorf("output missing scope line:\n%s", out)
	}
}

func TestAuthLoginChooseFullPermissions(t *testing.T) {
	tenant := &stubTenant{t: t}
	out, _, err := runBrowserLogin(t, tenant, map[string]string{"choose-roles": "true"}, "0\n")
	if err != nil {
		t.Fatalf("login: %v\n%s", err, out)
	}
	if last := tenant.created[len(tenant.created)-1]; last["scopedRoles"] != nil {
		t.Errorf("final credential = %#v, want it unscoped", last)
	}
	if !strings.Contains(out, "Credential inherits all of your roles.") {
		t.Errorf("output missing inherit line:\n%s", out)
	}
}

func TestAuthLoginChooseRolesDeletesHelperOnLookupFailure(t *testing.T) {
	tenant := &stubTenant{t: t, rolesFail: true}
	_, _, err := runBrowserLogin(t, tenant, map[string]string{"choose-roles": "true"}, "1\n")
	if code := exitCode(err); code != exitAuth {
		t.Errorf("exit code = %d (%v), want %d", code, err, exitAuth)
	}
	if len(tenant.created) != 1 || !reflect.DeepEqual(tenant.deleted, []string{"pc-1"}) {
		t.Errorf("created %d, deleted %v; want the helper alone, then deleted", len(tenant.created), tenant.deleted)
	}
}

func TestAuthLoginWarnsWhenHelperDeleteFails(t *testing.T) {
	tenant := &stubTenant{t: t, deleteFail: true}
	out, errOut, err := runBrowserLogin(t, tenant, map[string]string{"choose-roles": "true"}, "1\n")
	if err != nil {
		t.Fatalf("login: %v\n%s", err, out)
	}
	if !strings.Contains(errOut, "could not delete the temporary credential") || !strings.Contains(errOut, "pc-1") {
		t.Errorf("stderr missing the leftover-helper warning:\n%s", errOut)
	}
}

// runBrowserLoginAsTTY drives browserLogin with isTerminal forced true, so the
// first-login prompt and the re-login note are exercised.
func runBrowserLoginAsTTY(t *testing.T, seed func(baseURL string), stdin string) string {
	t.Helper()
	srv := httptest.NewServer(&stubTenant{t: t})
	t.Cleanup(srv.Close)
	seed(srv.URL)

	stubGetClient(t, srv)
	origOpen, origTTY := openBrowser, isTerminal
	openBrowser = func(string) error { return nil }
	isTerminal = func() bool { return true }
	t.Cleanup(func() { openBrowser, isTerminal = origOpen, origTTY })
	t.Cleanup(func() { _, _ = keychain.Delete(config.KeychainService(srv.URL)) })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.WriteString(stdin)
	_ = w.Close()
	saved := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = saved; _ = r.Close() })

	resetCmdFlags(t, authLoginCmd)
	var out bytes.Buffer
	authLoginCmd.SetOut(&out)
	t.Cleanup(func() { authLoginCmd.SetOut(nil) })
	authLoginCmd.SetContext(context.Background())
	if err := browserLogin(authLoginCmd, srv.URL, loginScope{}); err != nil {
		t.Fatalf("browserLogin: %v\n%s", err, out.String())
	}
	return out.String()
}

func TestAuthLoginFirstLoginAsks(t *testing.T) {
	out := runBrowserLoginAsTTY(t, func(string) {}, "1\n")
	if !strings.Contains(out, "What access should it have?") {
		t.Errorf("first login did not ask:\n%s", out)
	}
	if strings.Contains(out, "did not revoke") {
		t.Errorf("first login printed a previous-credential note:\n%s", out)
	}
}

func TestAuthLoginReloginSkipsPromptAndNamesPrevious(t *testing.T) {
	seed := func(baseURL string) {
		if _, err := keychain.Store(config.KeychainService(baseURL), "old-1@tenant/pcc", "old-sec"); err != nil {
			t.Fatal(err)
		}
	}
	out := runBrowserLoginAsTTY(t, seed, "")
	if strings.Contains(out, "What access should it have?") {
		t.Errorf("re-login asked the first-login question:\n%s", out)
	}
	if !strings.Contains(out, "c1i did not revoke the previous credential (old-1@tenant/pcc)") {
		t.Errorf("re-login did not name the previous credential:\n%s", out)
	}
}

func TestAuthLoginExpiryHintOnlyOn401(t *testing.T) {
	for _, tc := range []struct {
		status   int
		wantHint bool
	}{{http.StatusUnauthorized, true}, {http.StatusForbidden, false}} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			_, _, err := runBrowserLogin(t, &stubTenant{t: t, finalStatus: tc.status}, map[string]string{"choose-roles": "true"}, "1\n")
			if err == nil {
				t.Fatal("want an error from the final create")
			}
			if got := strings.Contains(err.Error(), "approval may have expired"); got != tc.wantHint {
				t.Errorf("expiry hint present = %v, want %v: %v", got, tc.wantHint, err)
			}
		})
	}
}

func TestMatchRoles(t *testing.T) {
	catalog := []roleListItem{
		{ID: stubUserRoleID, Name: roleBasicUser, DisplayName: "Basic User"},
		{ID: stubAppsRoleID, Name: "system:application-admin", DisplayName: "Application Administrator"},
		{ID: "aud100000000000000000000000", Name: "custom:aud-1", DisplayName: "Auditor"},
		{ID: "aud200000000000000000000000", Name: "custom:aud-2", DisplayName: "Auditor"},
		{ID: "decoy0000000000000000000000", Name: "custom:decoy", DisplayName: "system:user"},
	}
	tests := []struct {
		values  []string
		want    []string
		wantErr string
	}{
		{values: []string{"basic-user"}, want: []string{stubUserRoleID}},
		{values: []string{"Basic User"}, want: []string{stubUserRoleID}},
		{values: []string{"BASIC_USER"}, want: []string{stubUserRoleID}},
		// A role's name outranks another role's identical display name.
		{values: []string{"system:user"}, want: []string{stubUserRoleID}},
		{values: []string{stubAppsRoleID, "basic-user", "Basic User"}, want: []string{stubAppsRoleID, stubUserRoleID}},
		{values: []string{"auditor"}, wantErr: "matches several roles"},
		{values: []string{"basic-usr"}, wantErr: "matches no role; roles: Application Administrator; Auditor [aud100000000000000000000000]; Auditor [aud200000000000000000000000]; Basic User; system:user"},
		{values: []string{"zzzz00000000000000000000000"}, wantErr: "matches no role"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.values, ","), func(t *testing.T) {
			got, err := matchRoles(tt.values, catalog)
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

func TestAuthLoginScopedRoleNameResolvesWithHelper(t *testing.T) {
	tenant := &stubTenant{t: t}
	out, _, err := runBrowserLogin(t, tenant, map[string]string{"scoped-role": "basic-user"}, "")
	if err != nil {
		t.Fatalf("login: %v\n%s", err, out)
	}
	want := []map[string]any{
		{"displayName": helperDisplayName},
		{"displayName": "Created by c1i", "scopedRoles": []any{stubUserRoleID}},
	}
	if !reflect.DeepEqual(tenant.created, want) {
		t.Errorf("created = %#v, want %#v", tenant.created, want)
	}
	if !reflect.DeepEqual(tenant.deleted, []string{"pc-1"}) {
		t.Errorf("deleted = %v, want the helper [pc-1]", tenant.deleted)
	}
	if !strings.Contains(out, "Credential scoped to: Basic User") {
		t.Errorf("output missing scope line:\n%s", out)
	}
}

func TestAuthLoginScopedRoleUnknownNameCreatesNothing(t *testing.T) {
	tenant := &stubTenant{t: t}
	_, _, err := runBrowserLogin(t, tenant, map[string]string{"scoped-role": "basic-usr"}, "")
	if code := exitCode(err); code != exitUsage || !strings.Contains(err.Error(), "matches no role") {
		t.Fatalf("err = %v (exit %d), want a usage error naming the miss", err, code)
	}
	if len(tenant.created) != 1 || !reflect.DeepEqual(tenant.deleted, []string{"pc-1"}) {
		t.Errorf("created %d, deleted %v; want only the helper, then deleted", len(tenant.created), tenant.deleted)
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

func TestAuthLoginScopedRoleIDsAreDeduped(t *testing.T) {
	tenant := &stubTenant{t: t}
	if err := runScopedLogin(t, tenant, stubAppsRoleID, stubAppsRoleID); err != nil {
		t.Fatalf("login: %v", err)
	}
	if got := tenant.created[0]["scopedRoles"]; !reflect.DeepEqual(got, []any{stubAppsRoleID}) {
		t.Errorf("scopedRoles = %v, want one id", got)
	}
}

func TestAuthLoginMixedIDAndNameChecksBoth(t *testing.T) {
	tenant := &stubTenant{t: t}
	if err := runScopedLogin(t, tenant, stubAppsRoleID, "basic-user"); err != nil {
		t.Fatalf("login: %v", err)
	}
	final := tenant.created[len(tenant.created)-1]
	if got := final["scopedRoles"]; !reflect.DeepEqual(got, []any{stubAppsRoleID, stubUserRoleID}) {
		t.Errorf("scopedRoles = %v, want both, resolved", got)
	}
	if len(tenant.created) != 2 {
		t.Errorf("created %d, want the helper then the credential", len(tenant.created))
	}
}

// TestAuthLoginNotFoundHintOnlyForBareIDs pins that the "id may not exist"
// hint appears only when ids went to the server unchecked.
func TestAuthLoginNotFoundHintOnlyForBareIDs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		values   []string
		wantHint bool
	}{
		{"bare id", []string{stubAppsRoleID}, true},
		{"resolved name", []string{"basic-user"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := runScopedLogin(t, &stubTenant{t: t, finalStatus: http.StatusNotFound}, tc.values...)
			if code := exitCode(err); code != exitNotFound {
				t.Fatalf("exit = %d (%v), want %d", code, err, exitNotFound)
			}
			if got := strings.Contains(err.Error(), "may not exist"); got != tc.wantHint {
				t.Errorf("hint present = %v, want %v: %v", got, tc.wantHint, err)
			}
		})
	}
	_, _, err := runBrowserLogin(t, &stubTenant{t: t, finalStatus: http.StatusNotFound}, map[string]string{"choose-roles": "true"}, "1\n")
	if err == nil || strings.Contains(err.Error(), "may not exist") {
		t.Errorf("--choose-roles 404: err = %v, want no --scoped-role hint", err)
	}
}

// runScopedLogin runs a browser login with --scoped-role set to values.
func runScopedLogin(t *testing.T, tenant *stubTenant, values ...string) error {
	t.Helper()
	srv := httptest.NewServer(tenant)
	t.Cleanup(srv.Close)
	stubGetClient(t, srv)
	origHelper, origOpen := newHelperClient, openBrowser
	newHelperClient = func(_ *cobra.Command, _, _, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client(), client.WithMaxRetries(0)), nil
	}
	openBrowser = func(string) error { return nil }
	t.Cleanup(func() { newHelperClient, openBrowser = origHelper, origOpen })
	t.Cleanup(func() { _, _ = keychain.Delete(config.KeychainService(srv.URL)) })

	authLoginCmd.SetOut(io.Discard)
	authLoginCmd.SetErr(io.Discard)
	t.Cleanup(func() { authLoginCmd.SetOut(nil); authLoginCmd.SetErr(nil) })
	authLoginCmd.SetContext(context.Background())
	return loginWithBrowser(authLoginCmd, srv.URL, loginScope{roles: values})
}
