package cmd

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
)

// The fixed-route list (attribute-types, findings, role-mining) exits 6, like
// the paged engine, when a 200 body is not a readable list.
func TestGetFixedListUnreadablePageExitsServerError(t *testing.T) {
	for name, body := range map[string]string{
		"non-JSON":          "<html>not json</html>",
		"wrong-shape JSON":  `[1,2]`,
		"non-object item":   `{"list":[1]}`,
		"list is not array": `{"list":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("C1I_URL", mutTestURL)
			s := newMutServer(t, fixedReply(200, body))
			stubMutationClient(t, s.srv, 0)
			cmd := &cobra.Command{Use: "x"}
			restoreCmdWriters(t)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetContext(t.Context())
			err := getFixedList(cmd, "/api/v1/x", func(m map[string]any) map[string]any { return m })
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := exitCode(err); got != exitServer {
				t.Errorf("exit = %d, want %d (err: %v)", got, exitServer, err)
			}
		})
	}
}
