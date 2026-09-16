package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/spf13/cobra"
)

// service-principals manages tenant-owned non-human identities (SPCs): the
// principal, its client credentials, and its subject bindings. The API is not
// in the public OpenAPI spec, so these commands are the first-class surface.

var servicePrincipalsCmd = &cobra.Command{
	Use:     "service-principals",
	Aliases: []string{"service-principal", "sp"},
	Short:   "Manage service principals (non-human identities), their credentials, and bindings",
	Long: `Manage service principals: tenant-owned non-human identities.

A service principal is an identity; a client credential on it is what a caller
authenticates with (client_id/secret). A binding lets another subject (a
function, SSO application, AuthZEN server, or edge) act as the principal.

  service-principals list
  service-principals get <sp-id>
  service-principals create --display-name "payments-reconciler"
  service-principals credentials create <sp-id> --display-name ci --expires 720h
  service-principals bindings add --service-principal-id <sp-id> --function-id <fn-id>`,
}

// newServicePrincipalsClient is the client seam for the non-list SPC commands
// (get/create/update/delete and the credential/binding leaves), so tests can
// substitute an httptest-backed client. List commands use newListClient.
var newServicePrincipalsClient = newClient

func init() {
	rootCmd.AddCommand(servicePrincipalsCmd)
}

// spPath builds the path for one service principal.
func spPath(id string) string {
	return client.Path("/api/v1/service_principals/%s", id)
}

// spCredentialsPath builds the credentials collection path for one SP.
func spCredentialsPath(spID string) string {
	return client.Path("/api/v1/service_principals/%s/credentials", spID)
}

// spCredentialPath builds the path for one credential of one SP.
func spCredentialPath(spID, id string) string {
	return client.Path("/api/v1/service_principals/%s/credentials/%s", spID, id)
}

// Binding routes carry no path ids — the subject and SP travel in the body.
const (
	spBindingsPath       = "/api/v1/service_principals/bindings"
	spBindingsListPath   = "/api/v1/service_principals/bindings/list"
	spBindingsDeletePath = "/api/v1/service_principals/bindings/delete"
)

// bindingSubjectSpecs maps the binding subject flags to the proto oneof.
// appFlag is empty for a bare-id subject; otherwise both halves are required.
var bindingSubjectSpecs = []struct {
	key     string // proto3-JSON oneof key
	idFlag  string
	appFlag string
}{
	{"functionId", "function-id", ""},
	{"ssoApplication", "sso-application-id", "sso-app-id"},
	{"authzenServer", "authzen-server-id", "authzen-app-id"},
	{"edge", "edge-id", "edge-app-id"},
}

// addBindingSubjectFlags registers the subject selector flags shared by the
// binding add/list/delete commands.
func addBindingSubjectFlags(cmd *cobra.Command) {
	cmd.Flags().String("function-id", "", "Subject: function ID (the function acts as this service principal)")
	cmd.Flags().String("sso-application-id", "", "Subject: SSO application ID (requires --sso-app-id)")
	cmd.Flags().String("sso-app-id", "", "App ID owning --sso-application-id")
	cmd.Flags().String("authzen-server-id", "", "Subject: AuthZEN server ID (requires --authzen-app-id)")
	cmd.Flags().String("authzen-app-id", "", "App ID owning --authzen-server-id")
	cmd.Flags().String("edge-id", "", "Subject: edge ID (requires --edge-app-id)")
	cmd.Flags().String("edge-app-id", "", "App ID owning --edge-id")
}

// bindingSubject builds the subject oneof from flags, requiring exactly one
// subject kind (and both halves of an app-scoped subject).
func bindingSubject(cmd *cobra.Command) (map[string]any, error) {
	var chosen map[string]any
	var chosenFlag string
	for _, s := range bindingSubjectSpecs {
		id, _ := cmd.Flags().GetString(s.idFlag)
		var value any
		switch s.appFlag {
		case "":
			if id == "" {
				continue
			}
			value = id
		default:
			appID, _ := cmd.Flags().GetString(s.appFlag)
			if id == "" && appID == "" {
				continue
			}
			if id == "" || appID == "" {
				return nil, &usageError{fmt.Errorf("--%s and --%s must be given together", s.idFlag, s.appFlag)}
			}
			value = map[string]any{"appId": appID, "id": id}
		}
		if chosen != nil {
			return nil, &usageError{fmt.Errorf("only one subject may be given: --%s and --%s are mutually exclusive", chosenFlag, s.idFlag)}
		}
		chosen = map[string]any{s.key: value}
		chosenFlag = s.idFlag
	}
	if chosen == nil {
		return nil, &usageError{fmt.Errorf("a subject is required: pass one of --function-id, --sso-application-id (with --sso-app-id), --authzen-server-id (with --authzen-app-id), or --edge-id (with --edge-app-id)")}
	}
	return chosen, nil
}
