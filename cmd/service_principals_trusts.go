package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// trusts are workload-federation trust policies: per-SP rules that let an
// external workload's JWT, from a "workload-federation providers" issuer,
// be exchanged for a token acting as the service principal.

var spTrustsCmd = &cobra.Command{
	Use:   "trusts",
	Short: "Manage workload federation trusts (external workloads acting as a service principal)",
	Long: `Manage workload federation trusts.

A trust lets a workload holding a JWT from a registered issuer ("c1i
workload-federation providers") act as the service principal, when the
token's claims satisfy the trust's CEL condition. The API documents the
effective permissions as "min(SP roles, trust.scoped_role_ids)".

A trust is addressed by its client id, as list and search print it
(e.g. "word-word-12345@tenant.example.com/wfe"). <trust-id> takes either that
full id or the part before "@"; c1i sends only the part before "@", the only
form the API routes, and the rest is not checked. The trust is still looked up
under --service-principal-id.

  service-principals trusts list <sp-id>
  service-principals trusts create <sp-id> --provider-id <provider-id> \
    --display-name ci --condition 'claims.sub == "repo:acme/infra:ref:refs/heads/main"'
  service-principals trusts test-token <trust-id> --service-principal-id <sp-id> --subject-token-file jwt.txt`,
}

func spTrustsPath(spID string) string {
	return client.Path("/api/v1/service_principals/%s/trusts", spID)
}

// spTrustPath builds one trust's path. The API routes a trust by the part of
// its client id before "@"; the full id 404s in every encoding.
func spTrustPath(spID, trustID string) string {
	local, _, _ := strings.Cut(trustID, "@")
	return client.Path("/api/v1/service_principals/%s/trusts/%s", spID, local)
}

const spTrustsSearchPath = "/api/v1/search/workload_federation_trusts"

// wfTrust is the subset of WorkloadFederationTrust surfaced in list rows.
type wfTrust struct {
	ClientID            string   `json:"clientId"`
	ServicePrincipalID  string   `json:"servicePrincipalId"`
	ProviderID          string   `json:"providerId"`
	DisplayName         string   `json:"displayName"`
	Description         string   `json:"description"`
	ConditionExpression string   `json:"conditionExpression"`
	Disabled            bool     `json:"disabled"`
	ScopedRoleIDs       []string `json:"scopedRoleIds"`
	AllowSourceCIDRs    []string `json:"allowSourceCidrs"`
	PassthroughClaims   []string `json:"passthroughClaims"`
	CreatedAt           string   `json:"createdAt"`
	UpdatedAt           string   `json:"updatedAt"`
}

func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func wfTrustRow(t wfTrust) map[string]any {
	return map[string]any{
		"client_id":            nilIfEmpty(t.ClientID),
		"service_principal_id": nilIfEmpty(t.ServicePrincipalID),
		"provider_id":          nilIfEmpty(t.ProviderID),
		"display_name":         nilIfEmpty(t.DisplayName),
		"description":          nilIfEmpty(t.Description),
		"condition_expression": nilIfEmpty(t.ConditionExpression),
		"disabled":             t.Disabled,
		"scoped_role_ids":      emptyIfNil(t.ScopedRoleIDs),
		"allow_source_cidrs":   emptyIfNil(t.AllowSourceCIDRs),
		"passthrough_claims":   emptyIfNil(t.PassthroughClaims),
		"created_at":           nilIfEmpty(t.CreatedAt),
		"updated_at":           nilIfEmpty(t.UpdatedAt),
	}
}

func wfTrustPageSpec(route string, fetch pageFetch) pageSpec[wfTrust] {
	return pageSpec[wfTrust]{
		Route: route,
		Fetch: fetch,
		Parse: parseList[wfTrust],
		ID:    func(t wfTrust) string { return t.ClientID },
		Rows:  rowFn(wfTrustRow),
	}
}

const wfDeletedTrustNote = `A deleted trust leaves list and search at once, but "trusts get" still
returns it, with no field marking it deleted. Deleting the service principal
deletes its trusts the same way.`

var spTrustsListCmd = &cobra.Command{
	Use:   "list <sp-id>",
	Short: "List a service principal's workload federation trusts (NDJSON output)",
	Long: `List a service principal's workload federation trusts (NDJSON output).

An unknown <sp-id> lists nothing and exits 0: the API answers it with an
empty list. ` + wfDeletedTrustNote,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		path := spTrustsPath(args[0])
		return listPages(cmd, c, wfTrustPageSpec("GET "+path, getPage(cmd.Context(), path, nil)))
	},
}

var spTrustsSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search workload federation trusts across service principals (NDJSON output)",
	Long: `Search workload federation trusts across all service principals (NDJSON
output). Filters combine; with none, every trust is listed.

The API documents --query as full-text search on the trust's display name
and description. An unknown
--service-principal-id or --provider-id matches nothing and exits 0: the API
answers it with an empty list.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		base := map[string]any{}
		for flag, key := range map[string]string{"service-principal-id": "servicePrincipalId", "provider-id": "providerId", "query": "query"} {
			if flagGiven(cmd, flag) {
				v, _ := cmd.Flags().GetString(flag)
				base[key] = v
			}
		}
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		return listPages(cmd, c, wfTrustPageSpec("POST "+spTrustsSearchPath, postPage(cmd.Context(), spTrustsSearchPath, base)))
	},
}

var spTrustsGetCmd = &cobra.Command{
	Use:   "get <trust-id>",
	Short: "Get a single workload federation trust (pretty JSON)",
	Long: `Get a single workload federation trust (pretty JSON).

<trust-id> is the trust's client id or the part before "@". ` + wfDeletedTrustNote,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		spID, _ := cmd.Flags().GetString("service-principal-id")
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		c, err := newServicePrincipalsClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Get(cmd.Context(), spTrustPath(spID, args[0]), nil)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		return writeNamedResource(cmd, data, "trust")
	},
}

// servicePrincipalRefKind checks a trust's owner: the trust routes accept any
// id, and an unknown one stores an orphan trust.
var servicePrincipalRefKind = refKind{"service principal", func(r appRef) string { return spPath(r.ID) }}

var roleRefKind = refKind{"role", func(r appRef) string { return client.Path("/api/v1/iam/roles/%s", r.ID) }}

// scopedRoleRefs labels each --scoped-role for existingRefsCheck: the API
// stores an unknown role id as is.
func scopedRoleRefs(ids []string) []labeledRef {
	out := make([]labeledRef, len(ids))
	for i, id := range ids {
		out[i] = labeledRef{appRef{ID: id}, fmt.Sprintf("--scoped-role %q", id)}
	}
	return out
}

const wfTrustRefHelp = `Looks up <sp-id> and each --scoped-role first, since the API stores a trust
for an unknown id: an unknown one exits 4 before anything is sent, --dry-run
included. The API answers a human user's id with a 500 ("user ... is not a
service principal", exit 6).`

