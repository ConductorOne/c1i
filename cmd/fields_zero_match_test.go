package cmd

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// This file covers --fields on a LIST command (or `api
// --paginate`) used to behave inconsistently with the single-object `get`
// case — a spec that matched nothing in the entire result silently printed
// "{}" per row and exited 0, instead of erroring like the single-object path
// already did (see TestWriteObjectFailsLoudlyOnNoMatch /
// TestWriteObjectPartialMatchStillSucceeds in cmd/fields_test.go, which pin
// that existing single-object rule). Confirmed live, pre-fix, against
// a live tenant: `users list --fields totally.bogus.path`
// printed two "{}" rows and exited 0 on unfixed main.
//
// The fix is: a row whose projection is empty ("{}") carries no information,
// so the emitter skips writing it — still streamed one row at a time, never
// buffered (--paginate commands fetch potentially-unbounded results), just
// not written when there's nothing in it. The zero-match verdict is still
// judged once, after the command finishes successfully, via rootCmd's
// PersistentPostRunE (checkFieldsMatchedAnyRow in cmd/fields.go): error only
// when every row emitted for the entire invocation missed every requested
// path; a genuinely empty result (zero rows) is left alone, since there's
// nothing to judge a match against.
//
// newTestListTree below builds a minimal two-command tree (not the real
// rootCmd) that reproduces just the two hooks under test —
// PersistentPreRunE attaching a *fieldsMatchState and PersistentPostRunE
// running checkFieldsMatchedAnyRow — so these tests can drive arbitrary rows
// and RunE outcomes without touching auth, the network, or the real command
// tree's flags. The `api --paginate` end-to-end variants in cmd/api_test.go
// exercise the identical hooks through the real rootCmd instead, to prove
// the wiring holds all the way from Execute().

// newTestListTree returns a cobra tree whose "list" leaf emits rows (via the
// real newEmitter/Encode) and then returns rowErr. Its root reproduces
// rootCmd's PersistentPreRunE/PersistentPostRunE pairing exactly, so a test
// driving it exercises the real checkFieldsMatchedAnyRow logic without
// touching the global rootCmd singleton other tests in this package share.
func newTestListTree(rows []any, rowErr error) (*cobra.Command, *bytes.Buffer) {
	var buf bytes.Buffer
	root := &cobra.Command{
		Use: "root", SilenceUsage: true, SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SetContext(withFieldsMatchState(cmd.Context()))
			return nil
		},
		PersistentPostRunE: checkFieldsMatchedAnyRow,
	}
	leaf := &cobra.Command{
		Use: "list",
		RunE: func(cmd *cobra.Command, _ []string) error {
			enc := newEmitter(cmd)
			for _, r := range rows {
				if err := enc.Encode(r); err != nil {
					return err
				}
			}
			return rowErr
		},
	}
	root.AddCommand(leaf)
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"list"})
	return root, &buf
}

// TestFieldsListSparseDataDoesNotError is the MANDATORY inverse case, and is
// deliberately the first test in this file: sparse data (a requested field
// present on some rows, absent on others) must NOT error. Row 1 has no "id"
// (projects to "{}"); row 2 does. A fix that errors here — because it checks
// per row, or because it only looks at the first row — is worse than the bug
// it's supposed to fix, since --fields/C1I_FIELDS is a session-wide spec
// routinely applied across responses that don't all share every field.
//
// A row that projects to "{}" carries no information, so it must be SKIPPED
// rather than written: only row 2's projection reaches stdout.
func TestFieldsListSparseDataDoesNotError(t *testing.T) {
	viper.Set("fields", "id")
	t.Cleanup(func() { viper.Set("fields", "") })

	root, buf := newTestListTree([]any{
		map[string]any{"name": "no id on this row"}, // projects to {}, skipped
		map[string]any{"id": "1"},                   // projects to {"id":"1"}
	}, nil)

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("sparse data (field present on row 2, absent on row 1) must not error, got: %v (output: %s)", err, buf.String())
	}
	got := strings.Split(strings.TrimSpace(buf.String()), "\n")
	want := []string{`{"id":"1"}`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("output lines = %v, want %v (row 1's empty projection must be skipped, not written as {})", got, want)
	}
}

// TestFieldsListZeroRowsDoesNotError: a genuinely empty result (no rows
// emitted at all, e.g. a search that matched nothing) must not error either
// — there is nothing to judge a --fields match against.
func TestFieldsListZeroRowsDoesNotError(t *testing.T) {
	viper.Set("fields", "id")
	t.Cleanup(func() { viper.Set("fields", "") })

	root, buf := newTestListTree(nil, nil)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("zero emitted rows must not error, got: %v (output: %s)", err, buf.String())
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output for zero rows, got %q", buf.String())
	}
}

// TestFieldsListAllRowsMissErrors is the flip side of the sparse-data case:
// when --fields matches nothing in ANY row across the whole result, every
// row's projection is empty and therefore skipped — stdout must be
// completely empty — and the command must THEN fail with a *usageError
// (exit 2), using a message distinguishable from the single-object case
// ("...matched no keys in the response"). This is the core regression this
// fix targets: a consumer piping stdout without checking the exit code must
// never see N lines of "{}" and mistake them for real (if empty) rows.
func TestFieldsListAllRowsMissErrors(t *testing.T) {
	viper.Set("fields", "totally.bogus.path")
	t.Cleanup(func() { viper.Set("fields", "") })

	root, buf := newTestListTree([]any{
		map[string]any{"id": "1"},
		map[string]any{"id": "2"},
	}, nil)

	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("expected an error when --fields matches nothing in any row; output: %s", buf.String())
	}
	var usageErr *usageError
	if !errors.As(err, &usageErr) {
		t.Fatalf("error = %v (%T), want *usageError (exit code 2)", err, err)
	}
	if !strings.Contains(err.Error(), "matched no keys in any row of the response") {
		t.Errorf("error = %q, want the list-specific message (distinguishable from the single-object %q)", err.Error(), "matched no keys in the response")
	}
	if buf.Len() != 0 {
		t.Errorf("output = %q, want completely empty stdout (every row's projection was empty and must be skipped, not written as {})", buf.String())
	}
}

// TestFieldsListPreExistingErrorNotMasked proves the new check can never
// mask an already-failing command: cobra only invokes PersistentPostRunE
// after RunE has returned nil, so a RunE error (e.g. a wrapped API failure)
// must come back completely unchanged, verbatim, even though every row
// emitted before the failure also missed --fields.
func TestFieldsListPreExistingErrorNotMasked(t *testing.T) {
	viper.Set("fields", "totally.bogus.path")
	t.Cleanup(func() { viper.Set("fields", "") })

	wantErr := errors.New("boom: upstream API failure")
	root, buf := newTestListTree([]any{map[string]any{"id": "1"}}, wantErr)

	err := root.ExecuteContext(context.Background())
	if err != wantErr {
		t.Fatalf("error = %v, want the original RunE error %v returned completely unchanged (output: %s)", err, wantErr, buf.String())
	}
}

// TestFieldsListNoProjectionNeverErrors: without --fields set at all, the
// zero-match check must never fire, no matter what rows a command emits.
func TestFieldsListNoProjectionNeverErrors(t *testing.T) {
	viper.Set("fields", "")
	root, buf := newTestListTree([]any{
		map[string]any{"anything": "goes"},
	}, nil)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("no --fields set: expected no error, got %v (output: %s)", err, buf.String())
	}
}
