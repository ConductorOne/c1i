package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// optionalReadCallers pins every caller of the reads that skip resourceAt's
// missing-object check, keyed "file:declaration". Adding one is a deliberate
// edit here: a get addressed by its id must use writeResource/getResource.
var optionalReadCallers = map[string]string{
	"unwrap.go:getSingleton":                           "the singleton helper itself",
	"settings.go:newSettingsShowCmd":                   "tenant settings are a singleton; an unset message may come back empty",
	"mcp_classifiers.go:mcpClassifierPolicyShowCmd":    "the agent classifier policy is a tenant singleton",
	"role_mining.go:roleMiningConfigShowCmd":           "role mining config is a tenant singleton",
	"role_mining.go:roleMiningRunsLatestCmd":           "a latest lookup, not addressed by id; a tenant may have no run",
	"role_mining.go:roleMiningCustomAnalysisLatestCmd": "a latest lookup, not addressed by id; a user may have no analysis",
	"connectors_get.go:connectorsSyncStatusCmd":        "sync progress, not the connector; may be unset before a first sync",
	"apps_usage_controls.go:appsUsageControlsGetCmd":   "unset controls answer null and print only hasUsageData (TestUsageControlsGetShape); requireLiveApp guards a missing app",
}

var optionalReadFuncs = map[string]bool{"writeOptionalResource": true, "writeOptionalNamedResource": true, "getSingleton": true}

func TestOptionalReadCallersArePinned(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path) // #nosec G304 -- a source file of this package, globbed above
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			name := declName(decl)
			ast.Inspect(decl, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					if id, ok := c.Fun.(*ast.Ident); ok && optionalReadFuncs[id.Name] {
						got[path+":"+name] = true
					}
				}
				return true
			})
		}
	}
	var extra, stale []string
	for k := range got {
		if _, ok := optionalReadCallers[k]; !ok {
			extra = append(extra, k)
		}
	}
	for k, reason := range optionalReadCallers {
		if !got[k] {
			stale = append(stale, k)
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%s: pinned without a reason", k)
		}
	}
	sort.Strings(extra)
	sort.Strings(stale)
	if len(extra) > 0 {
		t.Errorf("unpinned callers of a read that skips the missing-object check: %v\n"+
			"a get must use writeResource/getResource; pin a singleton or latest read in optionalReadCallers with its reason", extra)
	}
	if len(stale) > 0 {
		t.Errorf("optionalReadCallers entries with no such call: %v", stale)
	}
}

func declName(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return d.Name.Name
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			if vs, ok := spec.(*ast.ValueSpec); ok && len(vs.Names) > 0 {
				return vs.Names[0].Name
			}
		}
	}
	return "?"
}
