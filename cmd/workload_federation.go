package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var workloadFederationCmd = &cobra.Command{
	Use:     "workload-federation",
	Aliases: []string{"wf"},
	Short:   "Manage workload federation providers (external JWT issuers) and test CEL conditions",
	Long: `Manage workload federation: external workloads (CI jobs, SPIFFE workloads)
exchanging their own JWT for a C1.ai token that acts as a service principal.

A provider registers an issuer for the tenant. A trust, created per service
principal with "c1i service-principals trusts", names a provider and a CEL
condition over the token's claims.

  workload-federation providers list
  workload-federation providers create --display-name gha --well-known-provider github-actions \
    --issuer-url https://token.actions.githubusercontent.com
  workload-federation test-cel --expression 'claims.sub == "x"' --claims '{"sub":"x"}'`,
}

var wfProvidersCmd = &cobra.Command{
	Use:   "providers",
	Short: "Manage workload federation providers (registered external JWT issuers)",
}

const wfProvidersPath = "/api/v1/workload_federation/providers"

func wfProviderPath(id string) string {
	return client.Path("/api/v1/workload_federation/providers/%s", id)
}

// wfProvider is the subset of WorkloadFederationProvider surfaced in rows.
type wfProvider struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	Description       string `json:"description"`
	IssuerURL         string `json:"issuerUrl"`
	WellKnownProvider string `json:"wellKnownProvider"`
	Disabled          bool   `json:"disabled"`
	CreatedAt         string `json:"createdAt"`
	UpdatedAt         string `json:"updatedAt"`
	SPIFFE            *struct {
		BundleEndpointURL string `json:"bundleEndpointUrl"`
	} `json:"spiffe"`
}

// wfProviderRow omits bundle_endpoint_url on a provider with no SPIFFE block.
func wfProviderRow(p wfProvider) map[string]any {
	row := map[string]any{
		"id":                  nilIfEmpty(p.ID),
		"display_name":        nilIfEmpty(p.DisplayName),
		"description":         nilIfEmpty(p.Description),
		"issuer_url":          nilIfEmpty(p.IssuerURL),
		"well_known_provider": nilIfEmpty(p.WellKnownProvider),
		"disabled":            p.Disabled,
		"created_at":          nilIfEmpty(p.CreatedAt),
		"updated_at":          nilIfEmpty(p.UpdatedAt),
	}
	if p.SPIFFE != nil {
		row["bundle_endpoint_url"] = nilIfEmpty(p.SPIFFE.BundleEndpointURL)
	}
	return row
}

var wfProvidersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List workload federation providers (NDJSON output)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := listClientFor(cmd)
		if err != nil {
			return err
		}
		return listPages(cmd, c, pageSpec[wfProvider]{
			Route: "GET " + wfProvidersPath,
			Fetch: getPage(cmd.Context(), wfProvidersPath, nil),
			Parse: parseList[wfProvider],
			ID:    func(p wfProvider) string { return p.ID },
			Rows:  rowFn(wfProviderRow),
		})
	},
}

var wfProvidersGetCmd = &cobra.Command{
	Use:   "get <provider-id>",
	Short: "Get a single workload federation provider (pretty JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getResource(cmd, wfProviderPath(args[0]), nil, "id", "provider")
	},
}

// wellKnownProviderFlag is closed: c1i picks the settings block from it.
// C1_EDGE is left out; the API refuses it on create.
var wellKnownProviderFlag = enumFlag{flag: "well-known-provider", prefix: "WELL_KNOWN_WORKLOAD_PROVIDER_", unknown: enumReject, values: []enumValue{
	{name: "custom", api: "WELL_KNOWN_WORKLOAD_PROVIDER_CUSTOM"},
	{name: "github-actions", api: "WELL_KNOWN_WORKLOAD_PROVIDER_GITHUB_ACTIONS"},
	{name: "gitlab-ci", api: "WELL_KNOWN_WORKLOAD_PROVIDER_GITLAB_CI"},
	{name: "hcp-terraform", api: "WELL_KNOWN_WORKLOAD_PROVIDER_HCP_TERRAFORM"},
	{name: "aws-iam-outbound", api: "WELL_KNOWN_WORKLOAD_PROVIDER_AWS_IAM_OUTBOUND"},
	{name: "spiffe", api: "WELL_KNOWN_WORKLOAD_PROVIDER_SPIFFE"},
}}

const wfSPIFFE = "WELL_KNOWN_WORKLOAD_PROVIDER_SPIFFE"

var wfProvidersCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Register a workload federation provider (an external JWT issuer)",
	Long: `Register a workload federation provider: an external JWT issuer whose tokens
trusts may accept.

--well-known-provider spiffe takes a spiffe:// trust-domain --issuer-url and
requires --bundle-endpoint-url (the HTTPS JWKS URL serving its signing keys).
Every other kind takes an https:// issuer, checked by OIDC discovery, and
refuses --bundle-endpoint-url. The API refuses an issuer it cannot reach
("OIDC discovery at ... is unreachable or invalid"), an issuer that does not
fit a named kind (github-actions takes only
https://token.actions.githubusercontent.com), and an issuer already registered
in the tenant (409, exit 2). The kind and issuer are fixed after creation. A
new provider is enabled.

Prints the provider as pretty JSON, unwrapped like "providers get": the id is
.id.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		f := cmd.Flags()
		raw, _ := f.GetString("well-known-provider")
		kind, err := wellKnownProviderFlag.parse(raw)
		if err != nil {
			return err
		}
		displayName, _ := f.GetString("display-name")
		issuer, _ := f.GetString("issuer-url")
		body := map[string]any{"displayName": displayName, "issuerUrl": issuer, "wellKnownProvider": kind}
		if flagGiven(cmd, "description") {
			d, _ := f.GetString("description")
			body["description"] = d
		}
		bundle, _ := f.GetString("bundle-endpoint-url")
		switch {
		case kind == wfSPIFFE && !flagGiven(cmd, "bundle-endpoint-url"):
			return &usageError{errors.New("--well-known-provider spiffe requires --bundle-endpoint-url")}
		case kind == wfSPIFFE:
			body["spiffe"] = map[string]any{"bundleEndpointUrl": bundle}
		case flagGiven(cmd, "bundle-endpoint-url"):
			return &usageError{errors.New("--bundle-endpoint-url applies only to --well-known-provider spiffe")}
		default:
			body["oidc"] = map[string]any{}
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: wfProvidersPath, Body: body, Unwrap: named("provider")})
	},
}

var wfProvidersUpdateSpec = &updateSpec{fields: []updateField{
	strUpdate("display-name", "displayName"),
	strUpdate("description", "description"),
	boolUpdate("disabled", "disabled"),
	hookUpdate([]string{"bundle-endpoint-url"}, func(cmd *cobra.Command, obj map[string]any) ([]string, error) {
		if !flagGiven(cmd, "bundle-endpoint-url") {
			return nil, nil
		}
		v, _ := cmd.Flags().GetString("bundle-endpoint-url")
		obj["spiffe"] = map[string]any{"bundleEndpointUrl": v}
		return []string{"spiffe.bundleEndpointUrl"}, nil
	}),
}}

var wfProvidersUpdateCmd = &cobra.Command{
	Use:   "update <provider-id>",
	Short: "Update a workload federation provider",
	Long: `Update a workload federation provider. The update mask is derived from the
flags you pass; an omitted flag is left alone. The kind and issuer are fixed.

--disabled makes the provider reject every token exchange; --disabled=false
re-enables it. --bundle-endpoint-url is for SPIFFE providers: the API re-checks
it by fetching its JWKS and refuses one it cannot fetch (exit 2). On any other
provider the API still fetches it (an unfetchable URL is a 400, exit 2) and
then discards it.

A deleted provider still accepts an update (200) and stays out of "providers
list".

Prints the provider as pretty JSON, unwrapped like "providers get".`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := map[string]any{}
		mask, err := wfProvidersUpdateSpec.buildUpdateMask(cmd, provider)
		if err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPatch, Path: wfProviderPath(args[0]), Body: wrapUpdate("provider", provider, mask), Unwrap: named("provider")})
	},
}

var wfProvidersDeleteCmd = &cobra.Command{
	Use:   "delete <provider-id>",
	Short: "Delete a workload federation provider",
	Long: `Delete a workload federation provider.

The API refuses while a trust references it ("cannot delete provider: an
active trust references this provider; delete trusts first", exit 2); find
them with "c1i service-principals trusts search --provider-id <provider-id>"
(search can lag a just-created trust by about a second).

Prints the API's response, which is {}; exit 0 is the success signal.
Deleting it again also answers {}. A deleted provider leaves "providers list"
at once, but "providers get" still returns it, with no field marking it
deleted, and "providers update" still changes it.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return sendMutation(cmd, mutation{Method: http.MethodDelete, Path: wfProviderPath(args[0])})
	},
}

