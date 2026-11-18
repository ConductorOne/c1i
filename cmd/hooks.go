package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var hooksCmd = &cobra.Command{
	Use:     "hooks",
	Aliases: []string{"hook"},
	Short:   "Manage hooks (interception points for tool calls)",
	Long: `Manage hooks: customer-configured interception points for tool calls. A hook
fires on an event (pre-tool-use, post-tool-use or pre-output), optionally
narrowed by a CEL filter, and runs exactly one of a function, a built-in
pattern or a JSON patch.

  hooks list
  hooks get <hook-id>
  hooks create --display-name "audit" --event pre-tool-use --function-id <function-id>
  hooks update <hook-id> --priority 10
  hooks delete <hook-id>

Create takes a function hook from flags. A built-in pattern or JSON patch hook
(whose configs the flags do not model) goes through --body-file, a full Hook
object. A hook is created disabled unless you pass --enabled; an enabled hook
intercepts tool calls.`,
}

func init() {
	rootCmd.AddCommand(hooksCmd)
}

const hooksPath = "/api/v1/hooks"

func hookPath(id string) string { return client.Path(hooksPath+"/%s", id) }

var hookEventFlag = enumFlag{flag: "event", prefix: "HOOK_EVENT_TYPE_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "pre-tool-use", api: "HOOK_EVENT_TYPE_PRE_TOOL_USE"},
	{name: "post-tool-use", api: "HOOK_EVENT_TYPE_POST_TOOL_USE"},
	{name: "pre-output", api: "HOOK_EVENT_TYPE_PRE_OUTPUT"},
}}

// hookFieldFlags registers the writable Hook flags shared by create and update.
func hookFieldFlags(cmd *cobra.Command, update bool) {
	verb, keep := "Hook", ""
	if update {
		verb, keep = "New", "omit to leave unchanged; "
	}
	f := cmd.Flags()
	reqd := ""
	if !update {
		reqd = " (required unless --body-file)"
	}
	f.String("display-name", "", verb+" display name ("+keep+displayNameRule+")"+reqd)
	descUsage := `Hook description; "" is sent as an empty value`
	if update {
		descUsage = `New description (omit to leave unchanged; "" is sent as an empty value)`
	}
	f.String("description", "", descUsage)
	allowEmpty(cmd, emptyIsSent, "description")
	f.String("event", "", verb+" event ("+keep+hookEventFlag.usage()+")"+reqd)
	f.String("filter-cel", "", verb+" CEL filter, evaluated against the event context ("+keep+"not validated by the server)")
	f.Int("priority", 0, verb+" priority ("+keep+"the server enforces its range)")
	tail := ""
	if update {
		tail = " (omit to leave unchanged)"
	}
	f.Bool("enabled", false, "Enable (--enabled) or disable (--enabled=false) the hook"+tail)
	f.Bool("managed-by-guardrails", false, "Make the hook selectable in a guardrail rule's curated hook lists; a hook left false always runs"+tail)
	f.String("function-id", "", verb+" function to run ("+keep+"a function hook)"+reqd)
	f.String("function-commit-id", "", verb+" function commit to run (needs --function-id; omit for the function's published commit)")
}

var hookFilterField = hookUpdate([]string{"filter-cel"}, func(cmd *cobra.Command, obj map[string]any) ([]string, error) {
	if !flagGiven(cmd, "filter-cel") {
		return nil, nil
	}
	obj["filter"] = map[string]any{"celExpression": mustGetString(cmd, "filter-cel")}
	return []string{"filter"}, nil
})

var hookFunctionField = hookUpdate([]string{"function-id", "function-commit-id"}, func(cmd *cobra.Command, obj map[string]any) ([]string, error) {
	if !flagGiven(cmd, "function-id") {
		if flagGiven(cmd, "function-commit-id") {
			return nil, &usageError{fmt.Errorf("--function-commit-id needs --function-id")}
		}
		return nil, nil
	}
	fn := map[string]any{"functionId": mustGetString(cmd, "function-id")}
	if flagGiven(cmd, "function-commit-id") {
		fn["commitId"] = mustGetString(cmd, "function-commit-id")
	}
	obj["function"] = fn
	return []string{"function"}, nil
})

// hookCommonFields are the writable Hook fields, in mask order, shared by
// create and update.
func hookCommonFields() []updateField {
	return []updateField{
		strUpdate("display-name", "displayName"),
		strUpdate("description", "description"),
		enumUpdate("event", "event", &hookEventFlag),
		hookFilterField,
		intUpdate("priority", "priority"),
		boolUpdate("enabled", "enabled"),
		boolUpdate("managed-by-guardrails", "managedByGuardrails"),
		hookFunctionField,
	}
}

var hookCreateFields = &updateSpec{fields: hookCommonFields(), alt: "--body-file", scope: []string{"body-file"}}

// hookUpdateFields adds --clear-filter: the server clears a filter only when
// the mask names "filter" and the filter is null.
var hookUpdateFields = &updateSpec{
	fields: append(hookCommonFields(), hookUpdate([]string{"clear-filter"}, func(cmd *cobra.Command, obj map[string]any) ([]string, error) {
		if v, _ := cmd.Flags().GetBool("clear-filter"); !v {
			return nil, nil
		}
		if flagGiven(cmd, "filter-cel") {
			return nil, &usageError{fmt.Errorf("--clear-filter and --filter-cel are mutually exclusive")}
		}
		obj["filter"] = nil
		return []string{"filter"}, nil
	})),
	alt:   "--body-file with --update-mask",
	scope: []string{"body-file", "update-mask"},
}
