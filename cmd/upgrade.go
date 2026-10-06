package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/selfupdate"
	"github.com/ConductorOne/c1i/internal/transport"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/mod/module"
)

var upgradeChannels = map[string]bool{"stable": true, "latest": true, "preview": true}

var upgradeCmd = &cobra.Command{
	Use:     "upgrade",
	Aliases: []string{"update"},
	Short:   "Upgrade c1i to the latest release from the C1.ai distribution center",
	Long: `Check for and install a newer c1i release.

Release channels come from the C1.ai distribution center (dist.conductorone.com):
"stable" by default, or "latest" and "preview" via --channel. Before replacing
anything, upgrade verifies the release manifest's Sigstore signature (signed by
the release workflow run for that c1i tag) and the download's SHA-256 from that
manifest.

Only a standalone binary is replaced in place. For a Homebrew, "go install", or
container-image install, upgrade prints that method's upgrade command instead
and exits 0. --check prints a JSON report and changes nothing; --dry-run
verifies the release and prints what it would replace, installing nothing.

  c1i upgrade                       # upgrade to the latest stable release (asks first)
  c1i upgrade --check               # report whether a newer release is available
  c1i upgrade --channel latest -y   # take the newest release without prompting`,
	RunE: func(cmd *cobra.Command, args []string) error {
		channel, _ := cmd.Flags().GetString("channel")
		if !upgradeChannels[channel] {
			return &usageError{fmt.Errorf("unknown --channel %q: expected stable, latest, or preview", channel)}
		}
		checkOnly, _ := cmd.Flags().GetBool("check")
		assumeYes, _ := cmd.Flags().GetBool("yes")
		out := cmd.OutOrStdout()

		dist := newUpgradeClient()
		idx, err := dist.Index(cmd.Context())
		if err != nil {
			return distError(err, "reading release channels")
		}
		target := idx.Channels[channel]
		if target == "" {
			return &upstreamError{fmt.Errorf("the distribution center lists no %q channel", channel)}
		}
		// index.json is unsigned, so this yank check is best-effort (see README).
		if e, ok := idx.Semvers[target]; ok && e.Yanked {
			return &upstreamError{fmt.Errorf("the %q channel points at %q, which has been yanked; try again later", channel, target)}
		}

		current := Version
		cmp := 0
		if isReleaseVersion(current) {
			var ok bool
			if cmp, ok = selfupdate.CompareVersions(current, target); !ok {
				return &upstreamError{fmt.Errorf("cannot compare current version %q with %q", current, target)}
			}
		}
		if checkOnly {
			return writeUpgradeReport(cmd, current, target, channel, isReleaseVersion(current) && cmp < 0)
		}
		switch {
		case !isReleaseVersion(current):
			_, _ = fmt.Fprintf(out, "c1i is a development build (version %q); `c1i upgrade` works on released binaries.\n", current)
			_, _ = fmt.Fprintf(out, "The current %s release is %q.\n", channel, target)
			return nil
		case cmp == 0:
			_, _ = fmt.Fprintf(out, "c1i %s is current on the %s channel.\n", current, channel)
			return nil
		case cmp > 0:
			_, _ = fmt.Fprintf(out, "c1i %s is newer than the %s channel (%q); nothing to do.\n", current, channel, target)
			if latest := idx.Channels["latest"]; channel == "stable" && newerRelease(latest, current) && !idx.Semvers[latest].Yanked {
				_, _ = fmt.Fprintln(out, "(Pass --channel latest to track the newest release.)")
			}
			return nil
		}

		execPath, err := upgradeExecutable()
		if err != nil {
			return fmt.Errorf("locating the running binary: %w", err)
		}
		if method, hint := selfupdate.Detect(execPath, runtime.GOOS); method != selfupdate.Standalone {
			_, _ = fmt.Fprintf(out, "Not upgrading in place: %s\n", hint)
			return nil
		}
		installDir := filepath.Dir(execPath)
		if err := selfupdate.CheckWritable(installDir); err != nil {
			return fmt.Errorf("cannot write to the install directory %s: %w", installDir, err)
		}

		entry, ok := idx.Semvers[target]
		if !ok || entry.Manifest == "" {
			return &upstreamError{fmt.Errorf("no manifest listed for %q", target)}
		}
		manifest, manifestBytes, err := dist.ManifestRaw(cmd.Context(), entry.Manifest)
		if err != nil {
			return distError(err, "reading the %q manifest", target)
		}
		if cmp, ok := selfupdate.CompareVersions(manifest.Semver, target); !ok || cmp != 0 {
			return &upstreamError{fmt.Errorf("manifest for %q reports version %q; refusing the mismatch", target, manifest.Semver)}
		}
		if entry.Signature == "" || entry.Certificate == "" || manifest.SignatureBundleHref == "" {
			return &upstreamError{fmt.Errorf("release %q carries incomplete signature material", target)}
		}
		sig, err := dist.GetBytes(cmd.Context(), entry.Signature)
		if err != nil {
			return distError(err, "fetching the %q manifest signature", target)
		}
		cert, err := dist.GetBytes(cmd.Context(), entry.Certificate)
		if err != nil {
			return distError(err, "fetching the %q manifest certificate", target)
		}
		rekorBundle, err := dist.GetBytes(cmd.Context(), manifest.SignatureBundleHref)
		if err != nil {
			return distError(err, "fetching the %q manifest Rekor bundle", target)
		}
		if err := dist.VerifyManifest(cmd.Context(), manifestBytes, sig, cert, rekorBundle, manifest.Semver); err != nil {
			return distError(err, "verifying the %q release", target)
		}

		asset, ok := manifest.Assets[selfupdate.PlatformKey()]
		if !ok {
			return &upstreamError{fmt.Errorf("%s has no build for %s", target, selfupdate.PlatformKey())}
		}
		if dryRunActive() {
			_, _ = fmt.Fprintf(out, "[dry-run] manifest signature verified; would download %s\n", asset.Href)
			_, _ = fmt.Fprintf(out, "[dry-run] would verify sha256 %s and replace %s\n", asset.SHA256, execPath)
			return nil
		}

		unlock, err := selfupdate.LockExecutable(execPath)
		if err != nil {
			return err
		}
		defer unlock()
		// Re-read under the lock: a concurrent upgrade may have replaced the binary.
		current, err = installedVersion(execPath)
		if err != nil {
			return fmt.Errorf("reading the installed c1i version: %w", err)
		}
		if cmp, ok := selfupdate.CompareVersions(current, target); !ok {
			return &upstreamError{fmt.Errorf("cannot compare installed version %q with %s", current, target)}
		} else if cmp >= 0 {
			_, _ = fmt.Fprintf(out, "Installed c1i is %s; nothing to do.\n", current)
			return nil
		}
		_, _ = fmt.Fprintf(out, "A newer %s release is available: %s -> %s.\n", channel, current, target)

		if !assumeYes {
			ok, err := confirm(cmd, fmt.Sprintf("Upgrade c1i %s -> %s, replacing %s?", current, target, execPath))
			if err != nil {
				return err
			}
			if !ok {
				_, _ = fmt.Fprintln(out, "Upgrade cancelled.")
				return nil
			}
		}

		_, _ = fmt.Fprintf(out, "Downloading %s...\n", asset.Filename)
		bin, err := dist.FetchBinary(cmd.Context(), asset)
		if err != nil {
			return distError(err, "downloading %s", target)
		}
		if err := selfupdate.ReplaceExecutable(execPath, bin); err != nil {
			return fmt.Errorf("installing into %s: %w", installDir, err)
		}
		_, _ = fmt.Fprintf(out, "Upgraded c1i %s -> %s.\n", current, target)
		return nil
	},
}

