package cmd

import (
	"go/ast"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// The enforcement tests must fail on each violation they exist to catch.
// Each case injects a synthetic file and asserts the matching check fires.

const synthViolations = `package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/keychain"
	"github.com/spf13/cobra"
)

func zzRevertedDelete(cmd *cobra.Command, args []string) error {
	if err := zzSend(cmd); err != nil {
		return err
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Deleted app: id=%s\n", args[0])
	return err
}

func zzSend(cmd *cobra.Command) error { return sendMutation(cmd, mutation{}) }

func zzNonTail(cmd *cobra.Command, _ []string) error {
	err := sendMutation(cmd, mutation{})
	return err
}

func zzUnwrappedResolve(cmd *cobra.Command, _ []string) error {
	if _, err := resolvePolicyStepID(cmd.Context(), nil, "t", ""); err != nil {
		return err
	}
	return sendMutation(cmd, mutation{})
}

func zzBatch(cmd *cobra.Command, _ []string) error {
	return sendMutationBatch(cmd, nil, "id", nil)
}

func zzRawPost(cmd *cobra.Command, c interface{ Post() }) {
	c.Post()
}

func zzMethodValue(c interface{ Post() }) {
	f := c.Post
	f()
}

func zzStderr() {
	fmt.Fprintf(os.Stderr, "oops\n")
}

func zzStderrMethod(cmd *cobra.Command) {
	_, _ = cmd.ErrOrStderr().Write([]byte("oops\n"))
}

func zzWriteMethod(cmd *cobra.Command, _ []string) error {
	if _, err := cmd.OutOrStdout().Write([]byte("x")); err != nil {
		return err
	}
	return sendMutation(cmd, mutation{})
}

func zzOsWriteString(cmd *cobra.Command, _ []string) error {
	if _, err := os.Stdout.WriteString("x"); err != nil {
		return err
	}
	return sendMutation(cmd, mutation{})
}

func zzWrapped(cmd *cobra.Command, _ []string) error {
	var out io.Writer = cmd.OutOrStdout()
	b := bufio.NewWriter(out)
	_ = b.Flush()
	return sendMutation(cmd, mutation{})
}

func zzCopy(cmd *cobra.Command, _ []string) error {
	if _, err := io.Copy(os.Stdout, strings.NewReader("x")); err != nil {
		return err
	}
	return sendMutation(cmd, mutation{})
}

func zzEmit(w io.Writer) error {
	_, err := w.Write([]byte("x"))
	return err
}

func zzParamWrite(cmd *cobra.Command, _ []string) error {
	if err := zzEmit(cmd.OutOrStdout()); err != nil {
		return err
	}
	return sendMutation(cmd, mutation{})
}

func zzTemplate(cmd *cobra.Command) error {
	_, err := cmd.OutOrStdout().Write([]byte("{}"))
	return err
}

type zzGetter interface {
	Get(ctx any, path string, q any) ([]byte, error)
}

func zzEagerGetBuilder(cmd *cobra.Command, c zzGetter) preRead {
	_, _ = c.Get(cmd.Context(), "/x", nil)
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		return nil, nil
	}}
}

func zzEagerGet(cmd *cobra.Command, _ []string) error {
	return sendMutation(cmd, mutation{}, withPreRead(zzEagerGetBuilder(cmd, nil)))
}

func zzEagerWriteBuilder(cmd *cobra.Command) preRead {
	_, _ = cmd.OutOrStdout().Write([]byte("x"))
	return preRead{}
}

func zzEagerWrite(cmd *cobra.Command, _ []string) error {
	return sendMutation(cmd, mutation{}, withPreRead(zzEagerWriteBuilder(cmd)))
}

func zzLazyGetBuilder(cmd *cobra.Command) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		_, err := c.Get(ctx, "/x", nil)
		return nil, err
	}}
}

func zzLazyGet(cmd *cobra.Command, _ []string) error {
	return sendMutation(cmd, mutation{}, withPreRead(zzLazyGetBuilder(cmd)))
}

func zzPreReadStdout(cmd *cobra.Command, _ []string) error {
	return sendMutation(cmd, mutation{}, withPreRead(preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		fmt.Fprintln(cmd.OutOrStdout(), "resolved")
		return nil, nil
	}}))
}

func zzPreReadInput(cmd *cobra.Command, _ []string) error {
	return sendMutation(cmd, mutation{}, withPreRead(preRead{Run: func(ctx context.Context, c *client.Client, m *mutation) ([]resolvedID, error) {
		body, err := readJSONFile[map[string]any](cmd, "body-file", "-")
		m.Body = body
		return nil, err
	}}))
}

type zzOut struct{ zzW io.Writer }

func (o zzOut) zzSay() { _, _ = o.zzW.Write([]byte("x")) }

func zzFieldWriter(cmd *cobra.Command, _ []string) error {
	o := zzOut{}
	o.zzW = cmd.OutOrStdout()
	o.zzSay()
	return sendMutation(cmd, mutation{})
}

func zzHTTPPost() {
	_, _ = http.Post("https://example.invalid", "application/json", nil)
}

func zzDefaultDo(req *http.Request) {
	_, _ = http.DefaultClient.Do(req)
}

func zzNewClientMethod(c interface{ PostJSON() }) {
	c.PostJSON()
}

func zzScoped() {
	_, _ = keychain.Delete("svc")
}

func zzScopedOther(c interface{ Post() }) {
	_, _ = keychain.Delete("svc")
	c.Post()
}

func zzTemplateMode(cmd *cobra.Command, _ []string) error {
	if cmd.Flags().Changed("template") {
		return zzTemplate(cmd)
	}
	return sendMutation(cmd, mutation{})
}
`

func synthIndex(t *testing.T) *srcIndex {
	t.Helper()
	return loadSrcIndex(t, map[string]string{
		"cmd/zz_synthetic.go":          synthViolations,
		"internal/client/zz_client.go": "package client\n\nfunc (c *Client) PostJSON() {}\n",
	})
}

func synthLeaf(t *testing.T, ix *srcIndex, fn string, marked bool) (leafCmd, *closureReport) {
	t.Helper()
	var root ast.Node
	for _, d := range ix.files["cmd/zz_synthetic.go"].Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == fn {
			root = fd
		}
	}
	if root == nil {
		t.Fatalf("no synthetic %s", fn)
	}
	c := &cobra.Command{Use: "zz"}
	if marked {
		markMutating(c)
	}
	l := leafCmd{path: "c1i zz " + fn, cmd: c, root: root}
	return l, ix.closureOf(root)
}