var spTrustsCreateCmd = &cobra.Command{
	Use:   "create <sp-id>",
	Short: "Create a workload federation trust on a service principal",
	Long: `Create a workload federation trust on a service principal.

--condition is a CEL expression over the token's claims ("claims.sub",
"claims.repository", ...) that must return a bool; the API compiles it and
refuses an invalid one (exit 2). "c1i workload-federation test-cel" evaluates
one against sample claims first. A new trust is enabled.

` + wfTrustRefHelp + ` An unknown --provider-id exits 4 (the
API's answer).

Prints the trust as pretty JSON, unwrapped like "trusts get"; its client id
is .clientId.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		f := cmd.Flags()
		body := map[string]any{}
		for flag, key := range map[string]string{"provider-id": "providerId", "display-name": "displayName", "condition": "conditionExpression", "description": "description"} {
			if flagGiven(cmd, flag) {
				v, _ := f.GetString(flag)
				body[key] = v
			}
		}
		lists := map[string]string{"scoped-role": "scopedRoleIds", "allow-cidr": "allowSourceCidrs", "passthrough-claim": "passthroughClaims"}
		for flag, key := range lists {
			vs, err := repeatableStringFlag(cmd, flag)
			if err != nil {
				return err
			}
			if len(vs) > 0 {
				body[key] = vs
			}
		}
		roles, _ := body["scopedRoleIds"].([]string)
		spID := args[0]
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: spTrustsPath(spID), Body: body, Unwrap: named("trust")},
			withPreRead(existingRefsCheck(servicePrincipalRefKind, []labeledRef{{appRef{ID: spID}, fmt.Sprintf("<sp-id> %q", spID)}})),
			withPreRead(existingRefsCheck(roleRefKind, scopedRoleRefs(roles))))
	},
}

var spTrustsUpdateSpec = &updateSpec{
	fields: []updateField{
		strUpdate("display-name", "displayName"),
		strUpdate("description", "description"),
		strUpdate("condition", "conditionExpression"),
		boolUpdate("disabled", "disabled"),
		listUpdate("scoped-role", "scopedRoleIds", nil),
		listUpdate("allow-cidr", "allowSourceCidrs", nil),
		listUpdate("passthrough-claim", "passthroughClaims", nil),
	},
	alt:   "--body-file with --update-mask",
	scope: []string{"service-principal-id", "body-file", "update-mask"},
}

var spTrustsUpdateCmd = &cobra.Command{
	Use:   "update <trust-id>",
	Short: "Update a workload federation trust",
	Long: `Update a workload federation trust. The update mask is derived from the flags
you pass; an omitted flag is left alone. The provider is fixed at creation.

--scoped-role, --allow-cidr and --passthrough-claim replace the whole list.
To empty a list, pass a trust object via --body-file with an explicit
--update-mask (both required together, mutually exclusive with the field
flags), e.g. {"scopedRoleIds":[]} with --update-mask scopedRoleIds. A mask
that names a list the object leaves out (or sets to null) exits 2, nothing
sent: the API would clear it, and an empty scopedRoleIds gives the trust all
of the principal's roles.
--disabled turns the trust off; --disabled=false turns it back on.

Each scoped role id, from --scoped-role or --body-file (scopedRoleIds or
scoped_role_ids), is looked up first: the API stores an unknown role id. An
unknown one exits 4 before anything is sent, --dry-run included. A trust that
was deleted still accepts an update.

Prints the trust as pretty JSON, unwrapped like "trusts get".`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		spID, _ := cmd.Flags().GetString("service-principal-id")
		mask, usingBody, err := bodyFileMask(cmd, spTrustsUpdateSpec.flagNames()...)
		if err != nil {
			return err
		}
		var trust map[string]any
		if usingBody {
			bodyFile, _ := cmd.Flags().GetString("body-file")
			if trust, err = readJSONFile[map[string]any](cmd, "body-file", bodyFile); err != nil {
				return err
			}
			if err := checkTrustBodyMask(trust, mask); err != nil {
				return err
			}
		} else {
			trust = map[string]any{}
			if mask, err = spTrustsUpdateSpec.buildUpdateMask(cmd, trust); err != nil {
				return err
			}
		}
		roles, err := trustRoleRefs(trust, usingBody)
		if err != nil {
			return err
		}
		m := mutation{Method: http.MethodPatch, Path: spTrustPath(spID, args[0]), Body: wrapUpdate("trust", trust, mask), Unwrap: named("trust")}
		if len(roles) == 0 {
			return sendMutation(cmd, m)
		}
		return sendMutation(cmd, m, withPreRead(existingRefsCheck(roleRefKind, roles)))
	},
}

// trustListFields are the trust's list fields. A mask naming one that the
// body omits makes the server clear it; clearing scopedRoleIds widens the
// trust to all of the principal's roles.
var trustListFields = []string{"scopedRoleIds", "allowSourceCidrs", "passthroughClaims"}

// checkTrustBodyMask refuses a mask that names a list field the --body-file
// object leaves out or sets to null; an explicit [] is the way to clear one.
// Paths are folded as the server folds them; "*" and "trust" name every field.
func checkTrustBodyMask(trust map[string]any, mask string) error {
	named := map[string]bool{}
	all := false
	for _, p := range strings.Split(mask, ",") {
		f := foldMaskName(strings.TrimSpace(p))
		if f == "*" || f == "trust" || f == "trust.*" {
			all = true
		}
		named[strings.TrimPrefix(f, "trust.")] = true
	}
	for _, f := range trustListFields {
		v, _, err := bodyField(trust, f)
		if err != nil {
			return err
		}
		if (all || named[foldMaskName(f)]) && v == nil {
			return &usageError{fmt.Errorf("--update-mask names %s but --body-file sets no %s; send an explicit [] to clear it", f, f)}
		}
	}
	return nil
}

// trustRoleRefs reads the scoped role ids from a trust object, in either
// spelling, labeled by where they came from: --scoped-role, or the
// --body-file key.
func trustRoleRefs(trust map[string]any, fromBody bool) ([]labeledRef, error) {
	var out []labeledRef
	add := func(key, id string) {
		label := fmt.Sprintf("--scoped-role %q", id)
		if fromBody {
			label = fmt.Sprintf("--body-file %s %q", key, id)
		}
		out = append(out, labeledRef{appRef{ID: id}, label})
	}
	raw, key, err := bodyField(trust, "scopedRoleIds")
	if err != nil {
		return nil, err
	}
	switch v := raw.(type) {
	case nil:
	case []string:
		for _, id := range v {
			add(key, id)
		}
	case []any:
		for i, e := range v {
			id, ok := e.(string)
			if !ok {
				return nil, &usageError{fmt.Errorf("--body-file: %s[%d] must be a string", key, i)}
			}
			add(key, id)
		}
	default:
		return nil, &usageError{fmt.Errorf("--body-file: %s must be an array of strings", key)}
	}
	return out, nil
}

var spTrustsDeleteCmd = &cobra.Command{
	Use:   "delete <trust-id>",
	Short: "Delete a workload federation trust",
	Long: `Delete a workload federation trust.

Prints the API's response, which is {}; exit 0 is the success signal. Deleting
an already deleted trust also answers {}. ` + wfDeletedTrustNote,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		spID, _ := cmd.Flags().GetString("service-principal-id")
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: spTrustPath(spID, args[0])})
	},
}

// wfTestTokenSteps are TestToken's step fields, in the API's order.
var wfTestTokenSteps = []string{"jwtDecode", "issuerMatch", "signatureValidation", "audienceValidation", "tokenFreshness", "subjectValidation", "celEvaluation", "cidrCheck"}

var spTrustsTestTokenCmd = &cobra.Command{
	Use:   "test-token <trust-id>",
	Short: "Check a workload JWT against a trust without issuing a token (pretty JSON)",
	Long: `Check a workload JWT against a trust, step by step, without issuing an
access token. Changes nothing.

The JWT is read from --subject-token-file ("-" for stdin), never from the
command line; surrounding whitespace is trimmed. --source-ip overrides the
caller's address for the trust's --allow-cidr check.

Prints the API's report: "overallResult", "decodedClaimsJson", and one
object per step ("jwtDecode", "issuerMatch", "signatureValidation", ...),
each with "passed", "skipped" and "detail". The report always prints; then
the command exits 0 when overallResult is true, and 1 naming the failed
steps when it is false. The API answers a disabled trust, a disabled provider
or a deleted trust with a 500 ("trust is disabled", "provider is disabled",
"trust is deleted"; exit 6).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		spID, _ := cmd.Flags().GetString("service-principal-id")
		path, _ := cmd.Flags().GetString("subject-token-file")
		raw, err := readJSONBytes(cmd, path)
		if err != nil {
			return &usageError{fmt.Errorf("reading --subject-token-file: %w", err)}
		}
		token := strings.TrimSpace(string(raw))
		if !hasVisibleRune(token) {
			return &usageError{errors.New("--subject-token-file holds no token")}
		}
		body := map[string]any{"subjectToken": token}
		if flagGiven(cmd, "source-ip") {
			ip, _ := cmd.Flags().GetString("source-ip")
			body["sourceIp"] = ip
		}
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		c, err := newServicePrincipalsClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), spTrustPath(spID, args[0])+"/test", body)
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		var resp map[string]json.RawMessage
		var overall bool
		if json.Unmarshal(data, &resp) != nil || resp["overallResult"] == nil || json.Unmarshal(resp["overallResult"], &overall) != nil {
			return &nonJSONResponseError{errors.New("test-token: the response held no boolean overallResult")}
		}
		if err := writeObject(cmd, data); err != nil {
			return err
		}
		if overall {
			return nil
		}
		var failed []string
		for _, key := range wfTestTokenSteps {
			var step struct {
				StepName string `json:"stepName"`
				Passed   bool   `json:"passed"`
				Skipped  bool   `json:"skipped"`
			}
			if resp[key] == nil || json.Unmarshal(resp[key], &step) != nil || step.Passed || step.Skipped {
				continue
			}
			name := step.StepName
			if name == "" {
				name = key
			}
			failed = append(failed, name)
		}
		if len(failed) == 0 {
			return errors.New("the trust would reject this token")
		}
		return fmt.Errorf("the trust would reject this token; failed steps: %s", strings.Join(failed, ", "))
	},
}

