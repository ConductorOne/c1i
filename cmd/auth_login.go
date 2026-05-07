package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/config"
	"github.com/ConductorOne/c1i/internal/keychain"
	"github.com/ConductorOne/c1i/internal/login"
	"github.com/spf13/cobra"
)

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate to C1 via browser or API credentials",
	Long: `Authenticate to C1. By default, opens your browser for OAuth device flow login.
Alternatively, pass --client-id and --client-secret to store credentials directly.

Credentials are stored in the OS keyring when available, otherwise as a 0600
file under your config directory. For non-interactive / CI use, you can skip
storage entirely and pass credentials each invocation via the C1I_CLIENT_ID
and C1I_CLIENT_SECRET environment variables (combined with C1I_URL).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, source := GetBaseURLWithSource(cmd)

		if baseURL == "" {
			if !isTerminal() {
				return fmt.Errorf("url is required: set --url flag, C1I_URL env var, or url in ~/.c1i.yaml")
			}
			var err error
			baseURL, err = promptForURL(cmd)
			if err != nil {
				return err
			}
		}

		clientID, _ := cmd.Flags().GetString("client-id")
		clientSecret, _ := cmd.Flags().GetString("client-secret")

		var loginErr error
		if clientID != "" && clientSecret != "" {
			loginErr = loginWithCredentials(cmd, baseURL, clientID, clientSecret)
		} else if clientID != "" || clientSecret != "" {
			return fmt.Errorf("both --client-id and --client-secret are required for credential login")
		} else {
			loginErr = loginWithBrowser(cmd, baseURL)
		}

		if loginErr != nil {
			return loginErr
		}

		if source != URLSourceConfig && isTerminal() {
			offerSaveURL(cmd, baseURL)
		}

		return nil
	},
}

func init() {
	authLoginCmd.Flags().String("client-id", "", "C1 API client ID (skip browser login)")
	authLoginCmd.Flags().String("client-secret", "", "C1 API client secret (skip browser login)")
	authCmd.AddCommand(authLoginCmd)
}

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func promptForURL(cmd *cobra.Command) (string, error) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Enter your C1 URL (e.g. mycompany.conductor.one or mycompany): ")

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return "", fmt.Errorf("url is required: set --url flag, C1I_URL env var, or url in ~/.c1i.yaml")
	}

	raw := strings.TrimSpace(scanner.Text())
	if raw == "" {
		return "", fmt.Errorf("url is required: set --url flag, C1I_URL env var, or url in ~/.c1i.yaml")
	}

	return config.ParseURL(raw), nil
}

func offerSaveURL(cmd *cobra.Command, baseURL string) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Save %s as default URL in ~/.c1i.yaml? [Y/n] ", baseURL)

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return
	}

	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	if answer != "" && answer != "y" && answer != "yes" {
		return
	}

	if err := config.SaveToConfigFile("url", baseURL); err != nil {
		_, _ = fmt.Fprintf(out, "Warning: could not save config: %v\n", err)
		return
	}

	_, _ = fmt.Fprintf(out, "URL saved to ~/.c1i.yaml\n")
}

func loginWithBrowser(cmd *cobra.Command, baseURL string) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	code, err := login.StartDeviceFlow(ctx, baseURL)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "Opening browser to authenticate...\n\n")
	_, _ = fmt.Fprintf(out, "If your browser does not open, visit:\n  %s\n\n", code.VerificationURI)
	_, _ = fmt.Fprintf(out, "Verify this code matches: %s\n\n", code.UserCode)

	_ = openBrowser(code.VerificationURI)

	_, _ = fmt.Fprintf(out, "Waiting for approval...\n")

	creds, err := login.PollForToken(ctx, baseURL, code)
	if err != nil {
		return err
	}

	return storeAndVerify(cmd, baseURL, creds.ClientID, creds.ClientSecret)
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

	c, err := client.New(cmd.Context(), baseURL)
	if err != nil {
		_ = keychain.Delete(service)
		return fmt.Errorf("credentials stored but verification failed: %w", err)
	}

	body := map[string]any{"pageSize": 1}
	if _, err := c.Post(cmd.Context(), "/api/v1/search/users", body); err != nil {
		_ = keychain.Delete(service)
		return fmt.Errorf("credentials stored but API test failed: %w", err)
	}

	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Credentials stored and verified for %s.\n", baseURL)
	if backend == keychain.BackendFile {
		path, _ := keychain.FilePath(service)
		_, _ = fmt.Fprintf(out, "Note: no OS keyring available — credentials saved as a 0600 file at %s\n", path)
	}
	return nil
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	default:
		return nil
	}
}