func wantViolation(t *testing.T, got []string, substr string) {
	t.Helper()
	for _, g := range got {
		if strings.Contains(g, substr) {
			return
		}
	}
	t.Errorf("no violation containing %q in %q", substr, got)
}

func TestEnforcementCatchesViolations(t *testing.T) {
	ix := synthIndex(t)

	t.Run("reverted text confirmation", func(t *testing.T) {
		l, rep := synthLeaf(t, ix, "zzRevertedDelete", true)
		wantViolation(t, leafViolations(l, rep), "fmt.Fprintf to stdout")
	})
	t.Run("non-tail sendMutation", func(t *testing.T) {
		l, rep := synthLeaf(t, ix, "zzNonTail", true)
		wantViolation(t, leafViolations(l, rep), "not a tail call")
	})
	t.Run("resolvePolicyStepID outside withPreRead", func(t *testing.T) {
		l, rep := synthLeaf(t, ix, "zzUnwrappedResolve", true)
		wantViolation(t, leafViolations(l, rep), "outside withPreRead/withWait")
	})
	t.Run("unregistered batch caller", func(t *testing.T) {
		l, rep := synthLeaf(t, ix, "zzBatch", true)
		wantViolation(t, leafViolations(l, rep), "not in batchCommands")
	})
	t.Run("mutating command without markMutating", func(t *testing.T) {
		l, rep := synthLeaf(t, ix, "zzSend", false)
		wantViolation(t, leafViolations(l, rep), "does not call markMutating")
	})
	t.Run("c.Post outside the helper", func(t *testing.T) {
		wantViolation(t, ruleAViolations(ix), "zz_synthetic.go:zzRawPost")
	})
	t.Run("method value outside the helper", func(t *testing.T) {
		wantViolation(t, ruleAViolations(ix), "zz_synthetic.go:zzMethodValue")
	})
	t.Run("raw stderr write", func(t *testing.T) {
		wantViolation(t, stderrViolations(ix), "zz_synthetic.go:zzStderr;")
	})
	t.Run("stderr Write method", func(t *testing.T) {
		wantViolation(t, stderrViolations(ix), "zz_synthetic.go:zzStderrMethod;")
	})
	for fn, what := range map[string]string{
		"zzWriteMethod":   ".Write to stdout",
		"zzOsWriteString": ".WriteString to stdout",
		"zzWrapped":       "bufio.NewWriter to stdout",
		"zzCopy":          "io.Copy to stdout",
		"zzParamWrite":    "zzEmit writes stdout through its writer parameter",
	} {
		t.Run("stdout bypass "+fn, func(t *testing.T) {
			l, rep := synthLeaf(t, ix, fn, true)
			wantViolation(t, leafViolations(l, rep), what)
		})
	}
	t.Run("unexempted output mode", func(t *testing.T) {
		l, rep := synthLeaf(t, ix, "zzTemplateMode", true)
		wantViolation(t, leafViolations(l, rep), ".Write to stdout")
	})
	t.Run("exempted output mode", func(t *testing.T) {
		nonMutatingOutputModes["cmd/zz_synthetic.go:zzTemplate"] = "test"
		defer delete(nonMutatingOutputModes, "cmd/zz_synthetic.go:zzTemplate")
		l, rep := synthLeaf(t, ix, "zzTemplateMode", true)
		if v := leafViolations(l, rep); len(v) != 0 {
			t.Errorf("exempted mode still reported: %q", v)
		}
	})
	t.Run("stale output-mode exemption", func(t *testing.T) {
		nonMutatingOutputModes["cmd/zz_synthetic.go:zzSend"] = "test"
		defer delete(nonMutatingOutputModes, "cmd/zz_synthetic.go:zzSend")
		wantViolation(t, staleOutputExemptions(ix), "cmd/zz_synthetic.go:zzSend")
	})
	t.Run("read in a builder's eager body", func(t *testing.T) {
		l, rep := synthLeaf(t, ix, "zzEagerGet", true)
		wantViolation(t, leafViolations(l, rep), "a .Get read outside withPreRead/withWait")
	})
	t.Run("stdout write in a builder's eager body", func(t *testing.T) {
		l, rep := synthLeaf(t, ix, "zzEagerWrite", true)
		wantViolation(t, leafViolations(l, rep), ".Write to stdout")
	})
	t.Run("read in a builder's returned closure is allowed", func(t *testing.T) {
		l, rep := synthLeaf(t, ix, "zzLazyGet", true)
		if v := leafViolations(l, rep); len(v) != 0 {
			t.Errorf("deferred read reported: %q", v)
		}
	})
	t.Run("stdout write inside a pre-read", func(t *testing.T) {
		l, rep := synthLeaf(t, ix, "zzPreReadStdout", true)
		wantViolation(t, leafViolations(l, rep), "fmt.Fprintln to stdout")
	})
	t.Run("input file read inside a pre-read", func(t *testing.T) {
		l, rep := synthLeaf(t, ix, "zzPreReadInput", true)
		wantViolation(t, leafViolations(l, rep), "readJSONFile inside withPreRead/withWait")
	})
	t.Run("stdout writer held in a field", func(t *testing.T) {
		l, rep := synthLeaf(t, ix, "zzFieldWriter", true)
		wantViolation(t, leafViolations(l, rep), ".Write to stdout")
	})
	t.Run("http.Post", func(t *testing.T) {
		wantViolation(t, ruleAViolations(ix), "zz_synthetic.go:zzHTTPPost")
	})
	t.Run("http.DefaultClient.Do", func(t *testing.T) {
		wantViolation(t, ruleAViolations(ix), "zz_synthetic.go:zzDefaultDo")
	})
	t.Run("new exported client method", func(t *testing.T) {
		wantViolation(t, ruleAViolations(ix), "zz_synthetic.go:zzNewClientMethod")
	})
	t.Run("out-of-scope entry covers only its call", func(t *testing.T) {
		outOfScopeSelectors["cmd/zz_synthetic.go:zzScopedOther keychain.Delete"] = "test"
		defer delete(outOfScopeSelectors, "cmd/zz_synthetic.go:zzScopedOther keychain.Delete")
		got := ruleAViolations(ix)
		wantViolation(t, got, ".Post in cmd/zz_synthetic.go:zzScopedOther")
		wantViolation(t, got, ".Delete in cmd/zz_synthetic.go:zzScoped ")
		for _, g := range got {
			if strings.Contains(g, ".Delete in cmd/zz_synthetic.go:zzScopedOther") {
				t.Errorf("exempted call still reported: %s", g)
			}
		}
	})
	t.Run("stale allowlist entry", func(t *testing.T) {
		readOnlyPOST["cmd/zz_gone.go:nothing"] = "test"
		defer delete(readOnlyPOST, "cmd/zz_gone.go:nothing")
		wantViolation(t, staleAllowlistEntries(ix), "cmd/zz_gone.go:nothing")
	})
}
