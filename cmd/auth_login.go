package cmd

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/ductone/c1i/internal/client"
	"github.com/ductone/c1i/internal/config"
	"github.com/ductone/c1i/internal/keychain"
	"github.com/ductone/c1i/internal/login"
	"github.com/spf13/cobra"
)

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate to ConductorOne via browser or API credentials",
	Long: `Authenticate to ConductorOne. By default, opens your browser for OAuth device flow login.
Alternatively, pass --client-id and --client-secret to store credentials directly.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		baseURL, err := GetBaseURL()
		if err != nil {
			return err
		}

		clientID, _ := cmd.Flags().GetString("client-id")
		clientSecret, _ := cmd.Flags().GetString("client-secret")

		if clientID != "" && clientSecret != "" {
			return loginWithCredentials(cmd, baseURL, clientID, clientSecret)
		}

		if clientID != "" || clientSecret != "" {
			return fmt.Errorf("both --client-id and --client-secret are required for credential login")
		}

		return loginWithBrowser(cmd, baseURL)
	},
}

func init() {
	authLoginCmd.Flags().String("client-id", "", "ConductorOne API client ID (skip browser login)")
	authLoginCmd.Flags().String("client-secret", "", "ConductorOne API client secret (skip browser login)")
	authCmd.AddCommand(authLoginCmd)
}

func loginWithBrowser(cmd *cobra.Command, baseURL string) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	code, err := login.StartDeviceFlow(ctx, baseURL)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Opening browser to authenticate...\n\n")
	fmt.Fprintf(out, "If your browser does not open, visit:\n  %s\n\n", code.VerificationURI)
	fmt.Fprintf(out, "Verify this code matches: %s\n\n", code.UserCode)

	_ = openBrowser(code.VerificationURI)

	fmt.Fprintf(out, "Waiting for approval...\n")

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
	if err := keychain.Store(service, clientID, clientSecret); err != nil {
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

	fmt.Fprintf(cmd.OutOrStdout(), "Credentials stored and verified for %s.\n", baseURL)
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
