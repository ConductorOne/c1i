package cmd

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

var automationsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an automation (pretty JSON)",
	Long: `Create an automation.

--steps-file is a JSON array sent as automationSteps; --triggers-file a JSON
array sent as triggers. Each element is one AutomationStep or AutomationTrigger
object (see "c1i docs endpoint /api/v1/automations"). An automation with no
triggers runs only through "automations execute".

--body-file is the full CreateAutomationRequest, sent verbatim, for fields the
flags don't cover (circuitBreakerMax/circuitBreakerPeriod, draft steps and
triggers, appId, annotations, context). It is mutually exclusive with every
other flag here. Only one of --body-file, --steps-file and --triggers-file can
read stdin ("-"). Omitting --enabled leaves the server default (false). The
API currently rejects an enabled creation with no steps with a 500 "steps
cannot be nil"; create it disabled, add steps, then enable it with
"automations update --enabled".

Prints the automation as pretty JSON, unwrapped like "automations get": the id
is .id. The response's webhookCapabilityUrl and webhookHmacSecret stay
top-level siblings. Honors --dry-run.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := buildAutomationCreateBody(cmd)
		if err != nil {
			return err
		}
		return sendMutation(cmd, mutation{Method: http.MethodPost, Path: "/api/v1/automations", Body: body},
			secretBearing(secretAutomationWebhook))
	},
}

var automationCreateFieldFlags = []string{"display-name", "description", "enabled", "steps-file", "triggers-file"}

func buildAutomationCreateBody(cmd *cobra.Command) (map[string]any, error) {
	n := 0
	for _, f := range []string{"body-file", "steps-file", "triggers-file"} {
		if mustGetString(cmd, f) == "-" {
			n++
		}
	}
	if n > 1 {
		return nil, &usageError{errors.New(`only one of --body-file, --steps-file and --triggers-file can read stdin ("-")`)}
	}

	if flagGiven(cmd, "body-file") {
		for _, f := range automationCreateFieldFlags {
			if flagGiven(cmd, f) {
				return nil, &usageError{fmt.Errorf("--body-file is mutually exclusive with --%s", f)}
			}
		}
		return readJSONFile[map[string]any](cmd, "body-file", mustGetString(cmd, "body-file"))
	}

	displayName := mustGetString(cmd, "display-name")
	if displayName == "" {
		return nil, &usageError{errors.New("--display-name is required (or pass --body-file)")}
	}
	body := map[string]any{"displayName": displayName}
	if v := mustGetString(cmd, "description"); v != "" {
		body["description"] = v
	}
	if flagGiven(cmd, "enabled") {
		enabled, _ := cmd.Flags().GetBool("enabled")
		body["enabled"] = enabled
	}
	for _, fk := range [][2]string{{"steps-file", "automationSteps"}, {"triggers-file", "triggers"}} {
		flag, key := fk[0], fk[1]
		path := mustGetString(cmd, flag)
		if path == "" {
			continue
		}
		arr, err := readJSONFile[[]any](cmd, flag, path)
		if err != nil {
			return nil, err
		}
		body[key] = arr
	}
	return body, nil
}

func init() {
	f := automationsCreateCmd.Flags()
	f.String("display-name", "", "Display name ("+displayNameRule+") (required unless --body-file)")
	f.String("description", "", "Description")
	f.Bool("enabled", false, "Create enabled (--enabled) or disabled (--enabled=false); omit to send neither")
	f.String("steps-file", "", "JSON array of AutomationStep objects, sent as automationSteps (file, or \"-\" for stdin)")
	f.String("triggers-file", "", "JSON array of AutomationTrigger objects, sent as triggers (file, or \"-\" for stdin)")
	allowEmpty(automationsCreateCmd, emptyIsOmitted, "description", "steps-file", "triggers-file")
	f.String("body-file", "", "Full CreateAutomationRequest JSON body, verbatim (file, or \"-\" for stdin); mutually exclusive with the flags above")
	markMutating(automationsCreateCmd)
	automationsCmd.AddCommand(automationsCreateCmd)
}
