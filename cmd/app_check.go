package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
)

// requireLiveApp reads the app before a command that the server would let act
// on an unknown or soft-deleted app: unknown exits 4, deleted exits 2. label
// names where the id came from ("--app-id", "<app-id>").
func requireLiveApp(ctx context.Context, c *client.Client, appID, label string) error {
	data, err := c.Get(ctx, client.Path("/api/v1/apps/%s", appID), nil)
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return &refNotFoundError{fmt.Sprintf("%s %q: no such app", label, appID), apiErr}
		}
		return fmt.Errorf("checking %s %q: %w", label, appID, err)
	}
	var resp struct {
		App *struct {
			DeletedAt *string `json:"deletedAt"`
		} `json:"app"`
	}
	if err := json.Unmarshal(data, &resp); err != nil || resp.App == nil {
		return &nonJSONResponseError{fmt.Errorf("checking %s %q: the response held no app", label, appID)}
	}
	if resp.App.DeletedAt != nil {
		return &usageError{fmt.Errorf("%s %q: the app is deleted", label, appID)}
	}
	return nil
}

// liveAppPreRead is requireLiveApp as a mutation's pre-send read, so a
// --dry-run refuses what a real run refuses.
func liveAppPreRead(appID, label string) mutationOpt {
	return withPreRead(preRead{Run: func(ctx context.Context, c *client.Client, _ *mutation) ([]resolvedID, error) {
		return nil, requireLiveApp(ctx, c, appID, label)
	}})
}
