package cmd

import "github.com/spf13/cobra"

// decoyListItem is the subset of Decoy surfaced in list/search rows.
type decoyListItem struct {
	ID                        string            `json:"id"`
	DisplayName               string            `json:"displayName"`
	Description               string            `json:"description"`
	Kind                      string            `json:"kind"`
	Disabled                  bool              `json:"disabled"`
	Annotations               map[string]string `json:"annotations"`
	CreatedAt                 string            `json:"createdAt"`
	UpdatedAt                 string            `json:"updatedAt"`
	LastUsedAt                string            `json:"lastUsedAt"`
	MaterialFingerprintSha256 string            `json:"materialFingerprintSha256"`
}

// decoyRow flattens a decoy into an NDJSON row. Never the secret material:
// that is returned only once, by "decoys create" or "decoys rotate".
func decoyRow(d decoyListItem) map[string]any {
	return map[string]any{
		"id":                          nilIfEmpty(d.ID),
		"display_name":                nilIfEmpty(d.DisplayName),
		"description":                 nilIfEmpty(d.Description),
		"kind":                        nilIfEmpty(d.Kind),
		"disabled":                    d.Disabled,
		"annotations":                 d.Annotations,
		"created_at":                  nilIfEmpty(d.CreatedAt),
		"updated_at":                  nilIfEmpty(d.UpdatedAt),
		"last_used_at":                nilIfEmpty(d.LastUsedAt),
		"material_fingerprint_sha256": nilIfEmpty(d.MaterialFingerprintSha256),
	}
}

var decoysListCmd = &cobra.Command{
	Use:   "list",
	Short: "List decoys (NDJSON output)",
	Long: `List decoys for the tenant (NDJSON output), paginated. Never prints secret
material: that is shown only once, by "decoys create" or "decoys rotate".`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		return listPages(cmd, c, pageSpec[decoyListItem]{
			Route: "GET " + decoysPath,
			Fetch: getPage(cmd.Context(), decoysPath, nil),
			Parse: parseList[decoyListItem],
			ID:    func(it decoyListItem) string { return it.ID },
			Rows:  rowFn(decoyRow),
		})
	},
}

func init() {
	addPaginationFlags(decoysListCmd)
	decoysCmd.AddCommand(decoysListCmd)
}
