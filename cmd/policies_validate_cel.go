package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var policiesValidateCelCmd = &cobra.Command{
	Use:   "validate-cel <condition>",
	Short: "Check a CEL condition for compile errors (pretty JSON)",
	Long: `Check a CEL expression for compile errors, without creating or updating
anything. Cheap way to test a rules[].condition before using it in
"policies create"/"update".

This validates the "policy_condition" CEL environment — the one rules[]
conditions run in. Its root variable is "subject" (a User), plus "account"
(AppUser), "entitlement" (AppEntitlement), and "task"; "user" is not
declared and errors as an undeclared reference.

This is NOT the same environment ExpressionApproval.expressions run in
(the "policy_step" environment: "subject", "app_owners"/"appOwners",
"entitlement", "task" — no "account"). An expression written for an
approval step's --steps-file may validate here and still fail there, or
vice versa (e.g. "app_owners" is undeclared here but valid there) — this
command cannot check that environment.

The response has no "valid" boolean; success is an empty "markers" array,
and each compile error becomes one marker with severity ERROR and a
line/column location. This command additionally prints a synthesized
"valid" field derived from that (true iff markers is empty) for
convenience.

Quote the condition as a single shell argument, or pass several unquoted
words (like "docs search") — they're joined with spaces either way:
  c1i policies validate-cel 'subject.role == "admin"'
  c1i policies validate-cel true`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		condition := strings.Join(args, " ")

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newPoliciesClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		data, err := c.Post(cmd.Context(), "/api/v1/policies/validate/cel", map[string]any{"text": condition})
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		var resp struct {
			Markers []json.RawMessage `json:"markers"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		markers := resp.Markers
		if markers == nil {
			markers = []json.RawMessage{}
		}
		out := map[string]any{
			"valid":   len(markers) == 0,
			"markers": markers,
		}
		enriched, err := json.Marshal(out)
		if err != nil {
			return fmt.Errorf("marshaling response: %w", err)
		}

		return writeObject(cmd, enriched)
	},
}

func init() {
	policiesCmd.AddCommand(policiesValidateCelCmd)
}
