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
		{name: "multi-component", in: "1h30m", want: "5400s"},
		{name: "seconds", in: "90s", want: "90s"},
		{name: "protobuf seconds", in: "3600s", want: "3600s"},
		{name: "milliseconds", in: "500ms", want: "0.5s"},
		{name: "micro sign spellings", in: "1µs2μs3us", want: "0.000006s"},
		{name: "one nanosecond", in: "1ns", want: "0.000000001s"},
		{name: "max int64", in: "2562047h47m16s854775807ns", want: "9223372036.854775807s"},
		{name: "whitespace trimmed", in: " \t8h\n", want: "28800s"},
		{name: "plus sign", in: "+1h1ns", want: "3600.000000001s"},
		{name: "negative zero is zero", in: "-0s", want: "0s"},
		{name: "zero sent", in: "0s", want: "0s"},
		{name: "bare zero sent", in: "0", want: "0s"},
		{name: "negative refused", in: "-1m", wantErr: "--d must not be negative"},
		{name: "negative nanosecond refused", in: "-1ns", wantErr: "--d must not be negative"},
		{name: "decimal hours refused", in: "1.5h", wantErr: "--d must be a whole-number duration"},
		{name: "decimal seconds refused", in: "0.5s", wantErr: "--d must be a whole-number duration"},
		{name: "leading point refused", in: ".5h", wantErr: "--d must be a whole-number duration"},
		{name: "trailing point refused", in: "5.h", wantErr: "--d must be a whole-number duration"},
		{name: "decimal protobuf seconds refused", in: "3600.5s", wantErr: "--d must be a whole-number duration"},
		{name: "exponent refused", in: "1e3s", wantErr: "--d must be a whole-number duration"},
		{name: "negative decimal sum refused", in: "-0.5ns0.5ns", wantErr: "--d must be a whole-number duration"},
		{name: "empty refused", in: "", wantErr: "--d must be a whole-number duration"},
		{name: "whitespace only refused", in: "  ", wantErr: "--d must be a whole-number duration"},
		{name: "unitless number refused", in: "5", wantErr: "--d must be a whole-number duration"},
		{name: "days refused", in: "7d", wantErr: "--d must be a whole-number duration"},
		{name: "junk refused", in: "soon", wantErr: "--d must be a whole-number duration"},
		{name: "overflow refused", in: "2562048h", wantErr: "invalid --d"},
		{name: "one past max int64 refused", in: "9223372036854775808ns", wantErr: "invalid --d"},
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
