package cmd

import (
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

func newAppCreateFlagCmd() *cobra.Command {
	cmd := &cobra.Command{}
	f := cmd.Flags()
	f.String("display-name", "", "")
	f.String("description", "", "")
	f.StringSlice("owner", nil, "")
	return cmd
}

// TestBuildAppCreateBodyMinimal pins that only displayName is sent when nothing
// else is supplied (no empty description/owners keys leaking into the request).
func TestBuildAppCreateBodyMinimal(t *testing.T) {
	cmd := newAppCreateFlagCmd()
	_ = cmd.Flags().Set("display-name", "Google Workspace")

	got := buildAppCreateBody(cmd)
	want := map[string]any{"displayName": "Google Workspace"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

// TestBuildAppCreateBodyFull pins optional fields flow through when set.
func TestBuildAppCreateBodyFull(t *testing.T) {
	cmd := newAppCreateFlagCmd()
	_ = cmd.Flags().Set("display-name", "Acme")
	_ = cmd.Flags().Set("description", "the app")
	_ = cmd.Flags().Set("owner", "u1")
	_ = cmd.Flags().Set("owner", "u2")

	got := buildAppCreateBody(cmd)
	if got["displayName"] != "Acme" || got["description"] != "the app" {
		t.Errorf("scalar fields = %v", got)
	}
	owners, ok := got["owners"].([]string)
	if !ok || !reflect.DeepEqual(owners, []string{"u1", "u2"}) {
		t.Errorf("owners = %v", got["owners"])
	}
}
