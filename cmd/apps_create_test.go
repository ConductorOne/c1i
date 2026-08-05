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
	return cmd
}

// TestBuildAppCreateBodyMinimal pins that only displayName is sent when nothing
// else is supplied (no empty description key leaking into the request).
func TestBuildAppCreateBodyMinimal(t *testing.T) {
	cmd := newAppCreateFlagCmd()
	_ = cmd.Flags().Set("display-name", "Google Workspace")

	got := buildAppCreateBody(cmd)
	want := map[string]any{"displayName": "Google Workspace"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

// TestBuildAppCreateBodyFull pins optional description flows through when set.
func TestBuildAppCreateBodyFull(t *testing.T) {
	cmd := newAppCreateFlagCmd()
	_ = cmd.Flags().Set("display-name", "Acme")
	_ = cmd.Flags().Set("description", "the app")

	got := buildAppCreateBody(cmd)
	want := map[string]any{"displayName": "Acme", "description": "the app"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}
