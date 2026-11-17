package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// commandMapNode is one command or command group in the live Cobra tree.
type commandMapNode struct {
	Name     string           `json:"name"`
	Path     string           `json:"path"`
	Short    string           `json:"short"`
	Aliases  []string         `json:"aliases,omitempty"`
	Children []commandMapNode `json:"children,omitempty"`
}

type commandMapCounts struct {
	Families int `json:"families"`
	Commands int `json:"commands"`
	Leaves   int `json:"leaves"`
}

type commandMapDoc struct {
	Version  string           `json:"version"`
	Counts   commandMapCounts `json:"counts"`
	Families []commandMapNode `json:"families"`
}

// buildCommandMap walks the same command tree Cobra dispatches, so the map
// cannot drift from the commands this binary actually accepts.
func buildCommandMap() commandMapDoc {
	families := commandMapChildren(rootCmd, "c1i")
	commands, leaves := countCommandMap(families)
	return commandMapDoc{
		Version: Version,
		Counts: commandMapCounts{
			Families: len(families),
			Commands: commands,
			Leaves:   leaves,
		},
		Families: families,
	}
}

func commandMapChildren(parent *cobra.Command, prefix string) []commandMapNode {
	cmds := parent.Commands()
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name() < cmds[j].Name() })
	out := make([]commandMapNode, 0, len(cmds))
	for _, c := range cmds {
		if c.Name() == "help" || !c.IsAvailableCommand() {
			continue
		}
		path := prefix + " " + c.Name()
		aliases := append([]string(nil), c.Aliases...)
		sort.Strings(aliases)
		out = append(out, commandMapNode{
			Name:     c.Name(),
			Path:     path,
			Short:    c.Short,
			Aliases:  aliases,
			Children: commandMapChildren(c, path),
		})
	}
	return out
}

func countCommandMap(nodes []commandMapNode) (commands, leaves int) {
	for _, n := range nodes {
		commands++
		if len(n.Children) == 0 {
			leaves++
			continue
		}
		c, l := countCommandMap(n.Children)
		commands += c
		leaves += l
	}
	return commands, leaves
}

func renderCommandMapText(doc commandMapDoc) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "c1i %s command map\n", doc.Version)
	fmt.Fprintf(&b, "%d families, %d commands, %d executable leaves\n\n",
		doc.Counts.Families, doc.Counts.Commands, doc.Counts.Leaves)
	for _, family := range doc.Families {
		writeCommandMapText(&b, family, 0)
	}
	return b.String()
}

func writeCommandMapText(b *bytes.Buffer, node commandMapNode, depth int) {
	label := node.Name
	if depth == 0 || len(node.Children) == 0 {
		label = node.Path
	}
	if len(node.Aliases) > 0 {
		label += " (alias: " + strings.Join(node.Aliases, ", ") + ")"
	}
	fmt.Fprintf(b, "%s%s — %s\n", strings.Repeat("  ", depth), label, node.Short)
	for _, child := range node.Children {
		writeCommandMapText(b, child, depth+1)
	}
}

var docsMapCmd = &cobra.Command{
	Use:   "map",
	Short: "Print the c1i command tree (no auth required)",
	Long: `Print every command this binary accepts, generated from the live Cobra tree.

The default output is an indented outline of command families and commands,
with each command's one-line description. --json adds the build version, counts,
full command paths, aliases, and the nested tree for scripts or documentation
checks. Use -o to write the map to a file.

No credentials, network, or C1 tenant are needed.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		doc := buildCommandMap()
		asJSON, _ := cmd.Flags().GetBool("json")
		var content string
		if asJSON {
			data, err := json.MarshalIndent(doc, "", "  ")
			if err != nil {
				return fmt.Errorf("encoding command map: %w", err)
			}
			content = string(data) + "\n"
		} else {
			content = renderCommandMapText(doc)
		}
		return emitDoc(cmd, "command map", content)
	},
}

func init() {
	docsMapCmd.Flags().Bool("json", false, "Emit the tree and counts as JSON")
	docsMapCmd.Flags().StringP("output", "o", "", "write to file instead of stdout (\"-\" is stdout)")
	allowEmpty(docsMapCmd, emptyIsOmitted, "output")
	docsCmd.AddCommand(docsMapCmd)
}
