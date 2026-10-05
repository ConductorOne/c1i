package cmd

import (
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

Browser login creates a personal client with all of your roles. To limit it,
use --choose-roles (a menu after you approve in the browser; 0 = all roles;
needs a terminal) or --scoped-role <role> (repeatable; a name such as
basic-user, "Basic User" or system:user, or a 27-character role ID). When no
credential is stored for the tenant, a terminal login asks which you want;
Enter keeps all roles and --choose-roles=false skips the question. Scripts are
never asked.

Names are checked right after you approve, so a typo exits 2 before your
credential is created; IDs alone go to the server unchecked. Reading roles
briefly creates a temporary credential, deleted before yours is created. Login
then checks the new credential with C1 and prints its scope.

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

		var loginErr error
		if clientID != "" && clientSecret != "" {
			loginErr = loginWithCredentials(cmd, baseURL, clientID, clientSecret)
		} else if clientID != "" || clientSecret != "" {
			return &usageError{fmt.Errorf("both --client-id and --client-secret are required for credential login")}
		} else {
			loginErr = browserLogin(cmd, in, baseURL, scope)
		}

		// A lone helperLeftError means the login itself succeeded.
		if _, left := loginErr.(*helperLeftError); loginErr != nil && !left {
			return loginErr
		}

		if source != URLSourceConfig && isTerminal() {
			offerSaveURL(cmd, in, baseURL)
		}

		return loginErr
	},
}

func init() {
	authLoginCmd.Flags().String("client-id", "", "C1 API client ID (skip browser login)")
	authLoginCmd.Flags().String("client-secret", "", "C1 API client secret (skip browser login)")
	authLoginCmd.Flags().Bool("choose-roles", false, "Choose the browser-login credential's roles from a menu after approval (needs a terminal); =false skips the question")
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
		_, _ = fmt.Fprintf(out, "Warning: could not save config: %v\n", err)
		return
	}

	_, _ = fmt.Fprintf(out, "URL saved to ~/.c1i.yaml\n")
}

// browserLogin asks, when no credential is stored for the tenant, whether to
// scope the new one, and names the stored credential a re-login replaces.
func browserLogin(cmd *cobra.Command, in *lineReader, baseURL string, scope loginScope) error {
	previous := storedClientID(baseURL)
	if previous == "" && !scope.requested() && isTerminal() {
		var err error
		if scope.choose, err = askToChooseRoles(cmd, in); err != nil {
			return err
		}
	}
	err := loginWithBrowser(cmd, in, baseURL, scope)
	if _, left := err.(*helperLeftError); err != nil && !left {
		return err
	}
	if previous != "" {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "The previous credential (%s) was not revoked.\n", previous)
	}
	return err
}

func loginWithBrowser(cmd *cobra.Command, in *lineReader, baseURL string, scope loginScope) error {
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
		return err
	}

	_, _ = fmt.Fprintf(out, "Opening browser to authenticate...\n\n")
	_, _ = fmt.Fprintf(out, "If your browser does not open, visit:\n  %s\n\n", code.VerificationURI)
	_, _ = fmt.Fprintf(out, "Verify this code matches: %s\n\n", code.UserCode)

	_ = openBrowser(code.VerificationURI)

	_, _ = fmt.Fprintf(out, "Waiting for approval...\n")

	accessToken, err := login.PollForToken(ctx, baseURL, code, opts...)
	if err != nil {
		return err
	}

	// A helper that outlives its lookup doesn't stop the login, but is
	// reported again at the end and fails the command.
	var chosen []menuRole
	var leftover error
	ids := dedupe(scope.roles)
	if scope.choose || needsLookup(scope.roles) {
		var lookup roleLookup
		lookup, leftover, err = lookupRoles(cmd, baseURL, accessToken, opts)
		if err != nil {
			return err
		}
		if scope.choose {
			chosen, err = promptForRoles(cmd, in, lookup.offered)
		} else {
			chosen, err = matchRoles(scope.roles, lookup)
		}
		if err != nil {
			return err
		}
		ids = nil
		for _, r := range chosen {
			ids = append(ids, r.ID)
		}
	}

	pcc := login.PersonalClientOptions{DisplayName: credentialName(ids, chosen), ScopedRoles: ids}
	// Detached from Ctrl-C, like the helper: an interrupted create could commit
	// a credential whose id we never learn.
	createCtx, cancel := detached(ctx)
	creds, err := login.CreatePersonalClient(createCtx, baseURL, accessToken, pcc, opts...)
	cancel()
	if err != nil {
		return withLeftover(err, leftover)
	}
	if err := keepNewCredential(cmd, baseURL, accessToken, opts, creds, ids); err != nil {
		return withLeftover(err, leftover)
	}
	reportScope(cmd, creds.ScopedRoles, chosen)
	return leftover
}

