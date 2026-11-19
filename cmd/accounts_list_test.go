package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// accounts list --unmapped-only drops mapped accounts client-side, so its
// written row count is not 1:1 with the app users fetched. Pin that a small
// --limit neither shrinks the request page size nor ends the scan on a page of
// mapped rows: over a full page of mapped accounts followed by a page of
// unmapped ones, --limit 2 must read both pages at the full --page-size and
// emit the two unmapped rows. A --limit-driven page-size shrink (what
// effectivePageSize does for 1:1 lists) makes every request ask for the
// remaining limit instead, so the scan advances at --limit's pace.
func TestAccountsListUnmappedOnlyScansFullPagesUnderLimit(t *testing.T) {
	var gotPageSizes, gotTokens []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/search/app_users" {
			t.Errorf("unexpected request: %s %s (want POST /api/v1/search/app_users)", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body := decodeJSONBody(t, r)
		size, _ := body["pageSize"].(float64)
		tok, _ := body["pageToken"].(string)
		gotPageSizes = append(gotPageSizes, strconv.Itoa(int(size)))
		gotTokens = append(gotTokens, tok)

		var items []string
		next := ""
		switch tok {
		case "":
			// A full page of accounts, every one already linked to an
			// identity user: --unmapped-only drops all of them.
			for i := 0; i < int(size) && i < defaultPageSize; i++ {
				items = append(items, fmt.Sprintf(
					`{"appUser":{"id":%s,"appId":"app1","identityUserId":%s}}`,
					jstr(fmt.Sprintf("mapped-%d", i)), jstr(fmt.Sprintf("identity-%d", i))))
			}
			next = "tok1"
		case "tok1":
			// The unmapped accounts, on the page after the mapped one.
			items = []string{
				`{"appUser":{"id":"unmapped-1","appId":"app1"}}`,
				`{"appUser":{"id":"unmapped-2","appId":"app1"}}`,
			}
		default:
			t.Errorf("unexpected page token: %q", tok)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"list":[%s],"nextPageToken":%s}`, strings.Join(items, ","), jstr(next))
	}))
	defer srv.Close()

	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = orig })
	t.Setenv("C1I_URL", "https://example.invalid")

	resetCmdFlags(t, accountsListCmd)
	if err := accountsListCmd.Flags().Set("app-id", "app1"); err != nil {
		t.Fatalf("setting --app-id: %v", err)
	}
	if err := accountsListCmd.Flags().Set("unmapped-only", "true"); err != nil {
		t.Fatalf("setting --unmapped-only: %v", err)
	}
	if err := accountsListCmd.Flags().Set("limit", "2"); err != nil {
		t.Fatalf("setting --limit: %v", err)
	}

	var out strings.Builder
	restoreCmdWriters(t)
	accountsListCmd.SetOut(&out)
	accountsListCmd.SetContext(context.Background())
	t.Cleanup(func() {
		accountsListCmd.SetOut(nil)
		accountsListCmd.SetErr(nil)
		accountsListCmd.SetContext(context.Background())
	})
	if err := accountsListCmd.RunE(accountsListCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if len(gotTokens) != 2 || gotTokens[0] != "" || gotTokens[1] != "tok1" {
		t.Fatalf("server saw page tokens %v, want [\"\" tok1] (a stop on the mapped page would emit nothing)", gotTokens)
	}
	wantSize := strconv.Itoa(defaultPageSize)
	for i, ps := range gotPageSizes {
		if ps != wantSize {
			t.Errorf("request %d: pageSize = %q, want %q (a --limit-driven shrink would send %q here)", i, ps, wantSize, "2")
		}
	}

	rows := decodeNDJSONRows(t, out.String())
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (the unmapped accounts on page 2): %v", len(rows), rows)
	}
	if rows[0]["id"] != "unmapped-1" || rows[1]["id"] != "unmapped-2" {
		t.Errorf("got ids %v, %v, want unmapped-1, unmapped-2", rows[0]["id"], rows[1]["id"])
	}
	for i, row := range rows {
		if row["identity_user_id"] != nil {
			t.Errorf("row %d: identity_user_id = %v, want null (a mapped account leaked through)", i, row["identity_user_id"])
		}
	}
}

// `accounts list` rows carry the app user's whole profile with its real JSON
// types, so --fields reaches one attribute without a round trip through
// "accounts get".
func TestAccountsListRowsCarryProfile(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	clearViperOverrides(t, "fields")
	s := newMutServer(t, fixedReply(200,
		`{"list":[{"appUser":{"id":"a1","appId":"app1","displayName":"Ada","profile":{"department":"Eng","headcount":12,"contractor":true}}}]}`))
	stub := func() {
		orig := newListClient
		newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
			return client.NewForTesting(s.srv.URL, s.srv.Client()), nil
		}
		t.Cleanup(func() { newListClient = orig })
	}

	r := runCLI(t, stub, nil, "accounts", "list", "--app-id", "app1")
	if r.err != nil {
		t.Fatalf("exit %d: %v", r.exit, r.err)
	}
	rows := decodeNDJSONRows(t, r.stdout)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1: %q", len(rows), r.stdout)
	}
	profile, ok := rows[0]["profile"].(map[string]any)
	if !ok {
		t.Fatalf("profile = %v (%T), want the decoded object", rows[0]["profile"], rows[0]["profile"])
	}
	if profile["department"] != "Eng" {
		t.Errorf("profile.department = %v, want Eng", profile["department"])
	}
	if profile["headcount"] != float64(12) {
		t.Errorf("profile.headcount = %v (%T), want the JSON number 12", profile["headcount"], profile["headcount"])
	}
	if profile["contractor"] != true {
		t.Errorf("profile.contractor = %v, want true", profile["contractor"])
	}

	r = runCLI(t, stub, nil, "accounts", "list", "--app-id", "app1", "--fields", "profile.department")
	if r.err != nil {
		t.Fatalf("--fields: exit %d: %v", r.exit, r.err)
	}
	if got, want := strings.TrimSpace(r.stdout), `{"profile":{"department":"Eng"}}`; got != want {
		t.Errorf("--fields profile.department = %s, want %s", got, want)
	}
}
