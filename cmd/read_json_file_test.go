package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestReadJSONFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.json")
	for _, tc := range []struct {
		name, body, path string
		array            bool
		want             string // "" means success
	}{
		{name: "array ok", body: `[1]`, array: true},
		{name: "empty array ok", body: `[]`, array: true},
		{name: "object ok", body: `{"a":1}`},
		{name: "empty object ok", body: `{}`},
		{name: "array null", body: `null`, array: true, want: "--x-file must contain a JSON array, not null"},
		{name: "object null", body: `null`, want: "--x-file must contain a JSON object, not null"},
		{name: "array given object", body: `{}`, array: true, want: "parsing --x-file as JSON array:"},
		{name: "object given array", body: `[]`, want: "parsing --x-file as JSON object:"},
		{name: "array malformed", body: `[nope`, array: true, want: "parsing --x-file as JSON array:"},
		{name: "object missing file", path: missing, want: "reading --x-file:"},
		{name: "array missing file", path: missing, array: true, want: "reading --x-file:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.path
			if path == "" {
				path = writeTempJSON(t, "in.json", tc.body)
			}
			var err error
			if tc.array {
				var v []any
				v, err = readJSONFile[[]any](&cobra.Command{}, "x-file", path)
				if err == nil && v == nil {
					t.Error("got nil slice with no error")
				}
			} else {
				var v map[string]any
				v, err = readJSONFile[map[string]any](&cobra.Command{}, "x-file", path)
				if err == nil && v == nil {
					t.Error("got nil map with no error")
				}
			}
			if tc.want == "" {
				if err != nil {
					t.Errorf("err = %v, want nil", err)
				}
				return
			}
			if err == nil || exitCode(err) != exitUsage || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v (exit %d), want exit %d containing %q", err, exitCode(err), exitUsage, tc.want)
			}
		})
	}
}