// withLeftover adds a leftover-helper report to a login failure.
func withLeftover(err, leftover error) error {
	if leftover == nil {
		return err
	}
	return errors.Join(err, leftover)
}

// keepNewCredential checks a just-created credential and stores it. If either
// fails it deletes the credential, so nothing unwanted or non-expiring is
// left behind.
func keepNewCredential(cmd *cobra.Command, baseURL, accessToken string, opts []transport.Option, creds *login.Credentials, requested []string) error {
	problem := checkNewCredential(cmd, baseURL, creds, requested)
	if problem == nil {
		problem = storeCredential(cmd, baseURL, creds.ClientID, creds.ClientSecret)
	}
	if problem == nil {
		return nil
	}
	return fmt.Errorf("%w; %s", problem, discardCredential(cmd, baseURL, accessToken, opts, creds))
}

// checkNewCredential confirms a credential is usable and has the scope that
// was asked for.
func checkNewCredential(cmd *cobra.Command, baseURL string, creds *login.Credentials, requested []string) error {
	if err := cmd.Context().Err(); err != nil {
		return err
	}
	if !sameRoles(requested, creds.ScopedRoles) {
		return &nonJSONResponseError{fmt.Errorf("C1 scoped the new credential to %v, not the requested %v", creds.ScopedRoles, requested)}
	}
	c, err := newCredentialClient(cmd, baseURL, creds.ClientID, creds.ClientSecret)
	if err != nil {
		return fmt.Errorf("credential verification failed: %w", err)
	}
	introspect, err := c.Get(cmd.Context(), "/api/v1/auth/introspect", nil)
	if len(requested) > 0 && hasNoAccess(introspect, err) {
		return &usageError{fmt.Errorf("those roles give the credential no access: a scoped credential keeps only the overlap between its roles and your own")}
	}
	if err != nil {
		return fmt.Errorf("credential verification failed: %w", err)
	}
	return nil
}

// discardCredential deletes a credential login won't keep, and says whether it
// worked. One scoped to nothing can't delete itself, so a temporary helper
// does it instead.
func discardCredential(cmd *cobra.Command, baseURL, accessToken string, opts []transport.Option, creds *login.Credentials) string {
	if c, err := newCredentialClient(cmd, baseURL, creds.ClientID, creds.ClientSecret); err == nil {
		if deletePersonalClient(cmd.Context(), c, creds.ID) == nil {
			return "the new credential was deleted"
		}
	}
	_, err := withHelper(cmd, baseURL, accessToken, opts, func(h *client.Client) error {
		return deletePersonalClient(cmd.Context(), h, creds.ID)
	})
	if err == nil {
		return "the new credential was deleted"
	}
	return fmt.Sprintf("the new credential (%s) could not be deleted, so delete it under your personal clients in C1.ai", creds.ID)
}

func loginWithCredentials(cmd *cobra.Command, baseURL, clientID, clientSecret string) error {
	if err := verifyCredential(cmd, baseURL, clientID, clientSecret); err != nil {
		return err
	}
	return storeCredential(cmd, baseURL, clientID, clientSecret)
}

// verifyCredential proves credentials work before they replace a stored one.
func verifyCredential(cmd *cobra.Command, baseURL, clientID, clientSecret string) error {
	c, err := newCredentialClient(cmd, baseURL, clientID, clientSecret)
	if err != nil {
		return fmt.Errorf("credential verification failed: %w", err)
	}
	if _, err := c.Get(cmd.Context(), "/api/v1/auth/introspect", nil); err != nil {
		return fmt.Errorf("credential verification failed: %w", err)
	}
	return nil
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