func init() {
	addPaginationFlags(spTrustsListCmd)

	sf := spTrustsSearchCmd.Flags()
	sf.String("service-principal-id", "", "Only trusts of this service principal")
	sf.String("provider-id", "", "Only trusts that reference this provider")
	sf.String("query", "", "Full-text match on display name and description")
	addPaginationFlags(spTrustsSearchCmd)

	spTrustsGetCmd.Flags().String("service-principal-id", "", "Service principal that owns the trust")
	markRequired(spTrustsGetCmd, "service-principal-id")

	cf := spTrustsCreateCmd.Flags()
	cf.String("provider-id", "", `Provider whose tokens the trust accepts (from "workload-federation providers list"); fixed after creation`)
	cf.String("display-name", "", "Display name ("+displayNameRule+")")
	cf.String("condition", "", `CEL expression over the token's claims; must return a bool (e.g. claims.sub == "repo:acme/infra:ref:refs/heads/main")`)
	cf.String("description", "", "Description")
	addRepeatableStringFlag(spTrustsCreateCmd, "scoped-role", `Narrow the trust to a role ID from "c1i roles list" (repeatable)`)
	addRepeatableStringFlag(spTrustsCreateCmd, "allow-cidr", "Only accept token exchanges from this source CIDR (repeatable)")
	addRepeatableStringFlag(spTrustsCreateCmd, "passthrough-claim", `Copy this string claim from the workload token into the issued token's "c1wfc" claim (repeatable)`)
	markRequired(spTrustsCreateCmd, "provider-id", "display-name", "condition")
	markMutating(spTrustsCreateCmd)

	uf := spTrustsUpdateCmd.Flags()
	uf.String("service-principal-id", "", "Service principal that owns the trust")
	markRequired(spTrustsUpdateCmd, "service-principal-id")
	uf.String("display-name", "", "New display name (omit to leave unchanged; "+displayNameRule+")")
	uf.String("description", "", `New description; "" is sent as an empty value (omit to leave unchanged)`)
	allowEmpty(spTrustsUpdateCmd, emptyIsSent, "description")
	uf.String("condition", "", "New CEL condition (omit to leave unchanged)")
	uf.Bool("disabled", false, "Disable the trust (--disabled=false to enable; omit to leave unchanged)")
	addRepeatableStringFlag(spTrustsUpdateCmd, "scoped-role", "Replace the scoped role IDs (repeatable)")
	addRepeatableStringFlag(spTrustsUpdateCmd, "allow-cidr", "Replace the source CIDR allowlist (repeatable)")
	addRepeatableStringFlag(spTrustsUpdateCmd, "passthrough-claim", "Replace the passthrough claim names (repeatable)")
	uf.String("body-file", "", "Trust JSON object (file, or \"-\" for stdin); requires --update-mask; mutually exclusive with the field flags")
	uf.String("update-mask", "", bodyFileMaskUsage)
	setBlankHint(spTrustsUpdateCmd, "update-mask", updateMaskBlankHint)
	registerUpdateSpec(spTrustsUpdateCmd, spTrustsUpdateSpec)
	markMutating(spTrustsUpdateCmd)

	spTrustsDeleteCmd.Flags().String("service-principal-id", "", "Service principal that owns the trust")
	markRequired(spTrustsDeleteCmd, "service-principal-id")
	markMutating(spTrustsDeleteCmd)

	tf := spTrustsTestTokenCmd.Flags()
	tf.String("service-principal-id", "", "Service principal that owns the trust")
	tf.String("subject-token-file", "", `File holding the workload JWT ("-" for stdin)`)
	tf.String("source-ip", "", "Source IP (optionally with a CIDR prefix) to check against the trust's allowlist, instead of the caller's")
	markRequired(spTrustsTestTokenCmd, "service-principal-id", "subject-token-file")

	spTrustsCmd.AddCommand(spTrustsListCmd, spTrustsSearchCmd, spTrustsGetCmd, spTrustsCreateCmd, spTrustsUpdateCmd, spTrustsDeleteCmd, spTrustsTestTokenCmd)
	servicePrincipalsCmd.AddCommand(spTrustsCmd)
}
