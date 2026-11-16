package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A path flag error names the flag and the character position, like every
// other control-character error, and does not echo the value.
func TestPathFlagErrorDoesNotEchoValue(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	srv := newMutServer(t, fixedReply(200, "{}"))
	res := runMutationCLI(t, srv.srv, 0, nil, "docs", "readme", "--output", "dev/a\nsekr3t")
	if res.exit != exitUsage || res.err == nil {
		t.Fatalf("exit %d err %v, want a usage error", res.exit, res.err)
	}
	msg := res.err.Error()
	if strings.Contains(msg, "sekr3t") || strings.Contains(msg, "dev/a") {
		t.Errorf("error echoes the value: %q", msg)
	}
	if !strings.Contains(msg, "--output") || !strings.Contains(msg, "control character U+000A at byte 5") {
		t.Errorf("error = %q, want the flag, character and byte offset", msg)
	}
}

// accessKeyId lands in the SigV4 Authorization header, in either spelling.
func TestConfigFileAccessKeyIDRefusesControlChars(t *testing.T) {
	t.Setenv("C1I_URL", mutTestURL)
	for _, arm := range []string{"awsSigv4", "aws_sigv4"} {
		for _, field := range []string{"accessKeyId", "access_key_id"} {
			file := filepath.Join(t.TempDir(), "cfg.json")
			if err := os.WriteFile(file, []byte(`{"`+arm+`":{"`+field+`":"AK\r\nX"}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			srv := newMutServer(t, fixedReply(200, "{}"))
			res := runMutationCLI(t, srv.srv, 0, nil, "mcp", "servers", "test-connection", "--external-config-file", file, "--dry-run")
			if res.exit != exitUsage || res.err == nil || !strings.Contains(res.err.Error(), arm+"."+field) {
				t.Errorf("%s.%s: exit %d err %v, want a usage error naming the path", arm, field, res.exit, res.err)
			}
			if got := srv.requests(); len(got) != 0 {
				t.Errorf("%s.%s: requests sent: %v", arm, field, got)
			}
		}
	}
}
