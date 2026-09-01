package cmd

import (
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

func newCatalogCreateFlagCmd() *cobra.Command {
	cmd := &cobra.Command{}
	f := cmd.Flags()
	f.String("display-name", "", "")
	f.String("description", "", "")
	f.Bool("published", false, "")
	f.Bool("visible-to-everyone", false, "")
	f.Bool("request-bundle", false, "")
	return cmd
}

// TestBuildCatalogCreateBodyMinimal pins that only displayName is sent when
// nothing else is supplied — no empty description and no defaulted booleans
// leaking into the request, which would silently overwrite the server's own
// defaults for a caller who never asked.
func TestBuildCatalogCreateBodyMinimal(t *testing.T) {
	cmd := newCatalogCreateFlagCmd()
	_ = cmd.Flags().Set("display-name", "Engineering")

	got := buildAccessProfileCreateBody(cmd)
	want := map[string]any{"displayName": "Engineering"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

// TestBuildCatalogCreateBodyFull pins every optional flag flowing through with
// its real JSON type, and that the flag names map to the API's camelCase keys.
func TestBuildCatalogCreateBodyFull(t *testing.T) {
	cmd := newCatalogCreateFlagCmd()
	for flag, value := range map[string]string{
		"display-name":        "Engineering",
		"description":         "eng access",
		"published":           "true",
		"visible-to-everyone": "true",
		"request-bundle":      "true",
	} {
		if err := cmd.Flags().Set(flag, value); err != nil {
			t.Fatalf("set --%s: %v", flag, err)
		}
	}

	got := buildAccessProfileCreateBody(cmd)
	want := map[string]any{
		"displayName":       "Engineering",
		"description":       "eng access",
		"published":         true,
		"visibleToEveryone": true,
		"requestBundle":     true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

// TestBuildCatalogCreateBodySendsExplicitFalse pins that an explicitly passed
// --published=false is sent as false rather than dropped: "omitted" and
// "false" are different requests, and only the flag's Changed state tells them
// apart.
func TestBuildCatalogCreateBodySendsExplicitFalse(t *testing.T) {
	cmd := newCatalogCreateFlagCmd()
	_ = cmd.Flags().Set("display-name", "Engineering")
	if err := cmd.Flags().Set("published", "false"); err != nil {
		t.Fatalf("set --published: %v", err)
	}

	got := buildAccessProfileCreateBody(cmd)
	want := map[string]any{"displayName": "Engineering", "published": false}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

// TestBuildCatalogCreateBodyExplicitEmptyDescription pins that an explicitly
// passed --description "" reaches the body. The help promises every flag you
// pass is sent; an emptiness test here dropped it, and the same shape copied
// into an update command would silently fail to clear a description.
func TestBuildCatalogCreateBodyExplicitEmptyDescription(t *testing.T) {
	cmd := newCatalogCreateFlagCmd()
	_ = cmd.Flags().Set("display-name", "Engineering")
	_ = cmd.Flags().Set("description", "")

	got := buildAccessProfileCreateBody(cmd)
	want := map[string]any{"displayName": "Engineering", "description": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}
