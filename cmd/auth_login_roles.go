package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/config"
	"github.com/ConductorOne/c1i/internal/keychain"
	"github.com/ConductorOne/c1i/internal/login"
	"github.com/ConductorOne/c1i/internal/transport"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// helperDisplayName marks the short-lived credential the role menu reads with.
const helperDisplayName = "c1i login role lookup (temporary)"

// Built-in role names, stable across tenants.
const (
	roleSuperAdmin         = "system:owner"
	roleReadOnlySuperAdmin = "system:viewer"
	roleBasicUser          = "system:user"
)

// loginScope is how a browser login shapes its personal client.
type loginScope struct {
	choose      bool
	roles       []string
	displayName string
}

func (s loginScope) requested() bool {
	return s.choose || len(s.roles) > 0 || s.displayName != ""
}

func loginScopeFromFlags(cmd *cobra.Command) (loginScope, error) {
	var s loginScope
	s.choose, _ = cmd.Flags().GetBool("choose-roles")
	s.displayName, _ = cmd.Flags().GetString("display-name")
	roles, err := repeatableStringFlag(cmd, "scoped-role")
	if err != nil {
		return s, err
	}
	s.roles = roles
	if s.choose && len(s.roles) > 0 {
		return s, &usageError{fmt.Errorf("--choose-roles and --scoped-role are mutually exclusive")}
	}
	return s, nil
}

// storedClientID returns the client id stored for baseURL, or "" if none.
func storedClientID(baseURL string) string {
	return keychain.StoredClientID(config.KeychainService(baseURL))
}

// previousCredentialNote is printed after a login replaces a stored credential.
func previousCredentialNote(clientID string) string {
	note := fmt.Sprintf("c1i did not revoke the previous credential (%s)", clientID)
	if strings.HasSuffix(clientID, "/pcc") {
		note += "; revoke it under your personal clients in C1.ai if it is no longer needed"
	}
	return note + ".\n"
}

// scanLine reads one line, returning ctx.Err() once ctx ends (Ctrl-C) and
// io.EOF at end of input. A blocked read can't be interrupted, so on cancel
// its goroutine is abandoned to exit with the process.
func scanLine(ctx context.Context, s *bufio.Scanner) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	type result struct {
		line string
		ok   bool
	}
	ch := make(chan result, 1)
	go func() {
		ok := s.Scan()
		ch <- result{s.Text(), ok}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		if !r.ok {
			return "", io.EOF
		}
		return r.line, nil
	}
}

// askToChooseRoles asks, before the device code is shown, whether to scope the
// credential. Enter or EOF keeps the default: inherit all roles.
func askToChooseRoles(cmd *cobra.Command, in io.Reader) (bool, error) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Browser login creates a long-lived credential for c1i. What access should it have?\n")
	_, _ = fmt.Fprintf(out, "  1) All of your roles (default)\n")
	_, _ = fmt.Fprintf(out, "  2) Only roles I choose (a menu after you approve in the browser)\n")
	scanner := bufio.NewScanner(in)
	for {
		_, _ = fmt.Fprintf(out, "Choice [1]: ")
		line, err := scanLine(cmd.Context(), scanner)
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		switch strings.TrimSpace(line) {
		case "", "1":
			return false, nil
		case "2":
			return true, nil
		}
		_, _ = fmt.Fprintf(out, "Enter 1 or 2.\n")
	}
}

// menuRole is a role offered by the login menu.
type menuRole struct {
	roleListItem
	Held bool
}

// newHelperClient builds the client for the never-stored helper credential; a
// var so tests can stub it.
var newHelperClient = func(cmd *cobra.Command, baseURL, clientID, clientSecret string) (*client.Client, error) {
	return client.NewWithCredentials(cmd.Context(), baseURL, clientID, clientSecret,
		client.WithMaxRetries(viper.GetInt("max_retries")),
		client.WithDebug(viper.GetBool("debug")),
	)
}

// withHelper runs fn with a client for a temporary unscoped personal client.
// The device token may only create personal clients, so reading roles needs
// one; it is deleted when fn returns, even after Ctrl-C.
func withHelper(cmd *cobra.Command, baseURL, accessToken string, opts []transport.Option, fn func(*client.Client) error) error {
	helper, err := login.CreatePersonalClient(cmd.Context(), baseURL, accessToken, login.PersonalClientOptions{DisplayName: helperDisplayName}, opts...)
	if err != nil {
		return err
	}
	c, err := newHelperClient(cmd, baseURL, helper.ClientID, helper.ClientSecret)
	if err != nil {
		warnHelperLeft(cmd, helper.ID, err)
		return err
	}
	defer deleteHelper(cmd, c, helper.ID)
	return fn(c)
}

// chooseRoles runs the role menu.
func chooseRoles(cmd *cobra.Command, baseURL, accessToken string, opts []transport.Option) ([]menuRole, error) {
	var chosen []menuRole
	err := withHelper(cmd, baseURL, accessToken, opts, func(c *client.Client) error {
		roles, err := delegableRoles(cmd.Context(), c)
		if err != nil {
			return err
		}
		chosen, err = promptForRoles(cmd, os.Stdin, roles)
		return err
	})
	return chosen, err
}

