package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var authWhoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show the authenticated principal and the tenant being targeted",
	Long: `Calls /api/v1/auth/introspect and returns a compact summary of the
authenticated principal: userId, principleId, and counts of roles,
permissions, and feature flags.

Two client-resolved keys are added to that summary, and to --verbose:
"tenant" is the base URL this invocation resolved, and "tenantSource" is
where it came from ("flag", "env", or "config"). This is the machine-readable
form of the tenant "auth status" prints as text — check it before a write:

    c1i auth whoami --url https://mycompany.conductor.one --fields tenant

Both keys report where a request WOULD go; they are only emitted once the
credentials are proven against that tenant, so an auth failure exits nonzero
with no tenant rather than reporting an unusable target. "tenant" always
means the client-resolved URL, including under --verbose: if the introspect
payload ever carries a key of that name, this one wins, so the check reads
the same in either mode. The payload's own "tenantId" is untouched.

The full introspect payload can include hundreds of roles and over a
thousand permissions — pass --verbose to dump it all.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, urlSource, err := requireBaseURL()
		if err != nil {
			return err
		}
		c, err := newWhoamiClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("not authenticated: %w", err)
		}
		body, err := c.Get(cmd.Context(), "/api/v1/auth/introspect", nil)
		if err != nil {
			return err
		}

		verbose, _ := cmd.Flags().GetBool("verbose")
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			return fmt.Errorf("parsing introspect response: %w", err)
		}
		// A `null` body unmarshals into a nil map with no error: a 200 carrying
		// no identity at all, which must not read as a confirmed tenant.
		if payload == nil {
			return &nonJSONResponseError{fmt.Errorf("introspect returned a JSON null body")}
		}

		// Best-effort: enrich with display_name + email from /api/v1/users/{id}.
		// If that call fails (permissions, network), we still return the
		// introspect summary — identity from introspect alone is the contract.
		var displayName, email string
		if uid, ok := payload["userId"].(string); ok && uid != "" {
			if userBody, uerr := c.Get(cmd.Context(), client.Path("/api/v1/users/%s", uid), nil); uerr == nil {
				var u struct {
					UserView struct {
						User struct {
							DisplayName string `json:"displayName"`
							Email       string `json:"email"`
						} `json:"user"`
					} `json:"userView"`
				}
				if json.Unmarshal(userBody, &u) == nil {
					displayName = u.UserView.User.DisplayName
					email = u.UserView.User.Email
				}
			}
		}

		obj := summarize(payload, displayName, email)
		if verbose {
			obj = payload
		}
		// Always the client-resolved values, never the payload's: the question
		// is which host this invocation would write to.
		obj["tenant"] = baseURL
		obj["tenantSource"] = urlSourceToken(urlSource)
		out, err := json.Marshal(obj)
		if err != nil {
			return err
		}
		// Route through writeObject so --fields works (e.g. whoami --fields email).
		return writeObject(cmd, out)
	},
}

func summarize(p map[string]any, displayName, email string) map[string]any {
	out := map[string]any{
		"userId":      p["userId"],
		"principleId": p["principleId"],
		"counts": map[string]int{
			"roles":       sliceLen(p["roles"]),
			"permissions": sliceLen(p["permissions"]),
			"features":    sliceLen(p["features"]),
		},
	}
	if displayName != "" {
		out["displayName"] = displayName
	}
	if email != "" {
		out["email"] = email
	}
	return out
}

// newWhoamiClient is a var, not a direct newClient call, so a test can
// substitute an httptest-backed client — mirroring newListClient (cmd/client.go)
// and newAPIClient (cmd/api.go).
var newWhoamiClient = newClient

func sliceLen(v any) int {
	if s, ok := v.([]any); ok {
		return len(s)
	}
	return 0
}

func init() {
	authWhoamiCmd.Flags().BoolP("verbose", "v", false, "Include full roles, permissions, and features arrays")
	authCmd.AddCommand(authWhoamiCmd)
}
