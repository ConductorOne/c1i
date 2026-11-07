package cmd

import (
	"io"

	"github.com/spf13/cobra"
)

// Shared harness for the mutation output tests (cmd/mutation_*_test.go).

// renderTextError renders err as Run does in text mode.
func renderTextError(w io.Writer, err error, ran *cobra.Command) { writeError(w, err, "text", ran) }
