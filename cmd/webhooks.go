package cmd

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ConductorOne/c1i/internal/client"
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

// webhookFieldFlags registers the writable Webhook fields shared by create and
// update.
func webhookFieldFlags(cmd *cobra.Command, verb string) {
	cmd.Flags().String("display-name", "", verb+" display name, 3-100 characters")
	cmd.Flags().String("destination-url", "", verb+` destination URL; must be an absolute https:// URL with a host name or IPv4 address (an IPv6 literal later fails "test" with "invalid port ... after host")`)
	cmd.Flags().String("description", "", verb+" description (an empty value clears it on update)")
	cmd.Flags().String("callback-timeout", "", verb+` time to wait for an asynchronous callback, as a Go duration (e.g. 10m); 0 to 720h; over that, update returns "callback_timeout must be between 0 and 30 days" (create: "value must be inside range [0s, 720h0m0s]")`)
}

// webhookFields reads the changed field flags into a Webhook body and the
// matching camelCase update-mask paths, in a fixed order.
func webhookFields(cmd *cobra.Command) (map[string]any, []string, error) {
	fields := map[string]any{}
	var paths []string
	if cmd.Flags().Changed("display-name") {
		v, err := requireNonEmptyIfSet(cmd, "display-name")
		if err != nil {
			return nil, nil, err
		}
		// Update stores any length, so the create-side rule is applied to both.
		if n := utf8.RuneCountInString(v); n < 3 || n > 100 {
			return nil, nil, &usageError{fmt.Errorf("--display-name must be 3-100 characters, got %d (the server's create rule: \"value length must be between 3 and 100 runes, inclusive\")", n)}
		}
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
	if cmd.Flags().Changed("callback-timeout") {
		v, _ := cmd.Flags().GetString("callback-timeout")
		d, err := time.ParseDuration(strings.TrimSpace(v))
		if err != nil {
			return nil, nil, &usageError{fmt.Errorf("invalid --callback-timeout %q: %w", v, err)}
		}
		if d < 0 {
			return nil, nil, &usageError{fmt.Errorf("--callback-timeout must not be negative, got %q", v)}
		}
		fields["callbackTimeout"] = formatProtoJSONDuration(d)
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
		return &usageError{fmt.Errorf("--destination-url must be an absolute https:// URL with a host, got %q", raw)}
	}
	if strings.Contains(u.Hostname(), ":") {
		return &usageError{fmt.Errorf("--destination-url must not use an IPv6 literal host, got %q", raw)}
	}
	return nil
}
