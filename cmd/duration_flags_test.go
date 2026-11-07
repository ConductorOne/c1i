package cmd

import (
	"math"
	"math/big"
	"math/rand"
	"regexp"
	"strings"
	"testing"
	"time"

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
		// time.ParseDuration lands each of these a nanosecond short.
		{name: "float-misparsed hours 1", in: "70.91758459991000h", want: "255303.304559676s"},
		{name: "float-misparsed hours 2", in: "33.57013783261000h", want: "120852.496197396s"},
		{name: "float-misparsed hours 3", in: "8.47661702844250h", want: "30515.821302393s"},
		{name: "float-misparsed hours 4", in: "50.53582406065500h", want: "181928.966618358s"},
		{name: "exact only in decimal", in: "0.1h", want: "360s"},
		{name: "exact only in decimal, minutes", in: "0.3m", want: "18s"},
		{name: "multi-component fractions", in: "1.5h30.25m0.5s", want: "7215.5s"},
		{name: "plus sign and mixed units", in: "+1h1ns", want: "3600.000000001s"},
		{name: "negative zero is zero", in: "-0s", want: "0s"},
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

// TestProtoDurationFlagMatchesExactDecimal: for random accepted inputs, the
// seconds sent equal the input's exact decimal value, and an input that is
// not a whole number of nanoseconds (or overflows) is refused, never rounded.
func TestProtoDurationFlagMatchesExactDecimal(t *testing.T) {
	units := []struct {
		name  string
		nanos int64
	}{{"ns", 1}, {"us", 1e3}, {"µs", 1e3}, {"ms", 1e6}, {"s", 1e9}, {"m", 60e9}, {"h", 3600e9}}
	digits := func(r *rand.Rand, n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte('0' + r.Intn(10))
		}
		return string(b)
	}
	r := rand.New(rand.NewSource(1))
	checked := 0
	for i := 0; i < 50000; i++ {
		var in strings.Builder
		exact := new(big.Rat)
		for c := 1 + r.Intn(3); c > 0; c-- {
			u := units[r.Intn(len(units))]
			whole, frac := digits(r, 1+r.Intn(3)), digits(r, r.Intn(16))
			if r.Intn(3) == 0 {
				// Every remaining digit a nanosecond can hold, padded with
				// zeros: the shape ParseDuration's float path misreads.
				frac = strings.TrimRight(frac, "0") + strings.Repeat("0", r.Intn(4))
			}
			num := whole
			if frac != "" {
				num += "." + frac
			}
			in.WriteString(num + u.name)
			v, _ := new(big.Rat).SetString(num)
			exact.Add(exact, v.Mul(v, big.NewRat(u.nanos, 1)))
		}
		if _, err := time.ParseDuration(in.String()); err != nil {
			continue // outside the accepted syntax/range; not this test's concern
		}
		cmd := newProtoDurationTestCmd()
		_ = cmd.Flags().Set("d", in.String())
		got, _, err := protoDurationFlag(cmd, "d")
		representable := exact.IsInt() && exact.Cmp(big.NewRat(math.MaxInt64, 1)) <= 0
		if !representable {
			if exitCode(err) != exitUsage {
				t.Fatalf("%q (exact %s ns): got (%q, %v), want a usage error", in.String(), exact.FloatString(3), got, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", in.String(), err)
		}
		sent, ok := new(big.Rat).SetString(strings.TrimSuffix(got, "s"))
		if !ok || sent.Mul(sent, big.NewRat(1e9, 1)).Cmp(exact) != 0 {
			t.Fatalf("%q: sent %q, want %s ns exactly", in.String(), got, exact.FloatString(0))
		}
		checked++
	}
	if checked < 1000 {
		t.Fatalf("only %d inputs checked; the generator is broken", checked)
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