var wfTestCelCmd = &cobra.Command{
	Use:   "test-cel",
	Short: "Evaluate a trust CEL condition against sample claims (pretty JSON)",
	Long: `Evaluate a CEL expression against sample token claims, as a trust's
--condition would be, without a token, provider or trust. Changes nothing.

--claims (or --claims-file, "-" for stdin) is a JSON object of claims. The
API also accepts a signed JWT and evaluates its claims; in the runs checked it
did not verify the signature, and refused "alg": "none". Pass a JWT with
--claims-file, not --claims, to keep it off the command line.

Prints the API's answer: "matched", "error" and "expression". The answer
always prints; then the command exits 0 when the expression matched, 1 when it
evaluated to false, and 2 when the API reports an error (a syntax error, a
non-bool result such as "expression must return bool, got int", or claims
that are neither JSON nor a JWT).`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		f := cmd.Flags()
		var claims string
		switch {
		case flagGiven(cmd, "claims") && flagGiven(cmd, "claims-file"):
			return &usageError{errors.New("--claims and --claims-file are mutually exclusive")}
		case flagGiven(cmd, "claims"):
			claims, _ = f.GetString("claims")
		case flagGiven(cmd, "claims-file"):
			p, _ := f.GetString("claims-file")
			raw, err := readJSONBytes(cmd, p)
			if err != nil {
				return &usageError{fmt.Errorf("reading --claims-file: %w", err)}
			}
			claims = string(raw)
		default:
			return &usageError{errors.New("one of --claims or --claims-file is required")}
		}
		expr, _ := f.GetString("expression")
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}
		c, err := newClient(cmd, baseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		data, err := c.Post(cmd.Context(), "/api/v1/workload_federation/test-cel", map[string]any{"expression": expr, "claimsJson": claims})
		if err != nil {
			return fmt.Errorf("API error: %w", err)
		}
		var resp struct {
			Matched *bool  `json:"matched"`
			Error   string `json:"error"`
		}
		if json.Unmarshal(data, &resp) != nil || resp.Matched == nil {
			return &nonJSONResponseError{errors.New("test-cel: the response held no boolean matched")}
		}
		if err := writeObject(cmd, data); err != nil {
			return err
		}
		switch {
		case resp.Error != "":
			return &usageError{fmt.Errorf("the expression did not evaluate: %s", resp.Error)}
		case !*resp.Matched:
			return errors.New("the expression evaluated to false for these claims")
		}
		return nil
	},
}

func init() {
	addPaginationFlags(wfProvidersListCmd)

	cf := wfProvidersCreateCmd.Flags()
	cf.String("display-name", "", "Display name ("+displayNameRule+")")
	cf.String("issuer-url", "", "Issuer: an https:// OIDC issuer, or a spiffe:// trust domain for --well-known-provider spiffe")
	cf.String("well-known-provider", "", "Provider kind: "+wellKnownProviderFlag.usage())
	cf.String("description", "", "Description")
	cf.String("bundle-endpoint-url", "", "HTTPS JWKS URL of the SPIFFE trust domain's signing keys (spiffe only, required there)")
	markRequired(wfProvidersCreateCmd, "display-name", "issuer-url", "well-known-provider")
	markMutating(wfProvidersCreateCmd)

	uf := wfProvidersUpdateCmd.Flags()
	uf.String("display-name", "", "New display name (omit to leave unchanged; "+displayNameRule+")")
	uf.String("description", "", `New description; "" is sent as an empty value (omit to leave unchanged)`)
	allowEmpty(wfProvidersUpdateCmd, emptyIsSent, "description")
	uf.Bool("disabled", false, "Disable the provider (--disabled=false to enable; omit to leave unchanged)")
	uf.String("bundle-endpoint-url", "", "New JWKS URL for a SPIFFE provider (omit to leave unchanged)")
	registerUpdateSpec(wfProvidersUpdateCmd, wfProvidersUpdateSpec)
	markMutating(wfProvidersUpdateCmd)

	markMutating(wfProvidersDeleteCmd)

	tf := wfTestCelCmd.Flags()
	tf.String("expression", "", "CEL expression over claims; must return a bool")
	tf.String("claims", "", "Claims as a JSON object (or a JWT)")
	tf.String("claims-file", "", `File holding the claims ("-" for stdin)`)
	markRequired(wfTestCelCmd, "expression")

	wfProvidersCmd.AddCommand(wfProvidersListCmd, wfProvidersGetCmd, wfProvidersCreateCmd, wfProvidersUpdateCmd, wfProvidersDeleteCmd)
	workloadFederationCmd.AddCommand(wfProvidersCmd, wfTestCelCmd)
	rootCmd.AddCommand(workloadFederationCmd)
}
