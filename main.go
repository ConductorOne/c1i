package main

import (
	_ "embed"
	"os"

	"github.com/ConductorOne/c1i/cmd"
)

// Embedded here because go:embed cannot reach the repo root from cmd.
//
//go:embed README.md
var readme string

//go:embed CHANGELOG.md
var changelog string

func main() {
	cmd.SetEmbeddedDocs(readme, changelog)
	os.Exit(cmd.Run())
}
