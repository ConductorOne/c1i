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
