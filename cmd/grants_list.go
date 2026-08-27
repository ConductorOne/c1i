package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// grantListItem is one row of the SearchGrants response
// (AppEntitlementWithUserBinding): a grant view (account + timestamps + grant
// sources) plus the entitlement it binds to.
type grantListItem struct {
	AppEntitlementUserBinding struct {
		CreatedAt     string `json:"appEntitlementUserBindingCreatedAt"`
		DeprovisionAt string `json:"appEntitlementUserBindingDeprovisionAt"`
		AppUser       struct {
			AppUser struct {
				ID             string `json:"id"`
				AppID          string `json:"appId"`
				DisplayName    string `json:"displayName"`
				Email          string `json:"email"`
				Username       string `json:"username"`
				IdentityUserID string `json:"identityUserId"`
				AppUserType    string `json:"appUserType"`
				DeletedAt      string `json:"deletedAt"`
			} `json:"appUser"`
		} `json:"appUser"`
		// grantSources lists the groups/roles a grant is inherited through; an
		// empty list means the grant is direct.
		GrantSources []json.RawMessage `json:"grantSources"`
	} `json:"appEntitlementUserBinding"`
	Entitlement struct {
		AppEntitlement struct {
			ID          string `json:"id"`
			AppID       string `json:"appId"`
			DisplayName string `json:"displayName"`
			Slug        string `json:"slug"`
			DeletedAt   string `json:"deletedAt"`
		} `json:"appEntitlement"`
	} `json:"entitlement"`
}

// grantRow flattens a grant into the NDJSON output row.
func grantRow(item grantListItem) map[string]any {
	b := item.AppEntitlementUserBinding
	e := item.Entitlement.AppEntitlement
	au := b.AppUser.AppUser
	// The entitlement view carries the app id; fall back to the account's app id
	// if the entitlement wasn't expanded for some reason.
	appID := e.AppID
	if appID == "" {
		appID = au.AppID
	}
	return map[string]any{
		"app_id":                   appID,
		"entitlement_id":           e.ID,
		"entitlement_display_name": e.DisplayName,
		"entitlement_slug":         e.Slug,
		// A grant to a soft-deleted entitlement or account is still an active
		// binding server-side (only the entitlement/account itself, not this
		// grant, was deleted) — surface both so a stale grant is visible.
		"entitlement_deleted_at": nilIfEmpty(e.DeletedAt),
		"app_user_id":            au.ID,
		"app_user_display_name":  au.DisplayName,
		"app_user_deleted_at":    nilIfEmpty(au.DeletedAt),
		"email":                  au.Email,
		"username":               au.Username,
		"identity_user_id":       au.IdentityUserID,
		"app_user_type":          au.AppUserType,
		"created_at":             b.CreatedAt,
		"deprovision_at":         nilIfEmpty(b.DeprovisionAt),
		// 0 = direct grant; >0 = inherited through that many groups/roles.
		"grant_source_count": len(b.GrantSources),
	}
}

