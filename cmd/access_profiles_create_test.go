package cmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newCatalogCreateFlagCmd() *cobra.Command {
	cmd := &cobra.Command{}
	f := cmd.Flags()
	f.String("display-name", "", "")
	f.String("description", "", "")
	f.String("type", "", "")
	f.Bool("published", false, "")
	f.Bool("visible-to-everyone", false, "")
	f.Bool("request-bundle", false, "")
	f.String("enrollment-behavior", "", "")
	f.String("unenrollment-behavior", "", "")
	f.String("unenrollment-entitlement-behavior", "", "")
	return cmd
}

// TestBuildCatalogCreateBodyMinimal pins that only displayName is sent when
// nothing else is supplied — no empty description and no defaulted booleans
// leaking into the request, which would silently overwrite the server's own
// defaults for a caller who never asked.
func TestBuildCatalogCreateBodyMinimal(t *testing.T) {
	cmd := newCatalogCreateFlagCmd()
	_ = cmd.Flags().Set("display-name", "Engineering")

	got, err := buildAccessProfileCreateBody(cmd)
	if err != nil {
		t.Fatalf("buildAccessProfileCreateBody: %v", err)
	}
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

	got, err := buildAccessProfileCreateBody(cmd)
	if err != nil {
		t.Fatalf("buildAccessProfileCreateBody: %v", err)
	}
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

	got, err := buildAccessProfileCreateBody(cmd)
	if err != nil {
		t.Fatalf("buildAccessProfileCreateBody: %v", err)
	}
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

	got, err := buildAccessProfileCreateBody(cmd)
	if err != nil {
		t.Fatalf("buildAccessProfileCreateBody: %v", err)
	}
	want := map[string]any{"displayName": "Engineering", "description": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}

// TestBuildCatalogCreateBodyTypeBirthright pins that --type birthright maps to
// REQUEST_CATALOG_TYPE_BUNDLE in the body, and "role" and "birthright" both
// reach the same API value.
func TestBuildCatalogCreateBodyTypeBirthright(t *testing.T) {
	for _, input := range []string{"birthright", "role", "REQUEST_CATALOG_TYPE_BUNDLE"} {
		t.Run(input, func(t *testing.T) {
			cmd := newCatalogCreateFlagCmd()
			_ = cmd.Flags().Set("display-name", "Eng")
			if err := cmd.Flags().Set("type", input); err != nil {
				t.Fatalf("set --type %q: %v", input, err)
			}
			got, err := buildAccessProfileCreateBody(cmd)
			if err != nil {
				t.Fatalf("buildAccessProfileCreateBody: %v", err)
			}
			if got["type"] != "REQUEST_CATALOG_TYPE_BUNDLE" {
				t.Errorf("type = %v, want REQUEST_CATALOG_TYPE_BUNDLE", got["type"])
			}
		})
	}
}

// TestBuildCatalogCreateBodyTypeCatalogAndHybrid pins that --type catalog and
// --type hybrid map to their respective API enum values.
func TestBuildCatalogCreateBodyTypeCatalogAndHybrid(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"catalog", "REQUEST_CATALOG_TYPE_CATALOG"},
		{"hybrid", "REQUEST_CATALOG_TYPE_CATALOG_AND_BUNDLE"},
		{"REQUEST_CATALOG_TYPE_CATALOG", "REQUEST_CATALOG_TYPE_CATALOG"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			cmd := newCatalogCreateFlagCmd()
			_ = cmd.Flags().Set("display-name", "Eng")
			if err := cmd.Flags().Set("type", tc.input); err != nil {
				t.Fatalf("set --type %q: %v", tc.input, err)
			}
			got, err := buildAccessProfileCreateBody(cmd)
			if err != nil {
				t.Fatalf("buildAccessProfileCreateBody: %v", err)
			}
			if got["type"] != tc.want {
				t.Errorf("type = %v, want %v", got["type"], tc.want)
			}
		})
	}
}

// TestBuildCatalogCreateBodyBirthrightRejectsPublished pins that --type
// birthright combined with --published is rejected client-side (exit 2 path),
// quoting the server's own error so the message is greppable.
func TestBuildCatalogCreateBodyBirthrightRejectsPublished(t *testing.T) {
	cmd := newCatalogCreateFlagCmd()
	_ = cmd.Flags().Set("display-name", "Eng")
	_ = cmd.Flags().Set("type", "birthright")
	_ = cmd.Flags().Set("published", "true")

	_, err := buildAccessProfileCreateBody(cmd)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "--published") {
		t.Errorf("error = %q, want it to mention --published", err)
	}
	if !strings.Contains(err.Error(), "Birthright") {
		t.Errorf("error = %q, want it to quote the server error (Birthright)", err)
	}
}

// TestBuildCatalogCreateBodyBirthrightRejectsEnrollmentFlags pins that --type
// birthright combined with any enrollment/unenrollment behavior flag is
// rejected client-side.
func TestBuildCatalogCreateBodyBirthrightRejectsEnrollmentFlags(t *testing.T) {
	for _, flag := range []string{"enrollment-behavior", "unenrollment-behavior", "unenrollment-entitlement-behavior"} {
		t.Run(flag, func(t *testing.T) {
			cmd := newCatalogCreateFlagCmd()
			_ = cmd.Flags().Set("display-name", "Eng")
			_ = cmd.Flags().Set("type", "birthright")
			if err := cmd.Flags().Set(flag, "bypass"); err != nil {
				t.Fatalf("set --%s: %v", flag, err)
			}
			_, err := buildAccessProfileCreateBody(cmd)
			if err == nil {
				t.Fatalf("--%s + --type birthright: expected error, got nil", flag)
			}
			if !strings.Contains(err.Error(), "--"+flag) {
				t.Errorf("error = %q, want it to mention --%s", err, flag)
			}
		})
	}
}

// TestBuildCatalogCreateBodyNonBirthrightAllowsPublished confirms that
// --published is accepted when --type is not birthright (regression guard:
// the validation must be scoped to birthright only).
func TestBuildCatalogCreateBodyNonBirthrightAllowsPublished(t *testing.T) {
	cmd := newCatalogCreateFlagCmd()
	_ = cmd.Flags().Set("display-name", "Eng")
	_ = cmd.Flags().Set("type", "catalog")
	_ = cmd.Flags().Set("published", "true")

	got, err := buildAccessProfileCreateBody(cmd)
	if err != nil {
		t.Fatalf("buildAccessProfileCreateBody: %v", err)
	}
	if got["published"] != true {
		t.Errorf("published = %v, want true", got["published"])
	}
}
