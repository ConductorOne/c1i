package cmd

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var functionsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a function",
	Long: `Create a TypeScript function (function type FUNCTION_TYPE_ANY, the type the
Functions UI lists). --display-name is required.

Each --file uploads one local file as the initial commit, named by its base
name: --file ./src/main.ts becomes main.ts, the entry file. The API's own
description says main.ts must export a default async handler that returns a
JSON object. Two files with the same base name exit 2. Without --file the
function is created with no commit.

The new function is unpublished, and "functions invoke" runs the published
commit by default. To publish the initial commit, pass its id (.commit.id in
this command's output) to "functions update <function-id> --publish-commit".

Prints the function, unwrapped like "functions get": the id is .id; the
initial commit is the sibling .commit (null without --file). Honors --dry-run.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		f := cmd.Flags()
		name, _ := f.GetString("display-name")
		message, _ := f.GetString("commit-message")
		body := map[string]any{
			"displayName":   name,
			"functionType":  "FUNCTION_TYPE_ANY",
			"commitMessage": message,
		}
		if flagGiven(cmd, "description") {
			description, _ := f.GetString("description")
			body["description"] = description
		}
		files, err := functionFileMap(cmd)
		if err != nil {
			return err
		}
		if files != nil {
			body["initialContent"] = files
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/functions", Body: body, Unwrap: named("function")})
	},
}

// functionFileMap reads every --file into the path→bytes map create and update
// send (encoding/json base64-encodes []byte). nil when no --file was given.
func functionFileMap(cmd *cobra.Command) (map[string][]byte, error) {
	paths, err := repeatableStringFlag(cmd, "file")
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	files := make(map[string][]byte, len(paths))
	from := make(map[string]string, len(paths))
	for _, p := range paths {
		name := filepath.Base(p)
		if prev, dup := from[name]; dup {
			return nil, &usageError{fmt.Errorf("--file %q and --file %q both upload as %q", prev, p, name)}
		}
		data, err := os.ReadFile(p) // #nosec G304 -- user-supplied --file path is intentional
		if err != nil {
			return nil, &usageError{fmt.Errorf("reading --file: %w", err)}
		}
		files[name], from[name] = data, p
	}
	return files, nil
}

func init() {
	f := functionsCreateCmd.Flags()
	f.String("display-name", "", "Display name (required)")
	f.String("description", "", `Description; "" is sent as an empty value`)
	allowEmpty(functionsCreateCmd, emptyIsSent, "description")
	addRepeatableStringFlag(functionsCreateCmd, "file", "Local source file for the initial commit, uploaded under its base name (repeatable; main.ts is the entry file)")
	f.String("commit-message", "Initial commit", "Message of the initial commit (the API requires one)")
	markRequired(functionsCreateCmd, "display-name")
	markMutating(functionsCreateCmd)
	functionsCmd.AddCommand(functionsCreateCmd)
}