var grantsListCmd = &cobra.Command{
	Use:   "list",
	Short: "Search and list access grants (NDJSON output)",
	Long: `Search and list grants. At least one filter is required.

  # Who holds an entitlement?
  c1i grants list --app-id APP --entitlement-id ENT

  # What does a C1 identity have across apps?
  c1i grants list --user-id USER

  # Every grant in an app
  c1i grants list --app-id APP

Grant provisioning is asynchronous, so a read taken right after a grant or
revoke can catch the set mid-change. Pass --wait to poll instead: every 5s it
re-reads every page of the match, and once the same grants come back
--wait-stable times in a row it prints that settled set. Nothing reaches stdout
until it settles, unlike the default streaming output; progress goes to stderr,
so stdout stays pure NDJSON.

--wait settles on the WHOLE matching set, so it fetches every page on every
poll regardless of --limit; --limit only truncates what is printed at the end.
Filter narrowly. The poll interval is fixed at 5s -- --wait-stable and
--wait-timeout are the tunable parts.

--wait-stable defaults to 3 rather than 2 because two equal reads cannot be
told apart from a pause mid-change. Even 3 is a heuristic, not a proof: on a
set that keeps changing, --wait times out rather than printing anything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, _ := cmd.Flags().GetString("app-id")
		userID, _ := cmd.Flags().GetString("user-id")
		appUserID, _ := cmd.Flags().GetString("app-user-id")
		entitlementID, _ := cmd.Flags().GetString("entitlement-id")

		if appID == "" && userID == "" && appUserID == "" && entitlementID == "" {
			return &usageError{fmt.Errorf("at least one filter is required: --app-id, --user-id, --app-user-id, or --entitlement-id")}
		}
		if entitlementID != "" && appID == "" {
			return &usageError{fmt.Errorf("--entitlement-id requires --app-id (entitlements are scoped to an app)")}
		}

		wait, waitTimeout, err := waitFlagValues(cmd)
		if err != nil {
			return err
		}
		stableReads := getIntFlag(cmd, "wait-stable")
		if wait {
			if cmd.Flags().Changed("page-token") {
				// --page-token pins one page of a cursor the server is free to
				// re-issue as the set changes; "the same page twice" would not
				// mean the set settled.
				return &usageError{fmt.Errorf("--wait cannot be combined with --page-token; --wait re-reads every page")}
			}
			if stableReads < 2 {
				return &usageError{fmt.Errorf("--wait-stable must be at least 2 (one read cannot show that anything held steady)")}
			}
			// The first read is immediate, so n reads need (n-1) intervals.
			// Past that the wait cannot succeed, and the timeout would blame
			// slow provisioning for what is really bad arithmetic.
			if need := time.Duration(stableReads-1) * grantsWaitPollInterval; need >= waitTimeout {
				return &usageError{fmt.Errorf(
					"--wait-stable=%d needs %s at the fixed %s poll interval, which --wait-timeout=%s can never allow; raise --wait-timeout or lower --wait-stable",
					stableReads, need, grantsWaitPollInterval, waitTimeout)}
			}
		}

		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		c, err := newListClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		requestedPageSize := pageSizeFlag(cmd)
		pageToken, _ := cmd.Flags().GetString("page-token")
		manualPaging := cmd.Flags().Changed("page-token")
		limit := getIntFlag(cmd, "limit")

		q := grantsQuery{appID: appID, userID: userID, appUserID: appUserID, entitlementID: entitlementID}

		if wait {
			settled, err := waitForGrants(cmd, c, q, requestedPageSize, stableReads, waitTimeout)
			if err != nil {
				return err
			}
			enc := newEmitter(cmd)
			for _, item := range settled {
				_ = enc.Encode(grantRow(item))
				if limitReached(enc.Written(), limit) {
					break
				}
			}
			return nil
		}

		enc := newEmitter(cmd)
		for !limitReached(enc.Written(), limit) {
			pageSize := requestedPageSize
			if !enc.Filtered() {
				pageSize = effectivePageSize(requestedPageSize, limit, enc.Written())
			}
			body := q.searchBody(pageSize, pageToken)

			data, err := c.Post(cmd.Context(), "/api/v1/search/grants", body)
			if err != nil {
				return fmt.Errorf("API error: %w", err)
			}

			var resp struct {
				List          []grantListItem `json:"list"`
				NextPageToken string          `json:"nextPageToken"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			for _, item := range resp.List {
				_ = enc.Encode(grantRow(item))
				if limitReached(enc.Written(), limit) {
					return nil
				}
			}

			if resp.NextPageToken == "" || manualPaging {
				break
			}
			pageToken = resp.NextPageToken
		}

		return nil
	},
}

// grantsQuery is the filter set shared by the streaming and --wait paths, so
// both send byte-identical search bodies.
type grantsQuery struct {
	appID         string
	userID        string
	appUserID     string
	entitlementID string
}

func (q grantsQuery) searchBody(pageSize int, pageToken string) map[string]any {
	body := map[string]any{"pageSize": pageSize}
	if pageToken != "" {
		body["pageToken"] = pageToken
	}
	if q.appID != "" {
		body["appIds"] = []string{q.appID}
	}
	if q.userID != "" {
		body["userId"] = q.userID
	}
	if q.appUserID != "" {
		body["appUserIds"] = []string{q.appUserID}
	}
	if q.entitlementID != "" {
		body["entitlementRefs"] = []map[string]string{{"appId": q.appID, "id": q.entitlementID}}
	}
	return body
}

// grantsSnapshot is one full read of the matching grants. fingerprint is the
// comparable part runWait's Done predicate settles on; items is what gets
// printed once it does.
type grantsSnapshot struct {
	fingerprint string
	items       []grantListItem
}

// grantsWaitPollInterval is how often --wait re-reads. Each poll walks every
// page, so this is deliberately not tight. A var, not a const, so a test can
// drive the loop faster than real time.
var grantsWaitPollInterval = 5 * time.Second

