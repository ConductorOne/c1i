package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/config"
	"github.com/ConductorOne/c1i/internal/keychain"
	"github.com/ConductorOne/c1i/internal/login"
	"github.com/ConductorOne/c1i/internal/transport"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	// helperDisplayName marks the short-lived credential that reads roles.
	helperDisplayName = "c1i login role lookup (temporary)"
	// helperLifetime caps a helper the delete never reaches (e.g. kill -9).
	helperLifetime = "600s"
)

// Built-in role names, stable across tenants.
const (
	roleSuperAdmin         = "system:owner"
	roleReadOnlySuperAdmin = "system:viewer"
	roleBasicUser          = "system:user"
)

// loginScope is how a browser login shapes its personal client.
type loginScope struct {
	choose bool
	// noPrompt is an explicit --choose-roles=false: never ask.
	noPrompt bool
	roles    []string
}

func (s loginScope) requested() bool {
	return s.choose || s.noPrompt || len(s.roles) > 0
}

func loginScopeFromFlags(cmd *cobra.Command) (loginScope, error) {
	var s loginScope
	s.choose, _ = cmd.Flags().GetBool("choose-roles")
	s.noPrompt = cmd.Flags().Changed("choose-roles") && !s.choose
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
	if id := keychain.StoredClientID(config.KeychainService(baseURL)); id != "" {
		return id
	}
	if legacy := config.LegacyKeychainService(baseURL); legacy != "" {
		return keychain.StoredClientID(legacy)
	}
	return ""
}

// lineReader reads stdin lines on a single goroutine, so a prompt abandoned
// on Ctrl-C can't race the next one for input; an unread line is simply
// handed to the next prompt.
type lineReader struct {
	scanner *bufio.Scanner
	start   sync.Once
	lines   chan string
}

func newLineReader(r io.Reader) *lineReader {
	return &lineReader{scanner: bufio.NewScanner(r), lines: make(chan string)}
}

// readLine returns the next line, ctx.Err() once ctx ends (Ctrl-C), or io.EOF
// at end of input.
func (l *lineReader) readLine(ctx context.Context) (string, error) {
	l.start.Do(func() {
		go func() {
			for l.scanner.Scan() {
				l.lines <- l.scanner.Text()
			}
			close(l.lines)
		}()
	})
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case line, ok := <-l.lines:
		if !ok {
			return "", io.EOF
		}
		return line, nil
	}
}

// askToChooseRoles asks, before the device code is shown, whether to scope the
// credential. Enter or EOF keeps all roles.
func askToChooseRoles(cmd *cobra.Command, in *lineReader) (bool, error) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Browser login creates a long-lived credential for c1i. What access should it have?\n")
	_, _ = fmt.Fprintf(out, "  1) All of your roles (default)\n")
	_, _ = fmt.Fprintf(out, "  2) Only roles I choose (a menu after you approve in the browser)\n")
	for {
		_, _ = fmt.Fprintf(out, "Choice [1]: ")
		line, err := in.readLine(cmd.Context())
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

// roleListItem is the subset of a Role the login needs.
type roleListItem struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	DisplayName   string `json:"displayName"`
	SystemAPIOnly bool   `json:"systemApiOnly"`
}

// menuRole is a role a login may scope to.
type menuRole struct {
	roleListItem
	Held bool
}

// roleLookup is what a login reads about roles: the ones it may scope to, and
// the whole catalog, to explain a role outside that set.
type roleLookup struct {
	offered []menuRole
	catalog []roleListItem
}

// newCredentialClient builds a client for credentials that aren't stored (the
// helper, or a new credential before it is stored); a var so tests can stub it.
var newCredentialClient = func(cmd *cobra.Command, baseURL, clientID, clientSecret string) (*client.Client, error) {
	return client.NewWithCredentials(cmd.Context(), baseURL, clientID, clientSecret,
		client.WithMaxRetries(viper.GetInt("max_retries")),
		client.WithDebug(viper.GetBool("debug")),
	)
}

// helperLeftError reports a helper credential the login could not delete. The
// login itself succeeded; exiting non-zero keeps a script from missing it.
type helperLeftError struct {
	id  string
	err error
}

func (e *helperLeftError) Error() string {
	return fmt.Sprintf("the temporary credential %q (%s) was not deleted: %v. It has all of your roles until it expires in 10 minutes; delete it sooner under your personal clients in C1.ai", helperDisplayName, e.id, e.err)
}

func (e *helperLeftError) Unwrap() error { return e.err }

