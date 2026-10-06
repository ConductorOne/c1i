package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/config"
	"github.com/ConductorOne/c1i/internal/keychain"
	"github.com/ConductorOne/c1i/internal/login"
	"github.com/ConductorOne/c1i/internal/transport"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate to C1 via browser or API credentials",
	Long: `Authenticate to C1. By default, opens your browser for OAuth device flow login.
Alternatively, pass --client-id and --client-secret to store credentials directly.

Credentials are stored in the OS keyring when available, otherwise as a 0600
file under your config directory. For non-interactive / CI use, you can skip
storage entirely and pass credentials each invocation via the C1I_CLIENT_ID
and C1I_CLIENT_SECRET environment variables (combined with C1I_URL).

Browser login creates a personal client with all of your roles. To limit it:

  --choose-roles         pick roles from a menu after you approve in the browser
  --scoped-role <role>   name a role (repeatable): a role ID, or a name such as
                         basic-user, "Basic User" or system:user

A terminal login with no stored credential asks which you want; Enter keeps all
of your roles and --choose-roles=false skips the question. --choose-roles and
--scoped-role can't be combined.

A name that matches no role you can scope to exits 2 before your credential is
created; role IDs go to C1 as given. Login checks the new credential before
storing it and deletes it if the check fails. README's Authentication section
has the full rules and exit codes.

If a previous login used a mixed-case URL and commands now report "not
authenticated", re-run this command: the keychain key is derived from a
lower-cased host, so a credential stored under the old mixed-case key is no
longer found.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		scope, err := loginScopeFromFlags(cmd)
		if err != nil {
			return err
		}
		clientID, _ := cmd.Flags().GetString("client-id")
		clientSecret, _ := cmd.Flags().GetString("client-secret")
		if (clientID != "" || clientSecret != "") && (scope.choose || len(scope.roles) > 0) {
			return &usageError{fmt.Errorf("--choose-roles and --scoped-role apply only to browser login, not --client-id/--client-secret")}
		}
		if scope.choose && !isTerminal() {
			return &usageError{fmt.Errorf("--choose-roles needs an interactive terminal; pass --scoped-role instead")}
		}

		baseURL, source, err := GetBaseURLWithSource()
		if err != nil {
			return err
		}

		// One reader for every prompt: separate buffered readers on stdin would
		// each swallow input meant for the next.
		in := newLineReader(os.Stdin)
		if baseURL == "" {
			if !isTerminal() {
				return &usageError{fmt.Errorf("url is required: set --url flag, C1I_URL env var, or url in ~/.c1i.yaml")}
			}
			baseURL, err = promptForURL(cmd, in)
			if err != nil {
				return err
			}
		}

		var loginErr, leftover error
		if clientID != "" && clientSecret != "" {
			loginErr = loginWithCredentials(cmd, baseURL, clientID, clientSecret)
		} else if clientID != "" || clientSecret != "" {
			return &usageError{fmt.Errorf("both --client-id and --client-secret are required for credential login")}
		} else {
			leftover, loginErr = browserLogin(cmd, in, baseURL, scope)
		}
		if loginErr != nil {
			return joinLeftover(loginErr, leftover)
		}

		noteEnvCredentials(cmd)
		if source != URLSourceConfig && isTerminal() {
			offerSaveURL(cmd, in, baseURL)
		}

		// The login stored its credential; a helper it couldn't delete still
		// fails the command.
		return leftover
	},
}

func init() {
	authLoginCmd.Flags().String("client-id", "", "C1 API client ID (skip browser login)")
	authLoginCmd.Flags().String("client-secret", "", "C1 API client secret (skip browser login)")
	authLoginCmd.Flags().Bool("choose-roles", false, "Choose the browser-login credential's roles from a menu after approval (needs a terminal); =false skips the role question")
	addRepeatableStringFlag(authLoginCmd, "scoped-role", "Restrict the browser-login credential to a role, by name (e.g. basic-user) or ID (repeatable)")
	authCmd.AddCommand(authLoginCmd)
}

// isTerminal reports whether stdin is an interactive terminal. It gates every
// login prompt, so it must be false under a redirect: os.ModeCharDevice alone
// is not enough, as /dev/null is also a character device -- term.IsTerminal
// issues the TTY ioctl that tells them apart. A var so tests can stub it.
var isTerminal = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// promptForURL reads a URL interactively from in.
func promptForURL(cmd *cobra.Command, in *lineReader) (string, error) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Enter your C1 URL (e.g. mycompany.conductor.one or mycompany.c1eu.ai): ")

	line, err := in.readLine(cmd.Context())
	if errors.Is(err, io.EOF) {
		return "", &usageError{fmt.Errorf("url is required: set --url flag, C1I_URL env var, or url in ~/.c1i.yaml")}
	}
	if err != nil {
		return "", err
	}

	raw := strings.TrimSpace(line)
	if raw == "" {
		return "", &usageError{fmt.Errorf("url is required: set --url flag, C1I_URL env var, or url in ~/.c1i.yaml")}
	}

	url, warnings, err := config.ParseURL(raw)
	if err != nil {
		return "", &usageError{fmt.Errorf("%w (from interactive login prompt)", err)}
	}
	warnAboutURL(warnings)
	return url, nil
}

// offerSaveURL asks to save baseURL as the default. EOF or Ctrl-C declines:
// the login has already succeeded.
func offerSaveURL(cmd *cobra.Command, in *lineReader, baseURL string) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Save %s as default URL in ~/.c1i.yaml? [Y/n] ", baseURL)

	line, err := in.readLine(cmd.Context())
	if err != nil {
		_, _ = fmt.Fprintln(out)
		return
	}

	answer := strings.TrimSpace(strings.ToLower(line))
	if answer != "" && answer != "y" && answer != "yes" {
		return
	}

	if err := config.SaveToConfigFile("url", baseURL); err != nil {
		_, _ = fmt.Fprintf(out, "Warning: the URL was not saved: %v\n", err)
		return
	}

	_, _ = fmt.Fprintf(out, "URL saved to ~/.c1i.yaml\n")
}

// noteEnvCredentials warns that env credentials override the stored login.
func noteEnvCredentials(cmd *cobra.Command) {
	if keychain.EnvCredentialsSet() {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Note: C1I_CLIENT_ID/C1I_CLIENT_SECRET are set in your environment and take precedence over this login; unset them to use it.")
	}
}

// joinLeftover adds a helper credential login couldn't delete to its error.
func joinLeftover(err, leftover error) error {
	if leftover == nil {
		return err
	}
	return errors.Join(err, leftover)
}

// browserLogin asks, when no credential is stored for the tenant, whether to
// scope the new one, and names the stored credential this login leaves active
// in C1. leftover reports a helper credential it couldn't delete.
func browserLogin(cmd *cobra.Command, in *lineReader, baseURL string, scope loginScope) (leftover, err error) {
	previous := storedClientID(baseURL)
	if previous == "" && !scope.requested() && isTerminal() {
		if scope.choose, err = askToChooseRoles(cmd, in); err != nil {
			return nil, err
		}
	}
	leftover, err = loginWithBrowser(cmd, in, baseURL, scope)
	switch {
	case previous == "":
	case err == nil:
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Your previous credential (client id %s) is still active in C1.ai.\n", previous)
	case storedClientID(baseURL) == "":
		// keychain.Store clears the old entry before writing the new one.
		err = fmt.Errorf("%w; your previous credential (client id %s) is no longer stored here but is still active in C1.ai", err, previous)
	}
	return leftover, err
}

func loginWithBrowser(cmd *cobra.Command, in *lineReader, baseURL string, scope loginScope) (leftover, err error) {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	// The same --debug/--max-retries flags every REST command reads, so a
	// hung or flaky auth host traces and retries like the rest of the CLI
	// instead of silently hanging or giving up after one try.
	opts := []transport.Option{
		transport.WithMaxRetries(viper.GetInt("max_retries")),
		transport.WithDebug(viper.GetBool("debug")),
	}

	code, err := login.StartDeviceFlow(ctx, baseURL, opts...)
	if err != nil {
		return nil, err
	}

	_, _ = fmt.Fprintf(out, "Opening browser to authenticate...\n\n")
	_, _ = fmt.Fprintf(out, "If your browser does not open, visit:\n  %s\n\n", code.VerificationURI)
	_, _ = fmt.Fprintf(out, "Verify this code matches: %s\n\n", code.UserCode)

	_ = openBrowser(code.VerificationURI)

	_, _ = fmt.Fprintf(out, "Waiting for approval...\n")

	accessToken, err := login.PollForToken(ctx, baseURL, code, opts...)
	if err != nil {
		return nil, err
	}

	ids, names := splitScopedRoles(scope.roles)
	var named []menuRole
	if scope.choose || len(names) > 0 {
		var offered []menuRole
		leftover, err = withHelper(ctx, cmd, baseURL, accessToken, opts, func(c *client.Client) error {
			var rerr error
			offered, rerr = readRoles(ctx, c)
			return rerr
		})
		if err != nil {
			return leftover, err
		}
		if scope.choose {
			named, err = promptForRoles(cmd, in, offered)
		} else {
			named, err = matchRoles(names, offered)
		}
		if err != nil {
			return leftover, err
		}
		for _, r := range named {
			ids = append(ids, r.ID)
		}
		ids = dedupe(ids)
	}
	pcc := login.PersonalClientOptions{DisplayName: credentialName(ids, named), ScopedRoles: ids}
	creds, err := login.CreatePersonalClient(ctx, baseURL, accessToken, pcc, opts...)
	if err != nil {
		return leftover, err
	}
	if err := keepNewCredential(cmd, baseURL, accessToken, opts, creds, ids); err != nil {
		return leftover, err
	}
	reportScope(cmd, creds.ScopedRoles, named)
	return leftover, nil
}

// keepNewCredential checks a just-created credential and stores it. If either
// fails, the credential is deleted so nothing unwanted is left behind.
func keepNewCredential(cmd *cobra.Command, baseURL, accessToken string, opts []transport.Option, creds *login.Credentials, requested []string) error {
	c, introspect, problem := verifyCredential(cmd, baseURL, creds.ClientID, creds.ClientSecret)
	switch {
	case !scopeWithin(requested, creds.ScopedRoles):
		problem = &nonJSONResponseError{fmt.Errorf("C1 scoped the new credential to %s, not the requested %s", printable(strings.Join(creds.ScopedRoles, ", ")), strings.Join(requested, ", "))}
	case len(requested) > 0 && hasNoAccess(introspect, problem):
		problem = &usageError{fmt.Errorf("the requested roles give the credential no access: a scoped credential keeps only the overlap between its roles and your own")}
	}
	if problem == nil {
		problem = storeCredential(cmd, baseURL, creds.ClientID, creds.ClientSecret)
	}
	if problem == nil {
		return nil
	}
	if errors.Is(problem, context.Canceled) {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Deleting the new credential (%s)...\n", printable(creds.ID))
	}
	outcome, leftover := discardCredential(cmd, baseURL, accessToken, opts, c, creds)
	return joinLeftover(fmt.Errorf("%w; %s", problem, outcome), leftover)
}

// discardCredential deletes a credential login won't keep, using c when it
// works and otherwise a temporary helper (one scoped to nothing can't delete
// itself). It returns what happened, and a helper it couldn't delete.
func discardCredential(cmd *cobra.Command, baseURL, accessToken string, opts []transport.Option, c *client.Client, creds *login.Credentials) (outcome string, leftover error) {
	const deleted = "the new credential was deleted"
	if c != nil && deletePersonalClient(cmd.Context(), c, creds.ID) == nil {
		return deleted, nil
	}
	// Cleanup runs even after Ctrl-C.
	ctx, cancel := detached(cmd.Context())
	defer cancel()
	leftover, err := withHelper(ctx, cmd, baseURL, accessToken, opts, func(h *client.Client) error {
		return deletePersonalClient(ctx, h, creds.ID)
	})
	if err != nil {
		return fmt.Sprintf("the new credential (%s) could not be deleted, so delete it under your personal clients in C1.ai", printable(creds.ID)), leftover
	}
	return deleted, leftover
}

func loginWithCredentials(cmd *cobra.Command, baseURL, clientID, clientSecret string) error {
	if _, _, err := verifyCredential(cmd, baseURL, clientID, clientSecret); err != nil {
		return err
	}
	return storeCredential(cmd, baseURL, clientID, clientSecret)
}

// verifyCredential proves credentials work before they replace a stored one,
// returning their client (nil if it couldn't be built) and introspect body.
func verifyCredential(cmd *cobra.Command, baseURL, clientID, clientSecret string) (*client.Client, []byte, error) {
	c, err := newCredentialClient(cmd, baseURL, clientID, clientSecret)
	if err != nil {
		return nil, nil, fmt.Errorf("credential verification failed: %w", err)
	}
	introspect, err := c.Get(cmd.Context(), "/api/v1/auth/introspect", nil)
	if err != nil {
		return c, nil, fmt.Errorf("credential verification failed: %w", err)
	}
	return c, introspect, nil
}

func storeCredential(cmd *cobra.Command, baseURL, clientID, clientSecret string) error {
	service := config.KeychainService(baseURL)
	backend, err := keychain.Store(service, clientID, clientSecret)
	if err != nil {
		return fmt.Errorf("failed to store credentials: %w", err)
	}

	out := cmd.OutOrStdout()
	if backend == keychain.BackendFile {
		path, _ := keychain.FilePath(service)
		_, _ = fmt.Fprintf(out, "Credentials verified and saved for %s.\n", baseURL)
		_, _ = fmt.Fprintf(out, "No OS keyring available — stored as a 0600 file at %s\n", path)
	} else {
		_, _ = fmt.Fprintf(out, "Credentials verified and stored in the %s for %s.\n", keyringName(), baseURL)
	}
	return nil
}

// openBrowser is a var so tests can drive the device flow without launching one.
var openBrowser = func(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start() // #nosec G204 -- no shell is invoked; opening the login URL is the point
	case "linux":
		return exec.Command("xdg-open", url).Start() // #nosec G204 -- no shell is invoked; opening the login URL is the point
	default:
		return nil
	}
}
