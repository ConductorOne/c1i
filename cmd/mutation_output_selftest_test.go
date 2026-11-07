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
	"fmt"
	"os"

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
`

func synthIndex(t *testing.T) *srcIndex {
	t.Helper()
	return loadSrcIndex(t, map[string]string{"cmd/zz_synthetic.go": synthViolations})
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
		wantViolation(t, stderrViolations(ix), "zz_synthetic.go")
	})
	t.Run("stale allowlist entry", func(t *testing.T) {
		readOnlyPOST["cmd/zz_gone.go:nothing"] = "test"
		defer delete(readOnlyPOST, "cmd/zz_gone.go:nothing")
		wantViolation(t, staleAllowlistEntries(ix), "cmd/zz_gone.go:nothing")
	})
}