// Test seams.
var (
	newUpgradeClient = func() *selfupdate.Client {
		doer := func(limit int64) selfupdate.Doer {
			return transport.New(nil,
				transport.WithMaxRetries(viper.GetInt("max_retries")),
				transport.WithDebug(viper.GetBool("debug")),
				transport.WithMaxResponseBytes(limit),
			)
		}
		return &selfupdate.Client{HTTP: doer(selfupdate.MaxMetadataBytes), Download: doer(selfupdate.MaxArtifactBytes)}
	}
	upgradeExecutable = selfupdate.ExecutablePath
	installedVersion  = selfupdate.InstalledVersion
)

func init() {
	upgradeCmd.Flags().Bool("check", false, "Print a JSON report of whether a newer release is available; change nothing")
	upgradeCmd.Flags().String("channel", "stable", "Release channel: stable, latest, or preview")
	_ = upgradeCmd.RegisterFlagCompletionFunc("channel", cobra.FixedCompletions([]string{"stable", "latest", "preview"}, cobra.ShellCompDirectiveNoFileComp))
	upgradeCmd.Flags().BoolP("yes", "y", false, "Skip the confirmation prompt")
	rootCmd.AddCommand(upgradeCmd)
}

func writeUpgradeReport(cmd *cobra.Command, current, target, channel string, available bool) error {
	execPath, err := upgradeExecutable()
	if err != nil {
		return fmt.Errorf("locating the running binary: %w", err)
	}
	method, _ := selfupdate.Detect(execPath, runtime.GOOS)
	report := map[string]any{
		"current":          current,
		"latest":           target,
		"channel":          channel,
		"update_available": available,
		"install_method":   method.String(),
	}
	if c := method.Command(); c != "" {
		report["upgrade_command"] = c
	}
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	return writeObject(cmd, data)
}

// distError classifies a failure fetching or verifying a release as upstream
// (8), except that a dist 404, 429 or 5xx keeps its own exit code. dist needs
// no auth and takes no input from the caller, so for a refused redirect, a bad
// path, or any other 4xx the status is dropped and the exit code is 8.
func distError(err error, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		if s := apiErr.StatusCode; s == 404 || s == 429 || s >= 500 {
			return &upstreamError{fmt.Errorf("%s: %w", msg, err)}
		}
		return &upstreamError{fmt.Errorf("%s: %v", msg, err)}
	}
	var redirErr *client.RedirectError
	var pathErr *client.PathError
	if errors.As(err, &redirErr) || errors.As(err, &pathErr) {
		return &upstreamError{fmt.Errorf("%s: %v", msg, err)}
	}
	return &upstreamError{fmt.Errorf("%s: %w", msg, err)}
}

// newerRelease reports whether candidate is a version newer than current.
func newerRelease(candidate, current string) bool {
	cmp, ok := selfupdate.CompareVersions(candidate, current)
	return ok && cmp > 0
}

// isReleaseVersion reports whether v is a release tag. A source build reports
// "dev" or, since Go 1.24, a pseudo-version such as
// v0.8.1-0.20261005220836-bb1be01ebd69; neither is a release to upgrade from.
func isReleaseVersion(v string) bool {
	if module.IsPseudoVersion(v) {
		return false
	}
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
	scanner := bufio.NewScanner(cmd.InOrStdin())
	if !scanner.Scan() {
		return false, nil
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return answer == "y" || answer == "yes", nil
}
