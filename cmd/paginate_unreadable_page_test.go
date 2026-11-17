package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A 2xx page the engine cannot decode is C1 misbehaving: exit 6, not 1.
func TestListUnreadablePageExitsServerError(t *testing.T) {
	bodies := map[string]string{
		"non-JSON":         "<html>not json</html>",
		"wrong-shape JSON": `[1,2]`,
	}
	for _, tc := range listPaginationCases() {
		for name, body := range bodies {
			t.Run(tc.name+" / "+name, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = fmt.Fprint(w, body)
				}))
				defer srv.Close()
				_, _, err := runListPaginationCase(t, tc, srv)
				if err == nil {
					t.Fatal("expected an error")
				}
				if got := exitCode(err); got != exitServer {
					t.Errorf("exit = %d, want %d (err: %v)", got, exitServer, err)
				}
			})
		}
	}
}
