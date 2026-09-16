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
	"testing"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// stubServicePrincipalsClient points the non-list SPC commands at srv.
func stubServicePrincipalsClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := newServicePrincipalsClient
	newServicePrincipalsClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newServicePrincipalsClient = orig })
}

func stubListClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := newListClient
	newListClient = func(_ *cobra.Command, _ string) (*client.Client, error) {
		return client.NewForTesting(srv.URL, srv.Client()), nil
	}
	t.Cleanup(func() { newListClient = orig })
}

func TestBindingSubject(t *testing.T) {
	tests := []struct {
		name    string
		flags   map[string]string
		want    map[string]any
		wantErr bool
	}{
		{
			name:  "function id",
			flags: map[string]string{"function-id": "fn-1111111111111111111111"},
			want:  map[string]any{"functionId": "fn-1111111111111111111111"},
		},
		{
			name:  "sso application, app-scoped",
			flags: map[string]string{"sso-application-id": "app1", "sso-app-id": "parent1"},
			want:  map[string]any{"ssoApplication": map[string]any{"appId": "parent1", "id": "app1"}},
		},
		{
			name:    "no subject",
			flags:   map[string]string{},
			wantErr: true,
		},
		{
			name:    "two subjects",
			flags:   map[string]string{"function-id": "fn1", "edge-id": "e1", "edge-app-id": "a1"},
			wantErr: true,
		},
		{
			name:    "app-scoped half missing",
			flags:   map[string]string{"sso-application-id": "app1"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &cobra.Command{}
			addBindingSubjectFlags(c)
			for k, v := range tt.flags {
				if err := c.Flags().Set(k, v); err != nil {
					t.Fatalf("set --%s: %v", k, err)
				}
			}
			got, err := bindingSubject(c)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got %v", got)
				}
				if code := exitCode(err); code != exitUsage {
					t.Errorf("exit code = %d, want %d (a usage error)", code, exitUsage)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("subject = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestBuildSPCredentialCreateBody(t *testing.T) {
	tests := []struct {
		name    string
		flags   map[string][]string
		want    map[string]any
		wantErr bool
	}{
		{
			name:  "display name only omits optionals",
			flags: map[string][]string{"display-name": {"ci"}},
			want:  map[string]any{"displayName": "ci"},
		},
		{
			name: "expires converts to proto seconds string",
			flags: map[string][]string{
				"display-name": {"ci"},
				"expires":      {"720h"},
			},
			want: map[string]any{"displayName": "ci", "expires": "2592000s"},
		},
		{
			name: "fractional expires preserves nanoseconds",
			flags: map[string][]string{
				"display-name": {"ci"},
				"expires":      {"1.5s"},
			},
			want: map[string]any{"displayName": "ci", "expires": "1.500000000s"},
		},
		{
			name: "roles, cidrs and dpop",
			flags: map[string][]string{
				"display-name": {"ci"},
				"scoped-role":  {"role-a", "role-b"},
				"allow-cidr":   {"10.0.0.0/24"},
				"require-dpop": {"true"},
			},
			want: map[string]any{
				"displayName":      "ci",
				"scopedRoles":      []string{"role-a", "role-b"},
				"allowSourceCidrs": []string{"10.0.0.0/24"},
				"requireDpop":      true,
			},
		},
		{
			name:    "empty scoped role is a usage error",
			flags:   map[string][]string{"display-name": {"ci"}, "scoped-role": {""}},
			wantErr: true,
		},
		{
			name:    "empty allow CIDR is a usage error",
			flags:   map[string][]string{"display-name": {"ci"}, "allow-cidr": {""}},
			wantErr: true,
		},
		{
			name:    "invalid duration is a usage error",
			flags:   map[string][]string{"display-name": {"ci"}, "expires": {"soon"}},
			wantErr: true,
		},
		{
			name:    "non-positive duration is a usage error",
			flags:   map[string][]string{"display-name": {"ci"}, "expires": {"0s"}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetCmdFlags(t, spCredentialsCreateCmd)
			for name, vals := range tt.flags {
				for _, v := range vals {
					if err := spCredentialsCreateCmd.Flags().Set(name, v); err != nil {
						t.Fatalf("set --%s: %v", name, err)
					}
				}
			}
			got, err := buildSPCredentialCreateBody(spCredentialsCreateCmd)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got %v", got)
				}
				if code := exitCode(err); code != exitUsage {
					t.Errorf("exit code = %d, want %d (a usage error)", code, exitUsage)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("body = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestServicePrincipalsCreateSendsBody(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"servicePrincipal":{"id":"sp-1","displayName":"payments"}}`)
	}))
	defer srv.Close()

	resetCmdFlags(t, servicePrincipalsCreateCmd)
	stubServicePrincipalsClient(t, srv)
	withRealDryRun(t)
	t.Setenv("C1I_URL", "https://example.invalid")
	_ = servicePrincipalsCreateCmd.Flags().Set("display-name", "payments")

	var out bytes.Buffer
	servicePrincipalsCreateCmd.SetOut(&out)
	t.Cleanup(func() { servicePrincipalsCreateCmd.SetOut(nil) })
	servicePrincipalsCreateCmd.SetContext(context.Background())

	if err := servicePrincipalsCreateCmd.RunE(servicePrincipalsCreateCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/service_principals" {
		t.Errorf("request = %s %s, want POST /api/v1/service_principals", gotMethod, gotPath)
	}
	if want := map[string]any{"displayName": "payments"}; !reflect.DeepEqual(gotBody, want) {
		t.Errorf("body = %#v, want %#v", gotBody, want)
	}
	// Mutation output keeps the envelope (writeRawObject), so the id stays nested.
	if !bytes.Contains(out.Bytes(), []byte(`"id": "sp-1"`)) {
		t.Errorf("output missing created id: %s", out.String())
	}
}

func TestServicePrincipalsCredentialsCreateHitsNestedPath(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"credential":{"id":"a-b-12345"},"clientSecret":"shh"}`)
	}))
	defer srv.Close()

	resetCmdFlags(t, spCredentialsCreateCmd)
	stubServicePrincipalsClient(t, srv)
	withRealDryRun(t)
	t.Setenv("C1I_URL", "https://example.invalid")
	_ = spCredentialsCreateCmd.Flags().Set("display-name", "ci")

	var out bytes.Buffer
	spCredentialsCreateCmd.SetOut(&out)
	t.Cleanup(func() { spCredentialsCreateCmd.SetOut(nil) })
	spCredentialsCreateCmd.SetContext(context.Background())

	if err := spCredentialsCreateCmd.RunE(spCredentialsCreateCmd, []string{"sp-1"}); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if want := "/api/v1/service_principals/sp-1/credentials"; gotPath != want {
		t.Errorf("path = %s, want %s", gotPath, want)
	}
	if want := map[string]any{"displayName": "ci"}; !reflect.DeepEqual(gotBody, want) {
		t.Errorf("body = %#v, want %#v", gotBody, want)
	}
	if !bytes.Contains(out.Bytes(), []byte("shh")) {
		t.Errorf("secret not surfaced in output: %s", out.String())
	}
}

func TestServicePrincipalsListAutoPaginates(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page_token") == "" {
			_, _ = fmt.Fprint(w, `{"list":[{"id":"sp-1","displayName":"a"}],"nextPageToken":"tok2"}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"list":[{"id":"sp-2","displayName":"b"}],"nextPageToken":""}`)
	}))
	defer srv.Close()

	resetCmdFlags(t, servicePrincipalsListCmd)
	stubListClient(t, srv)
	t.Setenv("C1I_URL", "https://example.invalid")

	var out bytes.Buffer
	servicePrincipalsListCmd.SetOut(&out)
	t.Cleanup(func() { servicePrincipalsListCmd.SetOut(nil) })
	servicePrincipalsListCmd.SetContext(context.Background())

	if err := servicePrincipalsListCmd.RunE(servicePrincipalsListCmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("made %d requests, want 2 (auto-pagination): %v", len(paths), paths)
	}
	lines := bytes.Count(bytes.TrimSpace(out.Bytes()), []byte("\n")) + 1
	if lines != 2 {
		t.Errorf("emitted %d rows, want 2:\n%s", lines, out.String())
	}
}
