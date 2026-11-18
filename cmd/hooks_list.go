package cmd

import (
	"github.com/spf13/cobra"
)

// hookKind names which of a hook's mutually exclusive type fields is set.
func hookKind(h map[string]any) any {
	for _, k := range [][2]string{{"function", "function"}, {"builtinPattern", "builtin_pattern"}, {"jsonPatch", "json_patch"}} {
		if v, ok := h[k[0]].(map[string]any); ok && len(v) > 0 {
			return k[1]
		}
	}
	return nil
}

// hookRow flattens a hook into an NDJSON row; absent strings are null.
func hookRow(h map[string]any) map[string]any {
	str := func(k string) any {
		s, _ := h[k].(string)
		return nilIfEmpty(s)
	}
	return map[string]any{
		"id":                    str("id"),
		"display_name":          str("displayName"),
		"description":           str("description"),
		"event":                 str("event"),
		"kind":                  hookKind(h),
		"enabled":               h["enabled"],
		"managed_by_guardrails": h["managedByGuardrails"],
		"priority":              h["priority"],
		"created_at":            str("createdAt"),
		"updated_at":            str("updatedAt"),
	}
}

var hooksListCmd = &cobra.Command{
	Use:   "list",
	Short: "List hooks (NDJSON output)",
	Long: `List hooks (NDJSON output), one row per hook: id, display_name, description,
event, kind (function, builtin_pattern or json_patch), enabled,
managed_by_guardrails, priority and the timestamps. Deleted hooks are omitted.
"hooks get" shows the full config (filter, function, built-in pattern).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return listMapPages(cmd, "GET "+hooksPath, "list", []string{"id"}, hookRow,
			getPage(cmd.Context(), hooksPath, nil))
	},
}

func init() {
	addPaginationFlags(hooksListCmd)
	hooksCmd.AddCommand(hooksListCmd)
}
