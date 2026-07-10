package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var functionsSourceCmd = &cobra.Command{
	Use:   "source <function-id>",
	Short: "Fetch a function's source files (auto-resolves the published commit)",
	Long: `Fetch the TypeScript source for a C1 function.

Without --commit, resolves to the function's published commit, falling
back to its head (latest draft) if nothing is published. The C1 API
returns source files base64-encoded under a 'files' map; this command
decodes them and writes each file as text.

By default each file is printed to stdout separated by a delimiter line.
Use --out-dir to write files to disk instead (one file per source file,
named exactly as the function has them — usually main.ts and main.test.ts).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		functionID := args[0]
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		commitID, _ := cmd.Flags().GetString("commit")
		outDir, _ := cmd.Flags().GetString("out-dir")

		if commitID == "" {
			fnData, err := c.Get(cmd.Context(), client.Path("/api/v1/functions/%s", functionID), nil)
			if err != nil {
				return fmt.Errorf("API error fetching function metadata: %w", err)
			}
			// Single-resource GET wraps the body under "function"; list responses
			// don't wrap. Accept both shapes so the parser doesn't silently miss
			// the commit ID if the API ever flattens.
			var wrapper struct {
				Function struct {
					PublishedCommitID string `json:"publishedCommitId"`
					Head              string `json:"head"`
				} `json:"function"`
				PublishedCommitID string `json:"publishedCommitId"`
				Head              string `json:"head"`
			}
			if err := json.Unmarshal(fnData, &wrapper); err != nil {
				return fmt.Errorf("failed to parse function response: %w", err)
			}
			commitID = wrapper.Function.PublishedCommitID
			if commitID == "" {
				commitID = wrapper.PublishedCommitID
			}
			if commitID == "" {
				commitID = wrapper.Function.Head
			}
			if commitID == "" {
				commitID = wrapper.Head
			}
			if commitID == "" {
				return fmt.Errorf("function %s has no published commit or head; nothing to fetch", functionID)
			}
		}

		commitData, err := c.Get(cmd.Context(), client.Path("/api/v1/functions/%s/commits/%s", functionID, commitID), nil)
		if err != nil {
			return fmt.Errorf("API error fetching commit %s: %w", commitID, err)
		}

		// The commit response holds source files under "files" as a base64-encoded
		// map. Earlier API revisions used "content" — accept both so this command
		// keeps working across API updates.
		var commit struct {
			Files   map[string]string `json:"files"`
			Content map[string]string `json:"content"`
		}
		if err := json.Unmarshal(commitData, &commit); err != nil {
			return fmt.Errorf("failed to parse commit response: %w", err)
		}
		files := commit.Files
		if len(files) == 0 {
			files = commit.Content
		}
		if len(files) == 0 {
			return fmt.Errorf("commit %s has no source files", commitID)
		}

		// Sort for deterministic output across runs.
		names := make([]string, 0, len(files))
		for name := range files {
			names = append(names, name)
		}
		sort.Strings(names)

		if outDir != "" {
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return fmt.Errorf("failed to create out-dir: %w", err)
			}
		}

		out := cmd.OutOrStdout()
		for _, name := range names {
			decoded, err := base64.StdEncoding.DecodeString(files[name])
			if err != nil {
				return fmt.Errorf("failed to base64-decode %s: %w", name, err)
			}
			if outDir != "" {
				// The filename comes from the API response. Reject anything
				// that isn't a plain filename so a hostile or buggy server
				// can't write outside --out-dir (e.g. "../../etc/cron.d/x").
				if unsafeSourceName(name) {
					return fmt.Errorf("refusing to write file with unsafe name %q", name)
				}
				path := filepath.Join(outDir, name)
				if err := os.WriteFile(path, decoded, 0o644); err != nil {
					return fmt.Errorf("failed to write %s: %w", path, err)
				}
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s (%d bytes)\n", path, len(decoded))
			} else {
				_, _ = fmt.Fprintf(out, "// ===== %s =====\n", name)
				_, _ = out.Write(decoded)
				if len(decoded) > 0 && decoded[len(decoded)-1] != '\n' {
					_, _ = out.Write([]byte("\n"))
				}
			}
		}
		return nil
	},
}

// unsafeSourceName reports whether an API-supplied filename should be refused
// when writing to --out-dir. We only ever expect plain filenames (main.ts,
// main.test.ts), so anything containing a path separator, a parent-directory
// reference, or an absolute/empty name is rejected rather than joined onto
// the output directory.
func unsafeSourceName(name string) bool {
	return name == "" || name == "." || name == ".." || name != filepath.Base(name)
}

func init() {
	functionsSourceCmd.Flags().String("commit", "", "Specific commit ID to fetch (default: the function's publishedCommitId, falling back to head)")
	functionsSourceCmd.Flags().String("out-dir", "", "Write files to this directory instead of stdout")
	functionsCmd.AddCommand(functionsSourceCmd)
}
