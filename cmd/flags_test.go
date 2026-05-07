package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestMarkRequiredAnnotatesUsage(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("foo", "", "Foo flag")
	cmd.Flags().String("bar", "", "Bar flag")

	markRequired(cmd, "foo", "bar")

	for _, n := range []string{"foo", "bar"} {
		f := cmd.Flags().Lookup(n)
		if f == nil {
			t.Fatalf("flag %s missing", n)
		}
		if !strings.Contains(f.Usage, "(required)") {
			t.Errorf("flag --%s usage should contain (required), got %q", n, f.Usage)
		}
	}
}

func TestMarkRequiredIdempotent(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("foo", "", "Foo flag")

	markRequired(cmd, "foo")
	markRequired(cmd, "foo")

	got := cmd.Flags().Lookup("foo").Usage
	if strings.Count(got, "(required)") != 1 {
		t.Errorf("expected single (required) marker, got %q", got)
	}
}

func TestLimitReached(t *testing.T) {
	cases := []struct {
		emitted int
		limit   int
		want    bool
	}{
		{0, 0, false},   // unlimited, no rows yet
		{100, 0, false}, // unlimited, plenty of rows
		{4, 5, false},   // under cap
		{5, 5, true},    // at cap
		{6, 5, true},    // over cap (defensive)
		{10, -1, false}, // negative limit treated as unlimited
		{0, 1, false},   // limit set, no rows yet
		{1, 1, true},    // single-item limit hit on first emit
	}
	for _, tc := range cases {
		if got := limitReached(tc.emitted, tc.limit); got != tc.want {
			t.Errorf("limitReached(emitted=%d, limit=%d) = %v, want %v",
				tc.emitted, tc.limit, got, tc.want)
		}
	}
}

func TestEffectivePageSize(t *testing.T) {
	cases := []struct {
		name      string
		requested int
		limit     int
		emitted   int
		want      int
	}{
		{"unlimited returns requested as-is", 50, 0, 0, 50},
		{"limit unset (negative) returns requested", 50, -5, 10, 50},
		{"limit larger than requested returns requested", 50, 200, 0, 50},
		{"limit equal to requested on first page returns requested", 50, 50, 0, 50},
		{"limit smaller than requested tightens", 50, 3, 0, 3},
		{"limit smaller across pages tightens to remaining", 50, 75, 50, 25},
		{"emitted at limit returns 1 (defensive — outer loop should stop first)", 50, 5, 5, 1},
		{"emitted past limit returns 1 (defensive)", 50, 5, 7, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := effectivePageSize(tc.requested, tc.limit, tc.emitted)
			if got != tc.want {
				t.Errorf("effectivePageSize(req=%d, lim=%d, em=%d) = %d, want %d",
					tc.requested, tc.limit, tc.emitted, got, tc.want)
			}
		})
	}
}

func TestRequireNonEmpty(t *testing.T) {
	mkCmd := func() *cobra.Command {
		cmd := &cobra.Command{Use: "test"}
		cmd.Flags().String("a", "", "")
		cmd.Flags().String("b", "", "")
		cmd.Flags().String("c", "", "")
		return cmd
	}

	t.Run("all set", func(t *testing.T) {
		cmd := mkCmd()
		_ = cmd.Flags().Set("a", "x")
		_ = cmd.Flags().Set("b", "y")
		if err := requireNonEmpty(cmd, "a", "b"); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("single missing", func(t *testing.T) {
		cmd := mkCmd()
		_ = cmd.Flags().Set("a", "x")
		err := requireNonEmpty(cmd, "a", "b")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "--b") {
			t.Errorf("expected error to mention --b, got %v", err)
		}
	})

	t.Run("multiple missing", func(t *testing.T) {
		cmd := mkCmd()
		err := requireNonEmpty(cmd, "a", "b", "c")
		if err == nil {
			t.Fatal("expected error")
		}
		for _, want := range []string{"--a", "--b", "--c"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("expected error to mention %s, got %v", want, err)
			}
		}
	})
}