// grantSetFingerprint identifies the set of grants independently of the page
// order the server happens to return them in: a grant is (entitlement,
// account), and a re-ordered page is not a change.
func grantSetFingerprint(items []grantListItem) string {
	keys := make([]string, 0, len(items))
	for _, it := range items {
		keys = append(keys, it.Entitlement.AppEntitlement.ID+"\x00"+it.AppEntitlementUserBinding.AppUser.AppUser.ID)
	}
	sort.Strings(keys)
	return strconv.Itoa(len(keys)) + "\x01" + strings.Join(keys, "\x02")
}

// fetchAllGrants pages POST /api/v1/search/grants to completion. --wait
// fingerprints the whole set, so a first-page-only read would call a set
// settled while later pages were still moving.
func fetchAllGrants(ctx context.Context, c *client.Client, q grantsQuery, pageSize int) ([]grantListItem, error) {
	var all []grantListItem
	pageToken, prevToken := "", ""
	for {
		data, err := c.Post(ctx, "/api/v1/search/grants", q.searchBody(pageSize, pageToken))
		if err != nil {
			return nil, fmt.Errorf("API error: %w", err)
		}
		var resp struct {
			List          []grantListItem `json:"list"`
			NextPageToken string          `json:"nextPageToken"`
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}
		all = append(all, resp.List...)
		if resp.NextPageToken == "" {
			return all, nil
		}
		// Same guard as "api --paginate" (cmd/api.go): a server that re-issues
		// one token forever would otherwise turn each poll into a request storm
		// bounded only by --wait-timeout, then blame provisioning for it.
		if resp.NextPageToken == prevToken {
			return nil, fmt.Errorf("API returned the same nextPageToken twice in a row while --wait was re-reading grants; the cursor is not advancing")
		}
		prevToken = resp.NextPageToken
		pageToken = resp.NextPageToken
	}
}

// waitForGrants blocks until the matching grant set comes back identical
// stableReads times running, and returns that settled set.
func waitForGrants(cmd *cobra.Command, c *client.Client, q grantsQuery, pageSize, stableReads int, timeout time.Duration) ([]grantListItem, error) {
	stable := untilStable[string](stableReads)
	settled, err := runWait(cmd, waitOp[grantsSnapshot]{
		Poll: func(ctx context.Context) (grantsSnapshot, error) {
			items, err := fetchAllGrants(ctx, c, q, pageSize)
			if err != nil {
				return grantsSnapshot{}, err
			}
			return grantsSnapshot{fingerprint: grantSetFingerprint(items), items: items}, nil
		},
		Done:     func(s grantsSnapshot) bool { return stable(s.fingerprint) },
		Interval: grantsWaitPollInterval,
		Timeout:  timeout,
		Subject:  "the matching grants to stop changing",
		Success:  "Grants settled",
		Slow:     "grant provisioning can take several minutes",
		Recheck:  "c1i grants list " + strings.Join(q.rescanFlags(), " "),
		// stdout is this command's NDJSON stream; progress prose there would
		// land mid-stream and break a caller's jq.
		Out: cmd.ErrOrStderr(),
	})
	if err != nil {
		return nil, err
	}
	return settled.items, nil
}

// rescanFlags reproduces the filters this query was built from, so the
// timeout message hands back a command that reruns the same search.
func (q grantsQuery) rescanFlags() []string {
	var out []string
	for _, f := range []struct{ name, value string }{
		{"--app-id", q.appID},
		{"--user-id", q.userID},
		{"--app-user-id", q.appUserID},
		{"--entitlement-id", q.entitlementID},
	} {
		if f.value != "" {
			out = append(out, f.name+"="+f.value)
		}
	}
	return out
}

func init() {
	grantsListCmd.Flags().String("app-id", "", "Filter to grants in this application")
	grantsListCmd.Flags().String("user-id", "", "Filter to grants held by this C1 identity user")
	grantsListCmd.Flags().String("app-user-id", "", "Filter to grants held by this app account (app user)")
	grantsListCmd.Flags().String("entitlement-id", "", "Filter to grants of this entitlement (requires --app-id)")
	addPaginationFlags(grantsListCmd)
	addWaitFlags(grantsListCmd, "every page until the same grants come back --wait-stable times running", 4*time.Minute)
	grantsListCmd.Flags().Int("wait-stable", 3, "Consecutive identical reads --wait requires before printing (min 2)")
	grantsCmd.AddCommand(grantsListCmd)
}
