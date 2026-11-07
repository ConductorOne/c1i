package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ConductorOne/c1i/internal/mcpgateway"
)

func TestEmitGatewayToolsLimit(t *testing.T) {
	tools := []mcpgateway.Tool{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	for _, tc := range []struct {
		limit, want int
	}{{0, 3}, {2, 2}, {5, 3}} {
		var out bytes.Buffer
		mcpGatewayListToolsCmd.SetOut(&out)
		if err := emitGatewayTools(mcpGatewayListToolsCmd, tools, false, tc.limit); err != nil {
			t.Fatalf("limit %d: %v", tc.limit, err)
		}
		if got := strings.Count(out.String(), "\n"); got != tc.want {
			t.Errorf("limit %d: %d rows, want %d", tc.limit, got, tc.want)
		}
	}
	mcpGatewayListToolsCmd.SetOut(nil)
}

func TestGatewayListToolsLimitFlag(t *testing.T) {
	resetCmdFlags(t, mcpGatewayListToolsCmd)
	if mcpGatewayListToolsCmd.Flags().Lookup("limit") == nil {
		t.Fatal("list-tools has no --limit flag")
	}
	if err := mcpGatewayListToolsCmd.Flags().Set("limit", "-1"); err != nil {
		t.Fatal(err)
	}
	if err := validateCountFlags(mcpGatewayListToolsCmd); exitCode(err) != exitUsage {
		t.Errorf("negative --limit: err = %v, want usage error", err)
	}
}
