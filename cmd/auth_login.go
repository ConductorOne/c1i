package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
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

Browser login mints a personal client that inherits all of your roles. The
first browser login to a tenant in a terminal asks whether to keep that or
choose roles; --choose-roles chooses on any login. Choosing shows a menu after
you approve in the browser (0 = full permissions); c1i reads the roles with a
temporary credential and deletes it before creating yours. --scoped-role
<role-id> (repeatable) names roles up front, for scripts; an unknown id fails
with 404 after approval.

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
		if (clientID != "" || clientSecret != "") && scope.requested() {
			return &usageError{fmt.Errorf("--choose-roles, --scoped-role and --display-name apply only to browser login, not --client-id/--client-secret")}
		}
		if scope.choose && !isTerminal() {
			return &usageError{fmt.Errorf("--choose-roles needs an interactive terminal; pass --scoped-role instead")}
		}

		baseURL, source, err := GetBaseURLWithSource()
		if err != nil {
			return err
		}

		if baseURL == "" {
			if !isTerminal() {
				return &usageError{fmt.Errorf("url is required: set --url flag, C1I_URL env var, or url in ~/.c1i.yaml")}
			}
			baseURL, err = promptForURL(cmd, os.Stdin)
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
			loginErr = browserLogin(cmd, baseURL, scope)
		}

		if loginErr != nil {
			return loginErr
		}

		if source != URLSourceConfig && isTerminal() {
			return offerSaveURL(cmd, baseURL)
		}

		return nil
	},
}

func init() {
	authLoginCmd.Flags().String("client-id", "", "C1 API client ID (skip browser login)")
	authLoginCmd.Flags().String("client-secret", "", "C1 API client secret (skip browser login)")
	authLoginCmd.Flags().Bool("choose-roles", false, "Choose the browser-login credential's roles from a menu after approval (needs a terminal)")
	addRepeatableStringFlag(authLoginCmd, "scoped-role", "Restrict the browser-login credential to a role ID (repeatable; see c1i roles list)")
	authLoginCmd.Flags().String("display-name", "", "Name for the browser-login credential (default \""+login.DefaultDisplayName+"\")")
	authCmd.AddCommand(authLoginCmd)
}

// isTerminal reports whether stdin is an interactive terminal. It gates every
// login prompt, so it must be false under a redirect:
// os.ModeCharDevice alone is not enough, as /dev/null is also a character
// device -- term.IsTerminal issues the TTY ioctl that tells them apart.
// A var so tests can drive the interactive paths.
var isTerminal = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// promptForURL reads a URL interactively from in (os.Stdin in production;
// injectable for tests).
func promptForURL(cmd *cobra.Command, in io.Reader) (string, error) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Enter your C1 URL (e.g. mycompany.conductor.one or mycompany.c1eu.ai): ")

	line, err := scanLine(cmd.Context(), bufio.NewScanner(in))
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

// offerSaveURL asks to save baseURL as the default. Ctrl-C saves nothing and
// is returned; EOF just declines.
func offerSaveURL(cmd *cobra.Command, baseURL string) error {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Save %s as default URL in ~/.c1i.yaml? [Y/n] ", baseURL)

	line, err := scanLine(cmd.Context(), bufio.NewScanner(os.Stdin))
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}

	answer := strings.TrimSpace(strings.ToLower(line))
	if answer != "" && answer != "y" && answer != "yes" {
		return nil
	}

	if err := config.SaveToConfigFile("url", baseURL); err != nil {
		_, _ = fmt.Fprintf(out, "Warning: could not save config: %v\n", err)
		return nil
	}

	_, _ = fmt.Fprintf(out, "URL saved to ~/.c1i.yaml\n")
	return nil
}

// browserLogin asks on a tenant's first interactive login whether to scope the
// credential, and names the credential a re-login replaces.
func browserLogin(cmd *cobra.Command, baseURL string, scope loginScope) error {
	previous := storedClientID(baseURL)
	if !scope.choose && len(scope.roles) == 0 && previous == "" && isTerminal() {
		var err error
		if scope.choose, err = askToChooseRoles(cmd, os.Stdin); err != nil {
			return err
		}
	}
	if err := loginWithBrowser(cmd, baseURL, scope); err != nil {
		return err
	}
	if previous != "" {
		_, _ = fmt.Fprint(cmd.OutOrStdout(), previousCredentialNote(previous))
	}
	return nil
}

func loginWithBrowser(cmd *cobra.Command, baseURL string, scope loginScope) error {
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

	pcc := login.PersonalClientOptions{DisplayName: scope.displayName, ScopedRoles: scope.roles}
	var chosen []menuRole
	if scope.choose {
		chosen, err = chooseRoles(cmd, baseURL, accessToken, opts)
		if err != nil {
			return err
		}
		for _, r := range chosen {
			pcc.ScopedRoles = append(pcc.ScopedRoles, r.ID)
		}
	}

	creds, err := login.CreatePersonalClient(ctx, baseURL, accessToken, pcc, opts...)
	if err != nil {
		var apiErr *client.APIError
		switch {
		case len(pcc.ScopedRoles) > 0 && exitCode(err) == exitNotFound:
			return fmt.Errorf("%w (a scoped role id may not exist; c1i roles list shows them once you are logged in, or use --choose-roles)", err)
		case scope.choose && errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized:
			return fmt.Errorf("%w (the browser approval may have expired while the menu was open; run c1i auth login again)", err)
		}
		return err
	}

	if err := storeAndVerify(cmd, baseURL, creds.ClientID, creds.ClientSecret); err != nil {
		return err
	}
	reportScope(cmd, pcc.ScopedRoles, chosen)
	return nil
}

func loginWithCredentials(cmd *cobra.Command, baseURL, clientID, clientSecret string) error {
	return storeAndVerify(cmd, baseURL, clientID, clientSecret)
}

func storeAndVerify(cmd *cobra.Command, baseURL, clientID, clientSecret string) error {
	service := config.KeychainService(baseURL)
	backend, err := keychain.Store(service, clientID, clientSecret)
	if err != nil {
		return fmt.Errorf("failed to store credentials: %w", err)
	}

	c, err := newClient(cmd, baseURL)
	if err != nil {
		_, _ = keychain.Delete(service)
		return fmt.Errorf("credentials stored but verification failed: %w", err)
	}

	// Introspect works under any role scope; a narrowly scoped credential
	// can't call most other endpoints.
	if _, err := c.Get(cmd.Context(), "/api/v1/auth/introspect", nil); err != nil {
		_, _ = keychain.Delete(service)
		return fmt.Errorf("credentials stored but API test failed: %w", err)
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
