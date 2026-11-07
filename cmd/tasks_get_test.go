package cmd

import (
	"net/http"
	"testing"

	"github.com/spf13/cobra"
)

// tasks get and requests get read the same endpoint: the id is escaped into
// /api/v1/tasks/{id}.
func TestTasksGetRequestsGetShareEndpoint(t *testing.T) {
	for _, c := range []*cobra.Command{tasksGetCmd, requestsGetCmd} {
		t.Run(c.CommandPath(), func(t *testing.T) {
			srv, reqs := webhooksServer(t, `{"taskView":{"task":{"id":"a/b"}}}`)
			if _, err := runWebhookCmd(t, srv, c, []string{"a/b"}, nil); err != nil {
				t.Fatalf("RunE: %v", err)
			}
			if len(*reqs) != 1 || (*reqs)[0].method != http.MethodGet || (*reqs)[0].path != "/api/v1/tasks/a%2Fb" {
				t.Errorf("requests = %+v, want one GET /api/v1/tasks/a%%2Fb", *reqs)
			}
		})
	}
}
