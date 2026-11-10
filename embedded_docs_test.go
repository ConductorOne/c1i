package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestEmbeddedDocsMatchRepoFiles runs the real binary with no credentials
// and a dead C1I_URL, so a pass also proves neither command needs auth or
// the network.
func TestEmbeddedDocsMatchRepoFiles(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "c1i")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	env := append(os.Environ(),
		"HOME="+dir, "XDG_CONFIG_HOME="+dir, "APPDATA="+dir,
		"C1I_URL=http://127.0.0.1:1", "C1I_CLIENT_ID=", "C1I_CLIENT_SECRET=")

	for sub, file := range map[string]string{"readme": "README.md", "changelog": "CHANGELOG.md"} {
		want, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		c := exec.Command(bin, "docs", sub)
		c.Env, c.Stdout, c.Stderr = env, &stdout, &stderr
		if err := c.Run(); err != nil {
			t.Fatalf("docs %s: %v\n%s", sub, err, stderr.String())
		}
		if !bytes.Equal(stdout.Bytes(), want) {
			t.Errorf("docs %s output differs from %s (%d vs %d bytes)", sub, file, stdout.Len(), len(want))
		}
	}
}