// lookupRoles reads the roles a login may scope to. The device token may only
// create personal clients, so a temporary unscoped one does the reading and is
// deleted before this returns, even after Ctrl-C. A failed delete comes back
// as leftover, separate from err, so the login can still finish.
func lookupRoles(cmd *cobra.Command, baseURL, accessToken string, opts []transport.Option) (lookup roleLookup, leftover, err error) {
	ctx := cmd.Context()
	// Detached from Ctrl-C: an interrupted create could commit a helper whose id
	// we never learn.
	createCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	helper, err := login.CreatePersonalClient(createCtx, baseURL, accessToken, login.PersonalClientOptions{DisplayName: helperDisplayName, Expires: helperLifetime}, opts...)
	if err != nil {
		return lookup, nil, err
	}
	c, err := newCredentialClient(cmd, baseURL, helper.ClientID, helper.ClientSecret)
	if err != nil {
		leftover = &helperLeftError{helper.ID, err}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %v\n", leftover)
		return lookup, leftover, err
	}
	if err = ctx.Err(); err == nil {
		lookup, err = readRoles(ctx, c)
	}
	if delErr := deletePersonalClient(ctx, c, helper.ID); delErr != nil {
		leftover = &helperLeftError{helper.ID, delErr}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %v\n", leftover)
	}
	return lookup, leftover, err
}

// deletePersonalClient deletes a personal client even after Ctrl-C.
func deletePersonalClient(ctx context.Context, c *client.Client, id string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	_, err := c.Delete(ctx, client.Path("/api/v1/iam/personal_clients/%s", id))
	return err
}

