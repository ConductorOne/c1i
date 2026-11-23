package cmd

import "github.com/spf13/cobra"

var decoysSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search decoys by text, kind, status, annotation key, or usage (NDJSON output)",
	Long: `Search decoys (POST /api/v1/decoys/search), one row per decoy with the same
fields as "decoys list". --query matches the display name and description.
--kind and --status are each repeatable and OR within themselves; given
both, a decoy must match one of each. --annotation-key (repeatable) matches
a decoy carrying at least one of the given annotation keys (OR).
--has-been-used filters to decoys that have fired at least once; omit it (or
pass --has-been-used=false) for no filter — there is no "never used" filter.
With no flag it returns every decoy.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		kinds, err := parseRepeatableEnum(cmd, decoyKindFlag)
		if err != nil {
			return err
		}
		statuses, err := parseRepeatableEnum(cmd, decoyStatusFlag)
		if err != nil {
			return err
		}
		annotationKeys, err := repeatableStringFlag(cmd, "annotation-key")
		if err != nil {
			return err
		}
		body := map[string]any{}
		if flagGiven(cmd, "query") {
			body["query"] = mustGetString(cmd, "query")
		}
		if len(kinds) > 0 {
			body["kinds"] = kinds
		}
		if len(statuses) > 0 {
			body["statuses"] = statuses
		}
		if len(annotationKeys) > 0 {
			body["annotationKeys"] = annotationKeys
		}
		if hasBeenUsed, _ := cmd.Flags().GetBool("has-been-used"); hasBeenUsed {
			body["hasBeenUsed"] = true
		}
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		return listPages(cmd, c, pageSpec[decoyListItem]{
			Route: "POST " + decoysSearchPath,
			Fetch: postPage(cmd.Context(), decoysSearchPath, body),
			Parse: parseList[decoyListItem],
			ID:    func(it decoyListItem) string { return it.ID },
			Rows:  rowFn(decoyRow),
		})
	},
}

func init() {
	decoysSearchCmd.Flags().String("query", "", "Free-text match against display name and description")
	addRepeatableStringFlag(decoysSearchCmd, "kind", "Filter by kind: "+decoyKindFlag.usage()+" (repeatable)")
	addRepeatableStringFlag(decoysSearchCmd, "status", "Filter by status: "+decoyStatusFlag.usage()+" (repeatable)")
	addRepeatableStringFlag(decoysSearchCmd, "annotation-key", "Match a decoy carrying at least one of this annotation key (repeatable)")
	decoysSearchCmd.Flags().Bool("has-been-used", false, "Only decoys that have fired at least once (there is no \"never used\" filter)")
	addPaginationFlags(decoysSearchCmd)
	decoysCmd.AddCommand(decoysSearchCmd)
}