// roleIDPattern is the server's role id format; any other --scoped-role value
// is a role name.
var roleIDPattern = regexp.MustCompile(`^[a-zA-Z0-9]{27}$`)

// resolveScopedRoles turns --scoped-role values into role ids. Ids alone pass
// through for the server to validate; a name needs the role catalog, which
// then checks the ids too.
func resolveScopedRoles(cmd *cobra.Command, baseURL, accessToken string, opts []transport.Option, values []string) ([]string, []menuRole, error) {
	named := false
	for _, v := range values {
		if !roleIDPattern.MatchString(v) {
			named = true
		}
	}
	if !named {
		return dedupe(values), nil, nil
	}

	var resolved []menuRole
	err := withHelper(cmd, baseURL, accessToken, opts, func(c *client.Client) error {
		catalog, err := roleCatalog(cmd.Context(), c)
		if err != nil {
			return err
		}
		resolved, err = matchRoles(values, catalog)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	ids := make([]string, len(resolved))
	for i, r := range resolved {
		ids[i] = r.ID
	}
	return ids, resolved, nil
}

func dedupe(values []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// normalizeRoleName lets "basic-user", "Basic User" and "basic_user" match.
func normalizeRoleName(s string) string {
	return strings.NewReplacer(" ", "-", "_", "-").Replace(strings.ToLower(strings.TrimSpace(s)))
}

// matchRoles resolves each value by exact id, then name (e.g. system:user),
// then display name, comparing names after normalizeRoleName. Only a tie
// within the first tier that matches is ambiguous.
func matchRoles(values []string, catalog []roleListItem) ([]menuRole, error) {
	var out []menuRole
	seen := map[string]bool{}
	for _, v := range values {
		n := normalizeRoleName(v)
		tiers := []func(roleListItem) bool{
			func(r roleListItem) bool { return r.ID == v },
			func(r roleListItem) bool { return normalizeRoleName(r.Name) == n },
			func(r roleListItem) bool { return normalizeRoleName(r.DisplayName) == n },
		}
		var hits []menuRole
		for _, match := range tiers {
			for _, r := range catalog {
				if match(r) {
					hits = append(hits, menuRole{roleListItem: r})
				}
			}
			if len(hits) > 0 {
				break
			}
		}
		switch len(hits) {
		case 0:
			all := make([]menuRole, len(catalog))
			for i, r := range catalog {
				all[i] = menuRole{roleListItem: r}
			}
			labels := roleLabels(all)
			sort.Strings(labels)
			return nil, &usageError{fmt.Errorf("--scoped-role %q matches no role; roles: %s", v, strings.Join(labels, "; "))}
		case 1:
		default:
			labels := make([]string, len(hits))
			for i, r := range hits {
				labels[i] = r.DisplayName + " [" + r.ID + "]"
			}
			return nil, &usageError{fmt.Errorf("--scoped-role %q matches several roles; pass one id: %s", v, strings.Join(labels, "; "))}
		}
		if !seen[hits[0].ID] {
			seen[hits[0].ID] = true
			out = append(out, hits[0])
		}
	}
	return out, nil
}

// deleteHelper removes the helper credential, even after Ctrl-C.
func deleteHelper(cmd *cobra.Command, c *client.Client, id string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(cmd.Context()), 30*time.Second)
	defer cancel()
	if _, err := c.Delete(ctx, client.Path("/api/v1/iam/personal_clients/%s", id)); err != nil {
		warnHelperLeft(cmd, id, err)
	}
}

func warnHelperLeft(cmd *cobra.Command, id string, err error) {
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not delete the temporary credential %q (%s); it has all of your roles, so delete it under your personal clients in C1.ai: %v\n", helperDisplayName, id, err)
}

// delegableRoles returns the roles the menu offers, sorted by display name.
func delegableRoles(ctx context.Context, c *client.Client) ([]menuRole, error) {
	data, err := c.Get(ctx, "/api/v1/auth/introspect", nil)
	if err != nil {
		return nil, err
	}
	var me struct {
		UserID string `json:"userId"`
	}
	if err := json.Unmarshal(data, &me); err != nil {
		return nil, &nonJSONResponseError{fmt.Errorf("parsing introspect response: %w", err)}
	}

	data, err = c.Get(ctx, client.Path("/api/v1/users/%s", me.UserID), nil)
	if err != nil {
		return nil, err
	}
	var user struct {
		UserView struct {
			User struct {
				RoleIDs []string `json:"roleIds"`
			} `json:"user"`
		} `json:"userView"`
	}
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, &nonJSONResponseError{fmt.Errorf("parsing user response: %w", err)}
	}
	held := map[string]bool{}
	for _, id := range user.UserView.User.RoleIDs {
		held[id] = true
	}

	catalog, err := roleCatalog(ctx, c)
	if err != nil {
		return nil, err
	}
	return filterDelegable(catalog, held), nil
}

// roleCatalog reads every role in the tenant.
func roleCatalog(ctx context.Context, c *client.Client) ([]roleListItem, error) {
	var catalog []roleListItem
	params := map[string]string{}
	for {
		data, err := c.Get(ctx, "/api/v1/iam/roles", params)
		if err != nil {
			return nil, err
		}
		var page struct {
			List          []roleListItem `json:"list"`
			NextPageToken string         `json:"nextPageToken"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, &nonJSONResponseError{fmt.Errorf("parsing roles response: %w", err)}
		}
		catalog = append(catalog, page.List...)
		if page.NextPageToken == "" {
			break
		}
		params["page_token"] = page.NextPageToken
	}
	return catalog, nil
}

// filterDelegable combines C1.ai's two web pickers: the personal-client page
// offers API-only roles, Basic User and Read-Only Administrator; the MCP
// consent page offers the roles you hold, or every role to an administrator.
func filterDelegable(catalog []roleListItem, held map[string]bool) []menuRole {
	admin := false
	for _, r := range catalog {
		if held[r.ID] && (r.Name == roleSuperAdmin || r.Name == roleReadOnlySuperAdmin) {
			admin = true
		}
	}
	var out []menuRole
	for _, r := range catalog {
		if admin || held[r.ID] || r.SystemAPIOnly || r.Name == roleBasicUser || r.Name == roleReadOnlySuperAdmin {
			out = append(out, menuRole{roleListItem: r, Held: held[r.ID]})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DisplayName != out[j].DisplayName {
			return out[i].DisplayName < out[j].DisplayName
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// roleLabels names roles for display, adding the id where display names collide.
func roleLabels(roles []menuRole) []string {
	count := map[string]int{}
	for _, r := range roles {
		count[r.DisplayName]++
	}
	labels := make([]string, len(roles))
	for i, r := range roles {
		labels[i] = r.DisplayName
		if count[r.DisplayName] > 1 {
			labels[i] += " [" + r.ID + "]"
		}
	}
	return labels
}

// promptForRoles shows roles as a numbered menu and reads a selection. A nil
// result means full permissions; a blank answer re-prompts, so a stray Enter
// can't silently widen the credential.
func promptForRoles(cmd *cobra.Command, in io.Reader, roles []menuRole) ([]menuRole, error) {
	out := cmd.OutOrStdout()
	labels := roleLabels(roles)
	_, _ = fmt.Fprintf(out, "\nChoose the roles this credential may use (* = a role you hold):\n")
	_, _ = fmt.Fprintf(out, "  %2d) Full permissions (all of your roles)\n", 0)
	for i, r := range roles {
		label := labels[i]
		if r.Held {
			label += " *"
		}
		_, _ = fmt.Fprintf(out, "  %2d) %s\n", i+1, label)
	}

	scanner := bufio.NewScanner(in)
	for {
		_, _ = fmt.Fprintf(out, "Enter one or more numbers (e.g. 1,3): ")
		line, err := scanLine(cmd.Context(), scanner)
		if errors.Is(err, io.EOF) {
			return nil, &usageError{fmt.Errorf("no role selection read (stdin closed); re-run, or pass --scoped-role <role>")}
		}
		if err != nil {
			return nil, err
		}
		chosen, err := parseRoleSelection(line, roles)
		if err == nil {
			return chosen, nil
		}
		_, _ = fmt.Fprintf(out, "%v\n", err)
	}
}

func parseRoleSelection(answer string, roles []menuRole) ([]menuRole, error) {
	fields := strings.FieldsFunc(answer, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	if len(fields) == 0 {
		return nil, fmt.Errorf("enter at least one number; 0 keeps full permissions")
	}
	var out []menuRole
	seen := map[int]bool{}
	full := false
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 || n > len(roles) {
			return nil, fmt.Errorf("invalid choice %q: enter numbers from 0 to %d", f, len(roles))
		}
		if n == 0 {
			full = true
			continue
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, roles[n-1])
		}
	}
	if full && len(out) > 0 {
		return nil, fmt.Errorf("0 (full permissions) can't be combined with other roles")
	}
	return out, nil
}

// reportScope states what the new credential may do, whichever way it was set.
func reportScope(cmd *cobra.Command, scopedIDs []string, chosen []menuRole) {
	out := cmd.OutOrStdout()
	switch {
	case len(scopedIDs) == 0:
		_, _ = fmt.Fprintf(out, "Credential inherits all of your roles.\n")
	case len(chosen) > 0:
		// Ids always: duplicate display names can't be told apart within a subset.
		labels := make([]string, len(chosen))
		for i, r := range chosen {
			labels[i] = r.DisplayName + " [" + r.ID + "]"
		}
		_, _ = fmt.Fprintf(out, "Credential scoped to: %s\n", strings.Join(labels, "; "))
	default:
		_, _ = fmt.Fprintf(out, "Credential scoped to role ids: %s\n", strings.Join(scopedIDs, "; "))
	}
}