func readRoles(ctx context.Context, c *client.Client) (roleLookup, error) {
	userID, err := currentUserID(ctx, c)
	if err != nil {
		return roleLookup{}, err
	}
	data, err := c.Get(ctx, client.Path("/api/v1/users/%s", userID), nil)
	if err != nil {
		return roleLookup{}, err
	}
	var user struct {
		UserView struct {
			User struct {
				RoleIDs []string `json:"roleIds"`
			} `json:"user"`
		} `json:"userView"`
	}
	if err := json.Unmarshal(data, &user); err != nil {
		return roleLookup{}, &nonJSONResponseError{fmt.Errorf("parsing user response: %w", err)}
	}
	held := map[string]bool{}
	for _, id := range user.UserView.User.RoleIDs {
		held[id] = true
	}

	var catalog []roleListItem
	params := map[string]string{"page_size": "100"}
	for {
		data, err := c.Get(ctx, "/api/v1/iam/roles", params)
		if err != nil {
			return roleLookup{}, err
		}
		var page struct {
			List          []roleListItem `json:"list"`
			NextPageToken string         `json:"nextPageToken"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return roleLookup{}, &nonJSONResponseError{fmt.Errorf("parsing roles response: %w", err)}
		}
		catalog = append(catalog, page.List...)
		if page.NextPageToken == "" {
			break
		}
		params["page_token"] = page.NextPageToken
	}
	return roleLookup{offered: filterDelegable(catalog, held), catalog: catalog}, nil
}

// filterDelegable returns the roles worth scoping to, sorted by display name.
// A scoped credential keeps only the overlap between its roles and its owner's
// own access, so an unheld role adds little or nothing. Offered: roles you
// hold, Basic User, and Read-Only Administrator (a read-only copy of your
// access). Administrators hold every permission, so they see every role.
func filterDelegable(catalog []roleListItem, held map[string]bool) []menuRole {
	admin := false
	for _, r := range catalog {
		if held[r.ID] && (r.Name == roleSuperAdmin || r.Name == roleReadOnlySuperAdmin) {
			admin = true
		}
	}
	var out []menuRole
	for _, r := range catalog {
		if admin || held[r.ID] || r.Name == roleBasicUser || r.Name == roleReadOnlySuperAdmin {
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

// roleIDPattern is the server's role id format; any other --scoped-role value
// is a role name.
var roleIDPattern = regexp.MustCompile(`^[a-zA-Z0-9]{27}$`)

// needsLookup reports whether any --scoped-role value is a name. Ids alone go
// to the server unchecked, so no helper credential is created.
func needsLookup(values []string) bool {
	for _, v := range values {
		if !roleIDPattern.MatchString(v) {
			return true
		}
	}
	return false
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

var roleNameSeparators = strings.NewReplacer(" ", "-", "_", "-")

// normalizeRoleName lets "basic-user", "Basic User" and "basic_user" match.
func normalizeRoleName(s string) string {
	return roleNameSeparators.Replace(strings.ToLower(strings.TrimSpace(s)))
}

// matchRoles resolves each value against the offered roles by id, name (e.g.
// system:user) or display name, comparing names after normalizeRoleName. More
// than one match is ambiguous rather than guessed.
func matchRoles(values []string, lookup roleLookup) ([]menuRole, error) {
	var out []menuRole
	seen := map[string]bool{}
	for _, v := range values {
		n := normalizeRoleName(v)
		matches := func(r roleListItem) bool {
			return r.ID == v || normalizeRoleName(r.Name) == n || normalizeRoleName(r.DisplayName) == n
		}
		var hits []menuRole
		for _, r := range lookup.offered {
			if matches(r.roleListItem) {
				hits = append(hits, r)
			}
		}
		switch len(hits) {
		case 1:
		case 0:
			for _, r := range lookup.catalog {
				if matches(r) {
					return nil, &usageError{fmt.Errorf("--scoped-role %q: you don't hold %s, and a credential scoped to it gets only the overlap with your own access; choose from: %s", v, roleLabel(r), offeredLabels(lookup.offered))}
				}
			}
			return nil, &usageError{fmt.Errorf("--scoped-role %q matches no role; choose from: %s", v, offeredLabels(lookup.offered))}
		default:
			labels := make([]string, len(hits))
			for i, r := range hits {
				labels[i] = roleLabel(r.roleListItem)
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

// printable strips control characters from server-supplied text, so a role
// name can't fake menu lines or send terminal escape sequences.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// roleLabel names a role unambiguously: display name and id.
func roleLabel(r roleListItem) string {
	return printable(r.DisplayName) + " [" + r.ID + "]"
}

func offeredLabels(roles []menuRole) string {
	labels := make([]string, len(roles))
	for i, r := range roles {
		labels[i] = printable(r.DisplayName)
	}
	return strings.Join(labels, "; ")
}

// promptForRoles shows roles as a numbered menu and reads a selection. A nil
// result means all roles; a blank answer re-prompts, so a stray Enter can't
// silently widen the credential.
func promptForRoles(cmd *cobra.Command, in *lineReader, roles []menuRole) ([]menuRole, error) {
	out := cmd.OutOrStdout()
	count := map[string]int{}
	for _, r := range roles {
		count[r.DisplayName]++
	}
	_, _ = fmt.Fprintf(out, "\nChoose the roles this credential may use (* = a role you hold):\n")
	_, _ = fmt.Fprintf(out, "  %2d) All of your roles\n", 0)
	for i, r := range roles {
		label := printable(r.DisplayName)
		if count[r.DisplayName] > 1 {
			label = roleLabel(r.roleListItem)
		}
		if r.Held {
			label += " *"
		}
		_, _ = fmt.Fprintf(out, "  %2d) %s\n", i+1, label)
	}

	for {
		_, _ = fmt.Fprintf(out, "Enter one or more numbers (e.g. 1,3): ")
		line, err := in.readLine(cmd.Context())
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
		return nil, fmt.Errorf("enter at least one number; 0 keeps all of your roles")
	}
	var out []menuRole
	seen := map[int]bool{}
	all := false
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 || n > len(roles) {
			return nil, fmt.Errorf("invalid choice %q: enter numbers from 0 to %d", f, len(roles))
		}
		if n == 0 {
			all = true
			continue
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, roles[n-1])
		}
	}
	if all && len(out) > 0 {
		return nil, fmt.Errorf("0 (all of your roles) can't be combined with other roles")
	}
	return out, nil
}

// credentialName names the new credential after its scope, so several c1i
// logins can be told apart in C1.ai.
func credentialName(ids []string, chosen []menuRole) string {
	switch {
	case len(ids) == 0:
		return login.DefaultDisplayName
	case len(chosen) == 0:
		return login.DefaultDisplayName + " (scoped)"
	}
	names := make([]string, len(chosen))
	for i, r := range chosen {
		names[i] = printable(r.DisplayName)
	}
	name := login.DefaultDisplayName + " (" + strings.Join(names, ", ") + ")"
	if len(name) > 200 {
		name = fmt.Sprintf("%s (%d roles)", login.DefaultDisplayName, len(chosen))
	}
	return name
}

// reportScope states the new credential's scope as the server returned it.
func reportScope(cmd *cobra.Command, granted []string, chosen []menuRole) {
	out := cmd.OutOrStdout()
	if len(granted) == 0 {
		_, _ = fmt.Fprintf(out, "Credential has all of your roles.\n")
		return
	}
	byID := map[string]roleListItem{}
	for _, r := range chosen {
		byID[r.ID] = r.roleListItem
	}
	labels := make([]string, len(granted))
	for i, id := range granted {
		if r, ok := byID[id]; ok {
			labels[i] = roleLabel(r)
		} else {
			labels[i] = id
		}
	}
	_, _ = fmt.Fprintf(out, "Credential scoped to: %s\n", strings.Join(labels, "; "))
}

// sameRoles reports whether the server granted exactly the requested roles.
func sameRoles(requested, granted []string) bool {
	a, b := dedupe(requested), dedupe(granted)
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, id := range a {
		set[id] = true
	}
	for _, id := range b {
		if !set[id] {
			return false
		}
	}
	return true
}

// authRolePrefix marks the session-only service roles (introspect, ping) a
// credential keeps even when its scope leaves it nothing else.
const authRolePrefix = "role/c1.api.auth.v1.Auth:"

// hasNoAccess reports whether a scoped credential's introspect shows it can do
// nothing: a scope with no overlap loses introspect itself (403), and a nearly
// empty one keeps only the Auth service.
func hasNoAccess(introspect []byte, err error) bool {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusForbidden
	}
	if err != nil {
		return false
	}
	var resp struct {
		Roles []string `json:"roles"`
	}
	if json.Unmarshal(introspect, &resp) != nil {
		return false
	}
	for _, r := range resp.Roles {
		if !strings.HasPrefix(r, authRolePrefix) {
			return false
		}
	}
	return true
}
