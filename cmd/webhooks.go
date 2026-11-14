package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/redact"
	"github.com/spf13/cobra"
)

var webhooksCmd = &cobra.Command{
	Use:     "webhooks",
	Aliases: []string{"webhook"},
	Short:   "Manage outbound webhooks (the endpoints policies and automations call)",
	Long: `Manage outbound webhooks: registered https endpoints that C1.ai calls, for
example from a policy's webhook approval or provision step.

  webhooks list
  webhooks get <webhook-id>
  webhooks create --display-name "ticketing-bridge" --destination-url https://hooks.example.com/c1
  webhooks update <webhook-id> --destination-url https://hooks.example.com/c1-v2
  webhooks test <webhook-id>
  webhooks delete <webhook-id>

A webhook carries no signing secret: create and get return the same fields.`,
}

func init() {
	rootCmd.AddCommand(webhooksCmd)
}

const webhooksPath = "/api/v1/webhooks"

// webhookPath builds the path for one webhook.
func webhookPath(id string) string {
	return client.Path(webhooksPath+"/%s", id)
}

// webhookDisplayNameRule adds the server's length bound to the shared blank
// rule: create and update both 400 outside it.
const webhookDisplayNameRule = displayNameRule + "; the server requires 3-100 characters"

// webhookFieldFlags registers the writable Webhook fields shared by create and
// update.
func webhookFieldFlags(cmd *cobra.Command, update bool) {
	verb, keep := "Webhook", ""
	if update {
		verb, keep = "New", "omit to leave unchanged; "
	}
	cmd.Flags().String("display-name", "", verb+" display name ("+keep+webhookDisplayNameRule+")")
	cmd.Flags().String("destination-url", "", verb+" destination URL ("+keep+"https only, with a host; an IPv6 literal host is refused; else exit 2)")
	descUsage := "Webhook description"
	if update {
		descUsage = "New description (omit to leave unchanged; an empty value clears it)"
	}
	cmd.Flags().String("description", "", descUsage)
	allowEmpty(cmd, emptyIsSent, "description")
	addProtoDurationFlag(cmd, "callback-timeout", verb+` time to wait for an asynchronous callback, as a protobuf duration (600s) or a Go duration (10m), sent in seconds (`+keep+`0 to 720h; a sign or decimal (-1h, 1.5h) exits 2 before sending; over 720h, update returns "callback_timeout must be between 0 and 30 days" and create "value must be inside range [0s, 720h0m0s]")`)
}

// webhookFields reads the changed field flags into a Webhook body and the
// matching camelCase update-mask paths, in a fixed order.
func webhookFields(cmd *cobra.Command) (map[string]any, []string, error) {
	fields := map[string]any{}
	var paths []string
	if cmd.Flags().Changed("display-name") {
		v, _ := cmd.Flags().GetString("display-name")
		fields["displayName"] = v
		paths = append(paths, "displayName")
	}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		fields["description"] = v
		paths = append(paths, "description")
	}
	if cmd.Flags().Changed("destination-url") {
		v, _ := cmd.Flags().GetString("destination-url")
		if err := validateWebhookURL(v); err != nil {
			return nil, nil, err
		}
		fields["url"] = v
		paths = append(paths, "url")
	}
	if v, ok, err := protoDurationFlag(cmd, "callback-timeout"); err != nil {
		return nil, nil, err
	} else if ok {
		fields["callbackTimeout"] = v
		paths = append(paths, "callbackTimeout")
	}
	return fields, paths, nil
}

// validateWebhookURL refuses anything but an absolute https URL. Create
// rewrites any other scheme (or none) to https and stores an unparseable value
// as a URL that "test" then fails on, so the server can't be relied on here;
// update rejects it with "invalid url: only 'https' scheme is supported".
// An IPv6 literal is stored without its brackets, which "test" can't parse.
func validateWebhookURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return &usageError{fmt.Errorf("--destination-url must be an absolute https:// URL with a host, got %q", redact.URL(raw))}
	}
	if strings.Contains(u.Hostname(), ":") {
		return &usageError{fmt.Errorf("--destination-url must not use an IPv6 literal host, got %q", redact.URL(raw))}
	}
	return nil
}
