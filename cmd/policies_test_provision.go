package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

const policiesTestProvisionPath = "/api/v1/policies/test-account-provision-policy"

var policiesTestProvisionCmd = &cobra.Command{
	Use:   "test-provision <cel>",
	Short: "Evaluate a CEL expression for an account provision policy (pretty JSON)",
	Long: `Evaluate a CEL expression with the account provision policy tester
(POST /api/v1/policies/test-account-provision-policy) and print the result:
"value" (a string) and "type" (the result's data type). Nothing is created or
changed.

Quote the expression as one shell argument, or pass several unquoted words;
they are joined with spaces, like "policies validate-cel".

An expression that does not compile is answered by the server with a 500 that
carries the CEL error text (exit 6), not a 4xx; c1i does not check CEL itself.
Use "policies validate-cel" for compile checks of rule conditions.
  c1i policies test-provision '1+1'`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), policiesTestProvisionPath, map[string]any{"cel": strings.Join(args, " ")})
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeObject(cmd, data)
	},
}

func init() {
	policiesCmd.AddCommand(policiesTestProvisionCmd)
}
