package cmd

import (
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// TestDurationFlagsNameProtobufFormat: every flag the API parses as a
// google.protobuf.Duration goes through addProtoDurationFlag, and its help
// names both accepted forms. A string flag named like a duration must be one.
func TestDurationFlagsNameProtobufFormat(t *testing.T) {
	secondsExample := regexp.MustCompile(`\b\d+s\b`)
	seen := 0
	walkCommandTree(func(c *cobra.Command) {
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			registered := f.Annotations[protoDurationAnnotation] != nil
			if !registered {
				if strings.Contains(f.Name, "duration") && f.Value.Type() == "string" {
					t.Errorf("%s --%s: register it with addProtoDurationFlag", c.CommandPath(), f.Name)
				}
				return
			}
			seen++
			if !strings.Contains(f.Usage, "protobuf duration") || !secondsExample.MatchString(f.Usage) || !strings.Contains(f.Usage, "Go duration") {
				t.Errorf("%s --%s: help %q does not name both a protobuf duration with a seconds example and a Go duration", c.CommandPath(), f.Name, f.Usage)
			}
		})
	})
	// entitlements create/update, requests create grant, tasks
	// update-grant-duration, credentials create, webhooks create/update.
	if seen < 7 {
		t.Fatalf("found %d proto duration flags, want at least 7; the walk is broken", seen)
	}
}

func newProtoDurationTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "x"}
	addProtoDurationFlag(cmd, "d", "")
	return cmd
}

func TestProtoDurationFlag(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr string
	}{
		{name: "go hours", in: "8h", want: "28800s"},
		{name: "protobuf seconds", in: "3600s", want: "3600s"},
		{name: "fraction trimmed", in: "1m30.5s", want: "90.5s"},
		{name: "fraction kept to the nanosecond", in: "1.000000001s", want: "1.000000001s"},
		{name: "trailing zeros past nine digits", in: "1.5000000000s", want: "1.5s"},
		{name: "one nanosecond", in: "1ns", want: "0.000000001s"},
		{name: "fractional unit that is whole nanoseconds", in: "1.5us", want: "0.0000015s"},
		{name: "minute fraction that is whole nanoseconds", in: "0.0000000001m", want: "0.000000006s"},
		{name: "max int64", in: "2562047h47m16.854775807s", want: "9223372036.854775807s"},
		{name: "whitespace trimmed", in: " \t8h\n", want: "28800s"},
		{name: "zero sent", in: "0s", want: "0s"},
		{name: "bare zero sent", in: "0", want: "0s"},
		{name: "negative refused", in: "-1m", wantErr: "--d must not be negative"},
		{name: "negative nanosecond refused", in: "-1ns", wantErr: "--d must not be negative"},
		{name: "ten fractional digits refused", in: "1.0000000001s", wantErr: "finer than a nanosecond"},
		{name: "fractional nanosecond refused", in: "1.5ns", wantErr: "finer than a nanosecond"},
		{name: "sub-nanosecond hour fraction refused", in: "0.0000000000001h", wantErr: "finer than a nanosecond"},
		{name: "empty refused", in: "", wantErr: "invalid --d"},
		{name: "whitespace only refused", in: "  ", wantErr: "invalid --d"},
		{name: "days refused", in: "7d", wantErr: "invalid --d"},
		{name: "junk refused", in: "soon", wantErr: "invalid --d"},
		{name: "overflow refused", in: "2562048h", wantErr: "invalid --d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newProtoDurationTestCmd()
			if err := cmd.Flags().Set("d", tc.in); err != nil {
				t.Fatal(err)
			}
			got, set, err := protoDurationFlag(cmd, "d")
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
	got, set, err := protoDurationFlag(newProtoDurationTestCmd(), "d")
	if err != nil || set || got != "" {
		t.Errorf("got (%q, %v, %v), want (\"\", false, nil)", got, set, err)
	}
}

// Reading a flag the registrar didn't create is a bug, not a silent pass.
func TestProtoDurationFlagRequiresRegistrar(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	cmd.Flags().String("d", "", "")
	_ = cmd.Flags().Set("d", "1h")
	if _, _, err := protoDurationFlag(cmd, "d"); err == nil || !strings.Contains(err.Error(), "addProtoDurationFlag") {
		t.Errorf("err = %v, want a registrar error", err)
	}
}
