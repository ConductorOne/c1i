package cmd

import (
	"errors"
	"fmt"
	"os"
	"regexp"

	"github.com/spf13/cobra"
)

// README.md and CHANGELOG.md sit at the repo root, which go:embed in this
// package cannot reach; main.go embeds them and hands them over here.
var embeddedReadme, embeddedChangelog string

// SetEmbeddedDocs registers the root-level docs for `docs readme` and
// `docs changelog`. Call it before Run.
func SetEmbeddedDocs(readme, changelog string) {
	embeddedReadme, embeddedChangelog = readme, changelog
}

// docOutputFile is the file --output names; "" for stdout, which "-" also means.
func docOutputFile(cmd *cobra.Command) string {
	output, _ := cmd.Flags().GetString("output")
	if output == "-" {
		return ""
	}
	return output
}

// emitDoc writes content to --output or stdout. label names the doc in the
// "Wrote ..." notice and errors; shared by every embedded-doc command.
func emitDoc(cmd *cobra.Command, label, content string) error {
	if output := docOutputFile(cmd); output != "" {
		if err := os.WriteFile(output, []byte(content), 0o644); err != nil { // #nosec G306 -- content is a static, public doc, not sensitive
			return fmt.Errorf("writing %s: %w", label, err)
		}
		notice(cmd, noticeInfo, fmt.Sprintf("Wrote %s to %s", label, output), nil)
		return nil
	}
	_, err := fmt.Fprint(cmd.OutOrStdout(), content)
	return wrapOutputErr(err)
}

// releaseTagRE matches a published tag. A source build's pseudo-version
// ("v0.9.3-0.2026...-abc123", "+dirty") is not a tag, so its link would 404.
var releaseTagRE = regexp.MustCompile(`^v\d+\.\d+\.\d+(-rc\.?\d+)?$`)

// docsBlobURL is the version-pinned GitHub URL for a repo-root file, or the
// main branch when this build isn't a release.
func docsBlobURL(file string) string {
	ref := "main"
	if releaseTagRE.MatchString(Version) {
		ref = Version
	}
	return "https://github.com/ConductorOne/c1i/blob/" + ref + "/" + file
}

func newEmbeddedDocCmd(use, file string, content *string, long string) *cobra.Command {
	c := &cobra.Command{
		Use:   use,
		Short: "Print the embedded " + file,
		Long:  long,
		RunE: func(cmd *cobra.Command, args []string) error {
			if *content == "" {
				return errors.New("no embedded " + file + " in this build")
			}
			if err := emitDoc(cmd, file, *content); err != nil {
				return err
			}
			if docOutputFile(cmd) == "" {
				notice(cmd, noticeInfo, "Rendered markdown with working links: "+docsBlobURL(file), map[string]any{"url": docsBlobURL(file)})
			}
			return nil
		},
	}
	c.Flags().StringP("output", "o", "", "write to file instead of stdout (\"-\" is stdout)")
	allowEmpty(c, emptyIsOmitted, "output")
	return c
}

var docsReadmeCmd = newEmbeddedDocCmd("readme", "README.md", &embeddedReadme,
	`Print the README embedded in this binary, as-is, with no network call or
credentials. Release archives ship only the binary, so this is how an
installed c1i reads its own full reference.

The README is the full reference. "docs agents" is the concise guide for AI
agents; start there, and come here for detail.

The text is printed unmodified, so its relative links do not resolve. A
footer on stderr gives the version-pinned GitHub URL that renders them.`)

var docsChangelogCmd = newEmbeddedDocCmd("changelog", "CHANGELOG.md", &embeddedChangelog,
	`Print the CHANGELOG embedded in this binary, as-is, with no network call or
credentials. Breaking changes and migration notes are under "Upgrading from
..." headings.

A footer on stderr gives the version-pinned GitHub URL that renders the
markdown.`)

func init() {
	docsCmd.AddCommand(docsReadmeCmd, docsChangelogCmd)
}
