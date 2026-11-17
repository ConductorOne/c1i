package cmd

import (
	"github.com/spf13/cobra"
)

var policiesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all policies (NDJSON output)",
	Long: `List every policy in the tenant, auto-paginating through the full result.

Unlike most list commands in this CLI, GET /api/v1/policies takes no query
filter at all (only pagination) — use "policies search" for
query/display-name/policy-type filtering, or to include soft-deleted
policies.

Rows carry step_kinds, naming each step of the policy's baseline sequence in
the order it runs — each one of approval, provision, accept, reject, wait,
form, action. It is what separates an auto-approval policy from an approval
gate, which policy_type/step_count/system_builtin cannot: a built-in
auto-approval grant policy is a grant policy whose baseline is a lone accept
step.

  c1i policies list | jq -c 'select(.policy_type=="POLICY_TYPE_GRANT" and
    .system_builtin and .step_kinds==["accept"] and .baseline_policy_id==null)'

Match on that, never on the display name — one tenant spells it
"Auto-approval", the next "Auto approval". Nothing guarantees the match is
unique, so check what comes back rather than taking the first row: a tenant
can hold more than one auto-granting policy, and rule_count is what tells a
conditionally-routed one from a plain one (> 0 means conditional rules route
to alternative sequences). Two such policies can both have rule_count 0, and
then nothing on the row separates them.

Two more things the row states rather than hides. step_count counts every step
in every sequence, including those alternatives, so it can exceed step_kinds'
length. And baseline_policy_id is non-null when a policy defers its baseline
to another policy instead of holding one — step_kinds is then [] for a
perfectly healthy policy, and the clause above excludes it. Find those rows,
then follow the reference through this same listing:

  c1i policies list | jq -c 'select(.baseline_policy_id)'
  c1i policies list | jq -c 'select(.id=="<baseline-policy-id>")'

Not "policies get": that is a verbatim passthrough of the API object, so it
carries policySteps and baselinePolicyId and neither of the derived keys the
recipe above reads.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientVia(cmd, newPoliciesClient)
		if err != nil {
			return err
		}
		// A page can come back short (even empty) while nextPageToken is still set:
		// scope filtering happens after the page is read from storage. Paging follows
		// the token, not whether the page had rows.
		return listPages(cmd, c, pageSpec[policyListItem]{
			Route: "GET /api/v1/policies",
			Fetch: getPage(cmd.Context(), "/api/v1/policies", nil),
			Parse: parseList[policyListItem],
			ID:    func(it policyListItem) string { return it.ID },
			Rows:  rowFn(policyRow),
		})
	},
}

func init() {
	// One of the endpoints behind the shared --page-size caveat: it
	// over-fetches to absorb server-side filtering and does not trim back
	// (measured live, page_size=10 returned 12 rows). Cursor-following and
	// --limit are unaffected.
	addPaginationFlags(policiesListCmd)
	policiesCmd.AddCommand(policiesListCmd)
}
