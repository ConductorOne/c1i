package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var errStdoutFull = errors.New("no space left on device")

// failingWriter stands in for stdout on a full disk.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errStdoutFull }

func assertOutputWriteFailure(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Error("RunE returned nil although every write to stdout failed")
		return
	}
	if !errors.Is(err, errStdoutFull) {
		t.Errorf("error %v does not wrap the write error", err)
	}
	if got := exitCode(err); got != exitError {
		t.Errorf("exit code %d, want %d", got, exitError)
	}
}

// Every auto-paginating list (typed rows, listJSONPages-backed, export events)
// must fail on the first failed write and not fetch another page.
func TestListCommandsStopOnOutputWriteError(t *testing.T) {
	for _, tc := range listPaginationCases() {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				n := requests.Add(1)
				next := ""
				if n < 3 {
					next = fmt.Sprintf("tok%d", n)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, tc.page([]string{fmt.Sprintf("item%d", n)}, next))
			}))
			defer srv.Close()
			t.Cleanup(func() { tc.cmd.SetOut(nil) })

			_, err := runListPaginationCaseTo(t, tc, srv, failingWriter{})
			assertOutputWriteFailure(t, err)
			if got := requests.Load(); got != 1 {
				t.Errorf("server received %d requests, want 1: paging continued after a failed write", got)
			}
		})
	}
}

// Single-object reads go through writeObject; cover both its projected and
// raw branches.
func TestGetCommandsFailOnOutputWriteError(t *testing.T) {
	for _, fields := range []string{"", "id"} {
		for _, c := range getUnwrapCases() {
			t.Run(fmt.Sprintf("%s/fields=%q", c.name, fields), func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, c.body)
				}))
				defer srv.Close()

				resetCmdFlags(t, c.cmd)
				stubGetClient(t, srv)
				t.Setenv("C1I_URL", "https://example.invalid")
				viper.Set("fields", fields)
				t.Cleanup(func() { viper.Set("fields", "") })
				for name, val := range c.flags {
					if err := c.cmd.Flags().Set(name, val); err != nil {
						t.Fatalf("set --%s: %v", name, err)
					}
				}
				c.cmd.SetOut(failingWriter{})
				t.Cleanup(func() { c.cmd.SetOut(nil) })
				c.cmd.SetContext(context.Background())

				err := c.cmd.RunE(c.cmd, c.args)
				var ue *usageError
				if fields != "" && errors.As(err, &ue) {
					t.Skipf("--fields %s does not apply to this response", fields)
				}
				assertOutputWriteFailure(t, err)
			})
		}
	}
}

func TestWriteRawObjectFailsOnOutputWriteError(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetOut(failingWriter{})
	assertOutputWriteFailure(t, writeRawObject(cmd, []byte(`{"deleted":true}`)))
}

// --limit compares against Written, so a failed row must not count.
func TestEmitterCountsOnlyWrittenRows(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetOut(failingWriter{})
	cmd.SetContext(context.Background())
	enc := newEmitter(cmd)
	if err := enc.Encode(map[string]string{"id": "x"}); !errors.Is(err, errStdoutFull) {
		t.Fatalf("Encode error = %v, want it to wrap the write error", err)
	}
	if got := enc.Written(); got != 0 {
		t.Errorf("Written() = %d after a failed write, want 0", got)
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	enc = newEmitter(cmd)
	if err := enc.Encode(map[string]string{"id": "x"}); err != nil {
		t.Fatal(err)
	}
	if got := enc.Written(); got != 1 {
		t.Errorf("Written() = %d after a successful write, want 1", got)
	}
}

// TestNoDiscardedOutputWriteErrors fails on any `_ = x.Encode(...)`,
// `_, _ = x.Write(...)`, or bare `x.Encode(...)` statement in cmd/ — a
// discarded write error turns a truncated output into exit 0. fmt.Fprint*
// is not covered; stderr notices and prompts legitimately ignore it.
func TestNoDiscardedOutputWriteErrors(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("globbing cmd/*.go: %v (found %d)", err, len(paths))
	}
	isWriteCall := func(e ast.Expr) (string, bool) {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return "", false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Encode" && sel.Sel.Name != "Write") {
			return "", false
		}
		return sel.Sel.Name, true
	}
	var scanned int
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		scanned++
		ast.Inspect(file, func(n ast.Node) bool {
			switch s := n.(type) {
			case *ast.AssignStmt:
				if len(s.Rhs) != 1 {
					return true
				}
				name, ok := isWriteCall(s.Rhs[0])
				if !ok {
					return true
				}
				for _, l := range s.Lhs {
					if id, ok := l.(*ast.Ident); !ok || id.Name != "_" {
						return true
					}
				}
				t.Errorf("%s: discards the error from %s; return it so a failed write can't exit 0", fset.Position(s.Pos()), name)
			case *ast.ExprStmt:
				// Bare Write is left to errcheck, which knows a bytes.Buffer can't fail.
				if name, ok := isWriteCall(s.X); ok && name == "Encode" {
					t.Errorf("%s: ignores the error from %s; return it so a failed write can't exit 0", fset.Position(s.Pos()), name)
				}
			}
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("scanned no non-test files in cmd/")
	}
}
