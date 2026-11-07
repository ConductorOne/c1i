package cmd

import (
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// TestDurationFlagsNameProtobufFormat: the API parses every grant duration as
// a google.protobuf.Duration and 400s a Go-style "24h", so a duration flag's
// help must say which format it takes.
func TestDurationFlagsNameProtobufFormat(t *testing.T) {
	secondsExample := regexp.MustCompile(`\b\d+s\b`)
	seen := 0
	walkCommandTree(func(c *cobra.Command) {
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if !strings.Contains(f.Name, "duration") {
				return
			}
			seen++
			if !strings.Contains(f.Usage, "protobuf duration") || !secondsExample.MatchString(f.Usage) {
				t.Errorf("%s --%s: help %q does not say it takes a protobuf duration with a seconds example", c.CommandPath(), f.Name, f.Usage)
			}
		})
	})
	if seen == 0 {
		t.Fatal("found no duration flags; the walk is broken")
	}
}

func TestProtoDurationFlag(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		allowZero bool
		want      string
		wantErr   string
	}{
		{name: "go hours", in: "8h", want: "28800s"},
		{name: "protobuf seconds", in: "3600s", want: "3600s"},
		{name: "fraction trimmed", in: "1m30.5s", want: "90.5s"},
		{name: "fraction kept to the nanosecond", in: "1.000000001s", want: "1.000000001s"},
		{name: "one nanosecond", in: "1ns", want: "0.000000001s"},
		{name: "max int64", in: "2562047h47m16.854775807s", want: "9223372036.854775807s"},
		{name: "whitespace trimmed", in: " \t8h\n", want: "28800s"},
		{name: "zero allowed", in: "0s", allowZero: true, want: "0s"},
		{name: "zero refused", in: "0s", wantErr: "--d must be positive"},
		{name: "bare zero refused", in: "0", wantErr: "--d must be positive"},
		{name: "negative refused", in: "-1m", wantErr: "--d must be positive"},
		{name: "negative refused with zero allowed", in: "-1ns", allowZero: true, wantErr: "--d must be zero or positive"},
		{name: "empty refused", in: "", wantErr: "invalid --d"},
		{name: "whitespace only refused", in: "  ", wantErr: "invalid --d"},
		{name: "days refused", in: "7d", wantErr: "invalid --d"},
		{name: "junk refused", in: "soon", wantErr: "invalid --d"},
		{name: "overflow refused", in: "2562048h", wantErr: "invalid --d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "x"}
			cmd.Flags().String("d", "", "")
			if err := cmd.Flags().Set("d", tc.in); err != nil {
				t.Fatal(err)
			}
			got, set, err := protoDurationFlag(cmd, "d", tc.allowZero)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
				}
				if code := exitCode(err); code != exitUsage {
					t.Errorf("exit = %d, want %d", code, exitUsage)
				}
				if set || got != "" {
					t.Errorf("got (%q, %v) alongside an error", got, set)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got != tc.want || !set {
				t.Errorf("got (%q, %v), want (%q, true)", got, set, tc.want)
			}
		})
	}
}

// An unpassed flag is not a value, whatever its default.
func TestProtoDurationFlagUnset(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	cmd.Flags().String("d", "junk", "")
	got, set, err := protoDurationFlag(cmd, "d", false)
	if err != nil || set || got != "" {
		t.Errorf("got (%q, %v, %v), want (\"\", false, nil)", got, set, err)
	}
}
