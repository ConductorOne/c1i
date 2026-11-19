package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var accountsGetCmd = &cobra.Command{
	Use:   "get <app-user-id>",
	Short: "Get a single account (app user) by ID (pretty JSON)",
	Long: `Get a single application account (app user) by its ID.

There is no public GET-by-id route for app users (see "accounts list"); this
runs the same POST /api/v1/search/app_users the server exposes, filtered to
the one id under --app-id, and prints the appUser object unwrapped like the
other reads -- including "profile", which "accounts list" rows drop. An id
that belongs to a different app is not found here on purpose: zero matches
exits 4, and more than one match for the same id is a C1 failure (exit 6).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		appUserID := args[0]

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		appID := mustGetString(cmd, "app-id")
		body := map[string]any{"appId": appID, "appUserIds": []string{appUserID}}
		data, err := c.Post(cmd.Context(), "/api/v1/search/app_users", body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}

		list, err := appUserSearchList(appUserID, data)
		if err != nil {
			return err
		}

		// The filter is appUserIds, not an exact-match guarantee, so match the
		// id ourselves rather than trust list[0] -- the same defensive check
		// existingAppUserCheck makes before treating a search hit as a find.
		// Each row is decoded twice into single-field structs (never a map
		// index) so the id check and the raw appUser bytes both stay outside
		// TestBodyKeyReadsAreFolded's scope, which is for user-supplied JSON.
		var matches []json.RawMessage
		for _, raw := range list {
			var idOnly struct {
				AppUser struct {
					ID string `json:"id"`
				} `json:"appUser"`
			}
			if err := json.Unmarshal(raw, &idOnly); err != nil || idOnly.AppUser.ID != appUserID {
				continue
			}
			var withRaw struct {
				AppUser json.RawMessage `json:"appUser"`
			}
			if err := json.Unmarshal(raw, &withRaw); err != nil {
				continue
			}
			matches = append(matches, withRaw.AppUser)
		}

		switch len(matches) {
		case 0:
			return &refNotFoundError{msg: fmt.Sprintf("<app-user-id> %q: no such app user under --app-id %q", appUserID, appID)}
		case 1:
			return writeResource(cmd, matches[0], "id")
		default:
			return &nonJSONResponseError{fmt.Errorf("app user search for %q under --app-id %q returned %d matches, want at most 1", appUserID, appID, len(matches))}
		}
	},
}

// appUserSearchList decodes data's "list" array. An absent, null, or
// non-array "list" is the server breaking the search contract -- a C1
// failure (exit 6) -- distinct from a present, possibly empty list that
// simply has no row for this id, which is this command's own not-found
// case (exit 4). resourceAt (cmd/unwrap.go) draws the same distinction for
// every other get's single-object envelope; this is that check for a
// list-shaped one.
func appUserSearchList(appUserID string, data []byte) ([]json.RawMessage, error) {
	malformed := &nonJSONResponseError{fmt.Errorf("app user %q: the response held no list", appUserID)}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, &nonJSONResponseError{fmt.Errorf("app user %q: the search did not return JSON: %w", appUserID, err)}
	}
	listRaw, ok := top["list"]
	if !ok {
		return nil, malformed
	}
	var probe any
	if err := json.Unmarshal(listRaw, &probe); err != nil {
		return nil, malformed
	}
	if _, isArray := probe.([]any); !isArray {
		return nil, malformed
	}
	var list []json.RawMessage
	if err := json.Unmarshal(listRaw, &list); err != nil {
		return nil, malformed
	}
	return list, nil
}

func init() {
	accountsGetCmd.Flags().String("app-id", "", "Application ID")
	markRequired(accountsGetCmd, "app-id")
	accountsCmd.AddCommand(accountsGetCmd)
}
