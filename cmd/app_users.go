package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
)

// existingAppUserCheck searches for the app account under its app and turns a
// miss into a not-found error naming --app-user-id. There is no public GET by
// id for app users, so this is the search the API exposes, filtered to the one
// id; an account that exists in another app is not found here on purpose.
func existingAppUserCheck(appID, appUserID string) preRead {
	return preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		fetch := postPageBody(ctx, "/api/v1/search/app_users", func(size int, token string) map[string]any {
			body := map[string]any{"appId": appID, "appUserIds": []string{appUserID}}
			if token != "" {
				body["pageToken"] = token
			}
			body["pageSize"] = size
			return body
		})
		data, err := fetch(c, 1, "")
		if err != nil {
			return nil, fmt.Errorf("checking --app-user-id %q: %w", appUserID, err)
		}
		var resp struct {
			List []struct {
				AppUser struct {
					ID string `json:"id"`
				} `json:"appUser"`
			} `json:"list"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, &nonJSONResponseError{fmt.Errorf("checking --app-user-id %q: the app user search did not return JSON", appUserID)}
		}
		for _, v := range resp.List {
			if v.AppUser.ID == appUserID {
				return nil, nil
			}
		}
		return nil, refNotFoundLocal("app-user-id", appUserID, "app user")
	}}
}
