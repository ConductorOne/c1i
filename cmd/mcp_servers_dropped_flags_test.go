package cmd

import (
	"strings"
	"testing"
)

// A flag the chosen type or auth method would silently drop must be a usage
// error: the request body carries only the selected arm, so the value could
// never reach the server.
func TestRegisterRefusesConfigAndAuthFlagsItWouldDrop(t *testing.T) {
	hosted := writeTempJSON(t, "hosted.json", `{"mcpServerCatalogId":"cat1"}`)
	external := writeTempJSON(t, "external.json", `{"url":"https://mcp.example.invalid/sse"}`)

	cases := []struct {
		name string
		set  map[string]string
		want string
	}{
		{
			"external with the hosted config file",
			map[string]string{"type": "external", "server-url": "https://mcp.example.invalid/sse", "hosted-config-file": hosted},
			"--hosted-config-file is for --type hosted",
		},
		{
			"hosted with the external config file",
			map[string]string{"type": "hosted", "catalog-id": "cat1", "external-config-file": external},
			"--external-config-file is for --type external",
		},
		{
			"auth none with a bearer token",
			map[string]string{"type": "external", "server-url": "https://mcp.example.invalid/sse", "auth": "none", "bearer-token": "SECRET"},
			"--auth none takes no credential flags: drop --bearer-token",
		},
		{
			"a credential flag without --auth",
			map[string]string{"type": "external", "server-url": "https://mcp.example.invalid/sse", "bearer-token": "SECRET"},
			"requires --auth (with the method it belongs to)",
		},
		{
			"a companion of another method",
			map[string]string{"type": "external", "server-url": "https://mcp.example.invalid/sse", "auth": "bearer-token", "bearer-token": "SECRET", "header-name": "X-Key"},
			"--auth bearer-token does not take --header-name",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newServerFlagCmd()
			_ = cmd.Flags().Set("app-id", "app123")
			_ = cmd.Flags().Set("display-name", "X")
			for k, v := range tc.set {
				if err := cmd.Flags().Set(k, v); err != nil {
					t.Fatalf("set %s: %v", k, err)
				}
			}
			if _, err := buildRegisterBody(cmd); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if got := exitCode(func() error { _, err := buildRegisterBody(cmd); return err }()); got != exitUsage {
				t.Errorf("exit = %d, want %d", got, exitUsage)
			}
		})
	}
}

// The valid forms still build: the selected auth arm, and no credential flags
// with none.
func TestRegisterStillAcceptsMatchingFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  map[string]string
	}{
		{"bearer token", map[string]string{"auth": "bearer-token", "bearer-token": "SECRET"}},
		{"no auth", map[string]string{"auth": "none"}},
		{"no auth flag", map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newServerFlagCmd()
			_ = cmd.Flags().Set("app-id", "app123")
			_ = cmd.Flags().Set("type", "external")
			_ = cmd.Flags().Set("display-name", "X")
			_ = cmd.Flags().Set("server-url", "https://mcp.example.invalid/sse")
			for k, v := range tc.set {
				_ = cmd.Flags().Set(k, v)
			}
			if _, err := buildRegisterBody(cmd); err != nil {
				t.Fatalf("buildRegisterBody: %v", err)
			}
		})
	}
}

// update-credentials builds only the config arm named by --type, so the other
// file is refused there too (before any request).
func TestUpdateCredentialsRefusesTheOtherConfigFile(t *testing.T) {
	hosted := writeTempJSON(t, "hosted.json", `{"mcpServerCatalogId":"cat1"}`)
	srv := newMutServer(t, fixedReply(200, `{}`))
	res := runMutationCLI(t, srv.srv, 0, nil,
		"mcp", "servers", "update-credentials", "cn1", "--app-id", "a1", "--type", "external",
		"--hosted-config-file", hosted, "--dry-run")
	if res.exit != exitUsage || res.err == nil || !strings.Contains(res.err.Error(), "--hosted-config-file is for --type hosted") {
		t.Fatalf("exit %d err %v, want exit %d with the wrong-file message", res.exit, res.err, exitUsage)
	}
	if n := len(srv.requests()); n != 0 {
		t.Errorf("%d request(s) sent, want none", n)
	}
}
