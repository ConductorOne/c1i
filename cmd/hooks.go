package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

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
	f.Int("priority", 0, verb+" priority ("+keep+"0 to 1000)")
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

// hookPriorityField sends --priority. Create is validated by the server in
// [0,1000], but Update is not, so refuse an out-of-range value before the send.
var hookPriorityField = updateField{flags: []string{"priority"}, apply: func(cmd *cobra.Command, obj map[string]any) ([]string, error) {
	if !flagGiven(cmd, "priority") {
		return nil, nil
	}
	v, err := cmd.Flags().GetInt("priority")
	if err != nil {
		return nil, err
	}
	if v < 0 || v > 1000 {
		return nil, &usageError{fmt.Errorf("--priority must be between 0 and 1000 (got %d)", v)}
	}
	obj["priority"] = v
	return []string{"priority"}, nil
}}

// hookCommonFields are the writable Hook fields, in mask order, shared by
// create and update.
func hookCommonFields() []updateField {
	return []updateField{
		strUpdate("display-name", "displayName"),
		strUpdate("description", "description"),
		enumUpdate("event", "event", &hookEventFlag),
		hookFilterField,
		hookPriorityField,
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

// hookFunctionPreRead looks up --function-id, and --function-commit-id under it,
// before the send: the server answers an unknown function with a 400 and stores
// an unknown commit id with a 200. Not applied to --body-file, which is verbatim.
func hookFunctionPreRead(cmd *cobra.Command) []mutationOpt {
	if !flagGiven(cmd, "function-id") {
		return nil
	}
	fn := mustGetString(cmd, "function-id")
	commit := ""
	if flagGiven(cmd, "function-commit-id") {
		commit = mustGetString(cmd, "function-commit-id")
	}
	return []mutationOpt{withPreRead(preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		if err := requireHookRef(ctx, c, client.Path("/api/v1/functions/%s", fn), "function", "function-id", fn, "function"); err != nil {
			return nil, err
		}
		if commit == "" {
			return nil, nil
		}
		return nil, requireHookRef(ctx, c, client.Path("/api/v1/functions/%s/commits/%s", fn, commit), "commit", "function-commit-id", commit, "commit of that function")
	}})}
}

// requireHookRef refuses an unknown or deleted object; envelope is the
// response key holding it.
func requireHookRef(ctx context.Context, c *client.Client, path, envelope, flag, id, what string) error {
	data, err := c.Get(ctx, path, nil)
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return refNotFound(flag, id, what, apiErr)
		}
		return fmt.Errorf("checking --%s %q: %w", flag, id, err)
	}
	deleted, _, ok := deletedAtPath(data, envelope)
	if !ok {
		return &nonJSONResponseError{fmt.Errorf("checking --%s %q: the lookup did not return a %s", flag, id, what)}
	}
	if deleted {
		return &usageError{fmt.Errorf("--%s %q: the %s is deleted", flag, id, what)}
	}
	return nil
}

// checkHookPriorityValue refuses an out-of-range priority in a --body-file
// when the mask names it: Update does not validate it, so the server stores it.
func checkHookPriorityValue(mask string, hook map[string]any) error {
	named := false
	for _, path := range strings.Split(mask, ",") {
		if foldMaskName(path) == foldMaskName("priority") {
			named = true
		}
	}
	if !named {
		return nil
	}
	v, ok := hook["priority"]
	if !ok {
		return nil
	}
	var n int64
	switch x := v.(type) {
	case nil:
		return nil // the server clears it to 0
	case float64:
		n = int64(x)
		if float64(n) != x {
			return &usageError{fmt.Errorf("--body-file priority must be a whole number (got %v)", x)}
		}
	case int:
		n = int64(x)
	case string:
		// The server coerces a numeric string, so range-check it too.
		parsed, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return nil // not a number; the server decides
		}
		n = parsed
	default:
		return nil // the server decides
	}
	if n < 0 || n > 1000 {
		return &usageError{fmt.Errorf("--body-file priority must be between 0 and 1000 (got %d)", n)}
	}
	return nil
}
