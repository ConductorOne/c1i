package cmd

import (
	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

var decoysCmd = &cobra.Command{
	Use:     "decoys",
	Aliases: []string{"decoy"},
	Short:   "Manage decoy credentials (honeytokens that raise a Finding when used)",
	Long: `Manage decoys: planted honey-credentials that exist only to be stolen. A
decoy is exactly one of four kinds, chosen by which create selector flag is
given:

  --user-client-credential-user-id <user-id>  a client-credential decoy under an existing HUMAN/SERVICE user
  --access-token-subject-user-id <user-id>     a session access-token decoy under an existing user
  --connector-client                           a connector-shaped credential decoy (server-allocated placement)
  --workload-fed-provider-id <provider-id>     a workload-federation-trust decoy under an existing provider

  decoys list
  decoys get <decoy-id>
  decoys search --kind user-client-credential --status active
  decoys create --user-client-credential-user-id <user-id> --display-name "aws-root-decoy"
  decoys update <decoy-id> --disabled
  decoys rotate <decoy-id>
  decoys delete <decoy-id>

Create and rotate return freshly vended secret material (a clientSecret, an
accessToken, or a workloadFederationTrustId, under .material) exactly once,
in that one response: it is never logged and the server cannot return it
again. Capture it now, or run "decoys rotate" for a fresh one.`,
}

func init() {
	rootCmd.AddCommand(decoysCmd)
}

const decoysPath = "/api/v1/decoys"
const decoysSearchPath = decoysPath + "/search"

// decoyPath builds the path for one decoy.
func decoyPath(id string) string {
	return client.Path(decoysPath+"/%s", id)
}

// decoyRotatePath builds the rotate action path for one decoy.
func decoyRotatePath(id string) string {
	return client.Path(decoysPath+"/%s/rotate", id)
}

// decoyKindFlag maps --kind to the API's DecoyKind enum ("decoys search" filter).
var decoyKindFlag = enumFlag{flag: "kind", prefix: "DECOY_KIND_", unknown: enumPassPrefixed, values: []enumValue{
	{name: "user-client-credential", api: "DECOY_KIND_USER_CLIENT_CREDENTIAL", aliases: []string{"userclientcredential"}},
	{name: "connector-client", api: "DECOY_KIND_CONNECTOR_CLIENT", aliases: []string{"connectorclient"}},
	{name: "workload-federation", api: "DECOY_KIND_WORKLOAD_FEDERATION", aliases: []string{"workloadfederation", "workload-fed"}},
	{name: "access-token", api: "DECOY_KIND_ACCESS_TOKEN", aliases: []string{"accesstoken"}},
}}

// decoyStatusFlag maps --status to DecoyStatusFilter ("decoys search" filter only).
var decoyStatusFlag = enumFlag{flag: "status", prefix: "DECOY_STATUS_FILTER_", unknown: enumReject, values: []enumValue{
	{name: "active", api: "DECOY_STATUS_FILTER_ACTIVE"},
	{name: "disabled", api: "DECOY_STATUS_FILTER_DISABLED"},
}}

// decoyAnnotationFlagUsage is shared by create and update.
const decoyAnnotationFlagUsage = "Annotation key=value (repeatable)"
