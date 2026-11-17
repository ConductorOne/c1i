package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// requestSearchFilters holds the resolved filters for a `requests list` query,
// separated from flag parsing so the search-body construction is unit-testable.
type requestSearchFilters struct {
	pageSize      int
	pageToken     string
	scopeUserID   string // opener-or-subject scope; empty means tenant-wide (--all)
	appID         string
	entitlementID string
	state         string // TASK_STATE_* enum, or "" for no filter
	typ           string // "", "grant", or "revoke"
}

// buildRequestSearchBody turns resolved filters into the /api/v1/search/tasks
// request body. Requests are grant/revoke tasks, so taskTypes is always
// constrained to those (never certify/offboarding), narrowed further by --type.
func buildRequestSearchBody(f requestSearchFilters) map[string]any {
	body := map[string]any{"pageSize": f.pageSize}
	if f.pageToken != "" {
		body["pageToken"] = f.pageToken
	}

	taskTypes := make([]map[string]any, 0, 2)
	if f.typ == "" || f.typ == "grant" {
		taskTypes = append(taskTypes, map[string]any{"grant": map[string]any{}})
	}
	if f.typ == "" || f.typ == "revoke" {
		taskTypes = append(taskTypes, map[string]any{"revoke": map[string]any{}})
	}
	body["taskTypes"] = taskTypes

	if f.scopeUserID != "" {
		body["openerOrSubjectUserId"] = f.scopeUserID
	}
	if f.appID != "" {
		body["applicationIds"] = []string{f.appID}
	}
	if f.entitlementID != "" {
		body["appEntitlementIds"] = []string{f.entitlementID}
	}
	if f.state != "" {
		body["taskStates"] = []string{f.state}
	}
	return body
}

var requestsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List access requests and their status (NDJSON output)",
	Long: `List access requests — the grant and revoke tasks a requester files.

This is the requester lens: by default it shows requests you opened or are the
subject of, complementing "c1i tasks list" (the approver's My Work lens). Use
--user-id to scope to another user, or --all for every access request in the
tenant.

  # Requests I've filed (and their status)
  c1i requests list

  # Only open grant requests I've filed
  c1i requests list --type grant --state open

  # Every request for a given entitlement (admin)
  c1i requests list --all --app-id APP --entitlement-id ENT`,
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		entitlementID, _ := cmd.Flags().GetString("entitlement-id")
		userID, _ := cmd.Flags().GetString("user-id")
		all, _ := cmd.Flags().GetBool("all")
		var typ string
		// Only an empty value means "both". A whitespace-only value goes to
		// the parser, which rejects it instead of dropping the filter.
		if v := mustGetString(cmd, "type"); v != "" {
			var err error
			if typ, err = requestTypeFlag.parse(v); err != nil {
				return err
			}
		}
		state, err := parseTaskState(cmd)
		if err != nil {
			return err
		}
		if userID != "" && all {
			return &usageError{fmt.Errorf("--user-id and --all are mutually exclusive")}
		}

		if entitlementID != "" && appID == "" {
			return &usageError{fmt.Errorf("--entitlement-id requires --app-id (entitlements are scoped to an app)")}
		}

		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}

		// Resolve the requester scope. Default is "me" (opener or subject); --all
		// widens to the whole tenant; --user-id targets someone else.
		scopeUserID := userID
		if !all && scopeUserID == "" {
			scopeUserID, err = currentUserID(cmd.Context(), c)
			if err != nil {
				return err
			}
			// Never silently fall through to a tenant-wide listing if we can't
			// resolve the caller — that would leak far more than "my requests".
			if scopeUserID == "" {
				return &usageError{fmt.Errorf("could not determine the current user; pass --user-id or --all")}
			}
		}

		return listPages(cmd, c, pageSpec[taskSearchItem]{
			Route: "POST /api/v1/search/tasks",
			Fetch: postPageBody(cmd.Context(), "/api/v1/search/tasks", func(size int, token string) map[string]any {
				return buildRequestSearchBody(requestSearchFilters{
					pageSize:      size,
					pageToken:     token,
					scopeUserID:   scopeUserID,
					appID:         appID,
					entitlementID: entitlementID,
					state:         state,
					typ:           typ,
				})
			}),
			Parse: parseList[taskSearchItem],
			ID:    taskItemID,
			Rows:  taskItemRows,
		})
	},
}

func init() {
	requestsListCmd.Flags().String("user-id", "", "Scope to requests opened by or about this user (default: you)")
	requestsListCmd.Flags().Bool("all", false, "List every access request in the tenant, not just yours")
	requestsListCmd.Flags().String("app-id", "", "Filter to requests targeting this application")
	requestsListCmd.Flags().String("entitlement-id", "", "Filter to requests for this entitlement (requires --app-id)")
	requestsListCmd.Flags().String("state", "", "Filter by state: "+taskStateFlag.usage())
	requestsListCmd.Flags().String("type", "", "Filter by request type: "+requestTypeFlag.usage()+" (default: both)")
	addPaginationFlags(requestsListCmd)
	requestsCmd.AddCommand(requestsListCmd)
}

// requestTypeFlag is client-side: it picks which task types to search.
var requestTypeFlag = enumFlag{flag: "type", unknown: enumReject, values: []enumValue{
	{name: "grant", api: "grant"},
	{name: "revoke", api: "revoke"},
}}
