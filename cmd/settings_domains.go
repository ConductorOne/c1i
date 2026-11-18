package cmd

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

const settingsDomainsPath = "/api/v1/settings/domains"

var settingsDomainsCmd = &cobra.Command{Use: "domains", Short: "The tenant's verified domains"}

var settingsDomainsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the tenant's verified domains (NDJSON output)",
	Long:  `List the verified domains configured for the tenant. Rows: id, domain, created_at, updated_at, deleted_at.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return listMapPages(cmd, "GET "+settingsDomainsPath, "list", []string{"id"}, domainRow, getPage(cmd.Context(), settingsDomainsPath, nil))
	},
}

var settingsDomainsSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Replace the tenant's verified domains (pretty JSON)",
	Long: `Replace the tenant's whole set of verified domains with the --domain values
(repeatable): any domain you leave out is removed. List the current set first
with "settings domains list" and pass every domain you want to keep. A domain
already in the set keeps its id.

The server does not check that a value looks like a domain. To empty the set,
use --body-file with {"newDomains": []}; --body-file is the whole request and
is mutually exclusive with --domain. A body without a "newDomains" array
(missing or null) exits 2 and nothing is sent.

Prints the response as pretty JSON: {"list": [...]} with the resulting set.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		var body map[string]any
		if flagGiven(cmd, "body-file") {
			if flagGiven(cmd, "domain") {
				return &usageError{fmt.Errorf("--body-file is mutually exclusive with --domain")}
			}
			var err error
			if body, err = readRequiredJSONObject(cmd, "body-file"); err != nil {
				return err
			}
			// A missing or null list must not read as "empty the set"; only an
			// explicit [] does.
			_, ok, err := bodyFieldAs[[]any](body, "newDomains")
			if err != nil {
				return err
			}
			if !ok {
				return &usageError{fmt.Errorf(`--body-file must set "newDomains" to an array; send {"newDomains": []} to empty the set`)}
			}
		} else {
			domains, err := repeatableStringFlag(cmd, "domain")
			if err != nil {
				return err
			}
			if len(domains) == 0 {
				return &usageError{fmt.Errorf("pass --domain (repeatable) or --body-file")}
			}
			body = map[string]any{"newDomains": domains}
		}
		return sendMutation(cmd, mutation{Method: http.MethodPut, Path: settingsDomainsPath, Body: body})
	},
}

func domainRow(item map[string]any) map[string]any {
	return map[string]any{
		"id": optString(item, "id"), "domain": optString(item, "domain"),
		"created_at": optString(item, "createdAt"), "updated_at": optString(item, "updatedAt"), "deleted_at": optString(item, "deletedAt"),
	}
}

func init() {
	addPaginationFlags(settingsDomainsListCmd)
	addRepeatableStringFlag(settingsDomainsSetCmd, "domain", "Domain to keep or add (repeatable; the set is replaced by exactly these)")
	settingsDomainsSetCmd.Flags().String("body-file", "", `JSON request object, e.g. {"newDomains": [...]} (file, or "-" for stdin); mutually exclusive with --domain`)
	markMutating(settingsDomainsSetCmd)
	settingsDomainsCmd.AddCommand(settingsDomainsListCmd, settingsDomainsSetCmd)
	settingsCmd.AddCommand(settingsDomainsCmd)
}
