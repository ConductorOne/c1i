package cmd

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

var decoysCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Mint a decoy credential (the vended secret is shown once)",
	Long: `Create a decoy. Exactly one selector flag chooses its kind:

  --user-client-credential-user-id <user-id>  a client-credential decoy; the user must be an existing HUMAN or SERVICE user
  --access-token-subject-user-id <user-id>    a session access-token decoy under an existing user; --access-token-expires sets its lifetime
  --connector-client                          a connector-shaped credential decoy; the server allocates its placement, no further flags
  --workload-fed-provider-id <provider-id>    a workload-federation-trust decoy under an existing provider; requires --workload-fed-condition

Refused with exit 2 before a request is sent:
  - none, or more than one, of the four selector flags above
  - --access-token-expires without --access-token-subject-user-id
  - --workload-fed-condition or --workload-fed-service-principal-user-id without --workload-fed-provider-id
  - --workload-fed-provider-id without --workload-fed-condition
  - a malformed --annotation (not key=value, or a control character in the key)

Alternatively, pass the complete DecoyServiceCreateRequest body with
--body-file (mutually exclusive with every flag above); it is sent verbatim.

Prints the new decoy as pretty JSON, unwrapped like "decoys get" (jq -er
.id), with the vended secret under .material, shaped by kind:
.material.clientCredential
{clientId, clientSecret} for a user-client-credential or connector-client
decoy, .material.accessToken.accessToken for an access-token decoy, or
.material.workloadFederation.workloadFederationTrustId for a
workload-federation decoy (no server-vended secret there; bind the trust on
the IdP side). This is the only response that carries it — the server
cannot return it again; run "decoys rotate" for a fresh one. Honors
--dry-run.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := buildDecoyCreateBody(cmd)
		if err != nil {
			return err
		}
		opts := []mutationOpt{secretBearing(secretDecoy)}
		for _, flag := range decoyUserRefFlags {
			if flagGiven(cmd, flag) {
				opts = append(opts, withPreRead(existingUsersCheck(flag, []string{mustGetString(cmd, flag)})))
			}
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: decoysPath, Body: body}, opts...)
	},
}

// decoyUserRefFlags are decoys create's user-id-bearing flags; existingUsersCheck
// verifies whichever one is given before the send.
var decoyUserRefFlags = []string{
	"user-client-credential-user-id",
	"access-token-subject-user-id",
	"workload-fed-service-principal-user-id",
}

// decoySelectorFlags are the four create-input oneof selectors; exactly one
// is required.
var decoySelectorFlags = []string{
	"user-client-credential-user-id",
	"access-token-subject-user-id",
	"connector-client",
	"workload-fed-provider-id",
}

// decoyCreateCompanionFlags are the kind-specific optional companions; each
// requires its own selector flag.
var decoyCreateCompanionFlags = []string{
	"access-token-expires",
	"workload-fed-condition",
	"workload-fed-service-principal-user-id",
}

// decoyCreateFlagNames are every create-input flag --body-file replaces.
func decoyCreateFlagNames() []string {
	names := append([]string{"display-name", "description", "annotation"}, decoySelectorFlags...)
	return append(names, decoyCreateCompanionFlags...)
}

// buildDecoyCreateBody assembles the create request from flags, or reads it
// verbatim from --body-file. Pure (no network/auth) so --dry-run and unit
// tests exercise the same path the live request uses.
func buildDecoyCreateBody(cmd *cobra.Command) (map[string]any, error) {
	if flagGiven(cmd, "body-file") {
		for _, n := range decoyCreateFlagNames() {
			if flagGiven(cmd, n) {
				return nil, &usageError{fmt.Errorf("--body-file is mutually exclusive with --%s", n)}
			}
		}
		return readJSONFile[map[string]any](cmd, "body-file", mustGetString(cmd, "body-file"))
	}

	input, err := buildDecoyCreateInput(cmd)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	for k, v := range input {
		body[k] = v
	}
	if flagGiven(cmd, "display-name") {
		body["displayName"] = mustGetString(cmd, "display-name")
	}
	if flagGiven(cmd, "description") {
		body["description"] = mustGetString(cmd, "description")
	}
	pairs, err := repeatableStringFlag(cmd, "annotation")
	if err != nil {
		return nil, err
	}
	if len(pairs) > 0 {
		annotations, err := parseKeyValueFlag(pairs, "annotation", jsonKeyValues)
		if err != nil {
			return nil, err
		}
		body["annotations"] = annotations
	}
	return body, nil
}

// decoySelectorHint lists the four selector flags for the "none given" error.
const decoySelectorHint = "--user-client-credential-user-id, --access-token-subject-user-id, --connector-client, or --workload-fed-provider-id"

// buildDecoyCreateInput refuses anything but exactly one selector flag and
// returns the chosen create_input oneof arm.
func buildDecoyCreateInput(cmd *cobra.Command) (map[string]any, error) {
	var set []string
	for _, n := range decoySelectorFlags {
		if flagGiven(cmd, n) {
			set = append(set, "--"+n)
		}
	}
	switch {
	case len(set) == 0:
		return nil, &usageError{fmt.Errorf("exactly one of %s is required: a decoy has exactly one kind", decoySelectorHint)}
	case len(set) > 1:
		return nil, &usageError{fmt.Errorf("%s and %s are mutually exclusive: a decoy has exactly one kind", set[0], set[1])}
	}

	switch {
	case flagGiven(cmd, "user-client-credential-user-id"):
		if err := refuseOtherCompanions(cmd, "access-token-expires", "workload-fed-condition", "workload-fed-service-principal-user-id"); err != nil {
			return nil, err
		}
		return map[string]any{"userClientCredential": map[string]any{"userId": mustGetString(cmd, "user-client-credential-user-id")}}, nil
	case flagGiven(cmd, "access-token-subject-user-id"):
		if err := refuseOtherCompanions(cmd, "workload-fed-condition", "workload-fed-service-principal-user-id"); err != nil {
			return nil, err
		}
		at := map[string]any{"subjectUserId": mustGetString(cmd, "access-token-subject-user-id")}
		if v, ok, err := protoDurationFlag(cmd, "access-token-expires"); err != nil {
			return nil, err
		} else if ok {
			at["expiresIn"] = v
		}
		return map[string]any{"accessToken": at}, nil
	case flagGiven(cmd, "connector-client"):
		if err := refuseOtherCompanions(cmd, "access-token-expires", "workload-fed-condition", "workload-fed-service-principal-user-id"); err != nil {
			return nil, err
		}
		return map[string]any{"connectorClient": map[string]any{}}, nil
	default: // --workload-fed-provider-id
		if err := refuseOtherCompanions(cmd, "access-token-expires"); err != nil {
			return nil, err
		}
		if err := requireNonEmpty(cmd, "workload-fed-condition"); err != nil {
			return nil, fmt.Errorf("%w (required with --workload-fed-provider-id)", err)
		}
		wf := map[string]any{
			"providerId":          mustGetString(cmd, "workload-fed-provider-id"),
			"conditionExpression": mustGetString(cmd, "workload-fed-condition"),
		}
		if flagGiven(cmd, "workload-fed-service-principal-user-id") {
			wf["servicePrincipalUserId"] = mustGetString(cmd, "workload-fed-service-principal-user-id")
		}
		return map[string]any{"workloadFed": wf}, nil
	}
}

// refuseOtherCompanions rejects a companion flag of a different kind than the
// one selected, naming the first it finds set.
func refuseOtherCompanions(cmd *cobra.Command, names ...string) error {
	for _, n := range names {
		if flagGiven(cmd, n) {
			return &usageError{fmt.Errorf("--%s requires its own selector flag, not the one given", n)}
		}
	}
	return nil
}

func init() {
	c := decoysCreateCmd
	f := c.Flags()
	f.String("display-name", "", "Display name for the new decoy")
	f.String("description", "", "Description for the new decoy")
	addRepeatableStringFlag(c, "annotation", decoyAnnotationFlagUsage)
	f.String("user-client-credential-user-id", "", "Existing User ID to plant a client-credential decoy under (must be typ=HUMAN or typ=SERVICE)")
	f.String("access-token-subject-user-id", "", "Existing User ID whose subject claim the access-token decoy references")
	addProtoDurationFlag(c, "access-token-expires", "Lifetime of the access-token decoy as a protobuf duration (3600s) or a Go duration (1h), sent in seconds; requires --access-token-subject-user-id")
	f.Bool("connector-client", false, "Plant a connector-shaped credential decoy; the server allocates its placement")
	f.String("workload-fed-provider-id", "", "Existing WorkloadFederationProvider ID to bind the decoy Trust under")
	f.String("workload-fed-condition", "", "CEL boolean evaluated against the presented JWT's claims map (required with --workload-fed-provider-id), e.g. claims.sub.startsWith(\"repo:acme/fake-infra:\")")
	f.String("workload-fed-service-principal-user-id", "", "Existing SERVICE-typed User the Trust would act-as on match; requires --workload-fed-provider-id")
	f.String("body-file", "", "Complete DecoyServiceCreateRequest JSON object, sent verbatim (file, or \"-\" for stdin); mutually exclusive with every flag above")
	markUserRefChecked(c, decoyUserRefFlags...)
	markMutating(c)
	decoysCmd.AddCommand(c)
}
