package cmd

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/ConductorOne/c1i/internal/selfupdate"
	"github.com/ConductorOne/c1i/internal/transport"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var upgradeChannels = map[string]bool{"stable": true, "latest": true, "preview": true}

var upgradeCmd = &cobra.Command{
	Use:     "upgrade",
	Aliases: []string{"update"},
	Short:   "Upgrade c1i to the latest release from the C1 distribution center",
	Long: `Check for and install a newer c1i release.

Release metadata comes from the C1 distribution center
(dist.conductorone.com): the "stable" channel by default, with "latest" and
"preview" available via --channel. The downloaded artifact is verified against
the release manifest's SHA-256 before anything is replaced.

Only a standalone downloaded binary is replaced in place. If c1i was installed
with Homebrew, "go install", or is running as a container image, upgrade prints
the right command for that install method instead of self-replacing.

  c1i upgrade            # upgrade to the latest stable release (asks first)
  c1i upgrade --check    # report whether a newer release is available; change nothing
  c1i upgrade --channel latest -y   # take the newest release without prompting`,
	RunE: func(cmd *cobra.Command, args []string) error {
		channel, _ := cmd.Flags().GetString("channel")
		if !upgradeChannels[channel] {
			return &usageError{fmt.Errorf("unknown --channel %q: expected stable, latest, or preview", channel)}
		}
		checkOnly, _ := cmd.Flags().GetBool("check")
		assumeYes, _ := cmd.Flags().GetBool("yes")
		out := cmd.OutOrStdout()

		client := &selfupdate.Client{HTTP: newUpgradeDoer()}

		idx, err := client.Index(cmd.Context())
		if err != nil {
			return &upstreamError{fmt.Errorf("reading release channels: %w", err)}
		}
		target := idx.Channels[channel]
		if target == "" {
			return &upstreamError{fmt.Errorf("the distribution center lists no %q channel", channel)}
		}
		if e, ok := idx.Semvers[target]; ok && e.Yanked {
			return &upstreamError{fmt.Errorf("the %q channel points at %s, which has been yanked; try again later", channel, target)}
		}

		current := Version
		if !isReleaseVersion(current) {
			_, _ = fmt.Fprintf(out, "c1i is a development build (version %q); `c1i upgrade` works on released binaries.\n", current)
			_, _ = fmt.Fprintf(out, "The current %s release is %s.\n", channel, target)
			return nil
		}

		cmp, ok := selfupdate.CompareVersions(current, target)
		switch {
		case !ok:
			return &upstreamError{fmt.Errorf("cannot compare current version %q with %s", current, target)}
		case cmp == 0:
			_, _ = fmt.Fprintf(out, "c1i %s is already the latest %s release.\n", current, channel)
			return nil
		case cmp > 0:
			_, _ = fmt.Fprintf(out, "c1i %s is newer than the %s channel (%s); nothing to do.\n", current, channel, target)
			if channel == "stable" {
				_, _ = fmt.Fprintln(out, "(Pass --channel latest to track the newest release.)")
			}
			return nil
		}

		// cmp < 0: an upgrade is available.
		_, _ = fmt.Fprintf(out, "A newer %s release is available: %s -> %s.\n", channel, current, target)
		if checkOnly {
			return nil
		}

		execPath, err := selfupdate.ExecutablePath()
		if err != nil {
			return fmt.Errorf("locating the running binary: %w", err)
		}
		method, hint := selfupdate.Detect(execPath, runtime.GOOS)
		if method != selfupdate.Standalone {
			_, _ = fmt.Fprintf(out, "Not upgrading in place: %s\n", hint)
			return nil
		}

		entry, ok := idx.Semvers[target]
		if !ok || entry.Manifest == "" {
			return &upstreamError{fmt.Errorf("no manifest listed for %s", target)}
		}
		manifest, err := client.Manifest(cmd.Context(), entry.Manifest)
		if err != nil {
			return &upstreamError{fmt.Errorf("reading the %s manifest: %w", target, err)}
		}
		asset, ok := manifest.Assets[selfupdate.PlatformKey()]
		if !ok {
			return &upstreamError{fmt.Errorf("%s has no build for %s", target, selfupdate.PlatformKey())}
		}

		if dryRunActive() {
			_, _ = fmt.Fprintf(out, "[dry-run] would download %s\n", asset.Href)
			_, _ = fmt.Fprintf(out, "[dry-run] would verify sha256 %s and replace %s\n", asset.SHA256, execPath)
			return nil
		}

		if !assumeYes {
			ok, err := confirm(cmd, fmt.Sprintf("Upgrade c1i %s -> %s?", current, target))
			if err != nil {
				return err
			}
			if !ok {
				_, _ = fmt.Fprintln(out, "Upgrade cancelled.")
				return nil
			}
		}

		_, _ = fmt.Fprintf(out, "Downloading %s...\n", asset.Filename)
		if err := client.Apply(cmd.Context(), asset, execPath); err != nil {
			return &upstreamError{fmt.Errorf("applying upgrade: %w", err)}
		}
		_, _ = fmt.Fprintf(out, "Upgraded c1i %s -> %s.\n", current, target)
		return nil
	},
}

// newUpgradeDoer builds the transport the self-updater fetches through. A var
// so a test can inject a fake dist server; production threads --max-retries and
// --debug like every other network path.
var newUpgradeDoer = func() selfupdate.Doer {
	return transport.New(nil,
		transport.WithMaxRetries(viper.GetInt("max_retries")),
		transport.WithDebug(viper.GetBool("debug")),
	)
}

func init() {
	upgradeCmd.Flags().Bool("check", false, "Report whether a newer release is available; change nothing")
	upgradeCmd.Flags().String("channel", "stable", "Release channel: stable, latest, or preview")
	upgradeCmd.Flags().BoolP("yes", "y", false, "Skip the confirmation prompt")
	rootCmd.AddCommand(upgradeCmd)
}

// isReleaseVersion reports whether Version looks like a real release tag
// (vMAJOR.MINOR.PATCH). A `go run`/source build reports "dev" (or "(devel)"),
// which cannot be compared or upgraded from.
func isReleaseVersion(v string) bool {
	_, ok := selfupdate.CompareVersions(v, v)
	return ok
}

// confirm asks a yes/no question. It requires --yes when stdin is not a
// terminal, so a non-interactive run never blocks or silently proceeds.
func confirm(cmd *cobra.Command, prompt string) (bool, error) {
	if !isTerminal() {
		return false, &usageError{fmt.Errorf("re-run with --yes to upgrade without a prompt (stdin is not a terminal)")}
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N] ", prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false, nil
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return answer == "y" || answer == "yes", nil
}
