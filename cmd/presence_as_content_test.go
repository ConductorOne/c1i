package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// presenceViolation is one use of presence (Changed) or a TrimSpace-emptiness
// check, which a content decision must not rely on.
type presenceViolation struct {
	key  string // file:top-level decl
	line int
	rule string // "trimspace" or "changed"
	flag string // literal flag name of a Changed call, "" if computed
}

// changedFlagAllowlist exempts a flag by name wherever it is read.
var changedFlagAllowlist = map[string]string{
	"page-token": "presence selects manual paging; a blank value is refused centrally (validateBlankFlags), so presence equals a non-blank token",
}

// changedAllowlist maps "file:decl" to why presence, not content, is the signal.
var changedAllowlist = map[string]string{
	"apps_create.go:buildAppCreateBody":                                "sends identityMatching only when passed; a blank value is refused centrally and the enum parser rejects the rest",
	"apps_update.go:appUpdateSpec":                                     "update-field hook: presence of --app-user-mapper-file is mask membership",
	"auth_login_roles.go:loginScopeFromFlags":                          "a bool: --choose-roles=false must differ from absent; a bool has no blank value",
	"docs_openapi.go:openAPIRetries":                                   "who set the retry count (flag vs default) decides precedence; the count's value is not inspected",
	"entitlements_update.go:entitlementUpdateSpec":                     "update-field hooks: a bool and a set/unset flag pair, where passing the flag is the signal",
	"flags.go:flagGiven":                                               "the one presence helper: Changed, minus an exact \"\" on an emptyIsOmitted flag",
	"flags.go:validateHeaderFlags":                                     "walks flags the user set to validate each value's content; Changed only skips unset ones",
	"flags.go:validatePathFlags":                                       "walks flags the user set to validate each value's content; Changed only skips unset ones",
	"flags.go:validateCountFlags":                                      "range-checks a count only if passed; the default is valid by construction",
	"flags.go:repeatableStringFlag":                                    "an unset repeatable flag is nil, not [\"\"]; each passed occurrence is content-checked",
	"flags.go:protoDurationFlag":                                       "an unset duration is omitted; a passed one is parsed and range-checked",
	"root.go:validateFlagsUTF8":                                        "validates the content of every flag the user set; Changed only skips unset ones",
	"root.go:validateBlankFlags":                                       "the central blank guard: walks set flags and judges their content itself",
	"root.go:validateGlobalSettings":                                   "which source (flag vs env/config) supplied a global setting, for the error wording",
	"root.go:validateDotSegmentIDFlags":                                "the central id guard: walks set flags and judges their content itself",
	"root.go:GetBaseURLWithSource":                                     "--url precedence: a passed flag outranks env/config; the value is validated elsewhere",
	"mutation.go:refuseMutationFields":                                 "--fields is refused on a mutation if passed at all, whatever its value",
	"access_profiles_entitlements.go:buildRequestableEntitlementsBody": "a bool: --create-requests=false must differ from absent",
	"flags.go:requireNonEmpty":                                         "content is judged by hasVisibleRune first; Changed only picks the unset vs blank message",
	"flags.go:bodyFileMask":                                            "was --body-file/--update-mask passed (even as an empty path); the mask content is checked by normalizeUpdateMask",
	"policies_update.go:readPolicyUpdateInputs":                        "was a --*-file passed (an empty path is refused centrally); presence picks the input source",
	"policies_update.go:policyUpdateSpecFor":                           "update-field hooks: presence of --steps-file/--rules-file is mask membership",
	"policy_binding.go:policyIDFlag":                                   "an unset flag means leave the binding alone; a blank id is refused centrally before this runs",
	"policy_binding.go:withPolicyBindingsCheck":                        "only validate bindings when a policy-id flag was passed; blank ids are refused centrally",
	"update_fields.go:strUpdate":                                       "the update-field builder: presence of a flag is mask membership",
	"update_fields.go:boolUpdate":                                      "the update-field builder: presence of a flag is mask membership",
	"update_fields.go:optionalBoolUpdate":                              "the update-field builder: presence of a flag is mask membership",
	"update_fields.go:enumUpdate":                                      "the update-field builder: presence of a flag is mask membership",
	"wait.go:waitFlagValues":                                           "--wait is a bool: passed (even =false) vs defaulted decides whether --wait-* companions are stray",
}

// presenceScan reports every violation in file; path is the base file name.
func presenceScan(fset *token.FileSet, file *ast.File, path string) []presenceViolation {
	var out []presenceViolation
	for _, decl := range file.Decls {
		name := "?"
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name = d.Name.Name
		case *ast.GenDecl:
			if len(d.Specs) > 0 {
				if vs, ok := d.Specs[0].(*ast.ValueSpec); ok && len(vs.Names) > 0 {
					name = vs.Names[0].Name
				}
			}
		}
		key := path + ":" + name
		calls := map[*ast.SelectorExpr]bool{}
		ast.Inspect(decl, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				// The field form (Lookup(x).Changed) reads presence too.
				if n.Sel.Name == "Changed" && !calls[n] {
					out = append(out, presenceViolation{key: key, line: fset.Position(n.Pos()).Line, rule: "changed"})
				}
			case *ast.CallExpr:
				if sel, ok := n.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Changed" {
					calls[sel] = true
					v := presenceViolation{key: key, line: fset.Position(n.Pos()).Line, rule: "changed"}
					if len(n.Args) == 1 {
						if lit, ok := n.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							v.flag = strings.Trim(lit.Value, `"`)
						}
					}
					out = append(out, v)
				}
			case *ast.BinaryExpr:
				if isTrimSpaceEmptiness(n) {
					out = append(out, presenceViolation{key: key, line: fset.Position(n.Pos()).Line, rule: "trimspace"})
				}
			}
			return true
		})
	}
	return out
}

func isStringsTrimSpace(e ast.Expr) bool {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "TrimSpace" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "strings"
}

// isTrimSpaceEmptiness matches strings.TrimSpace(x) compared to "" and
// len(strings.TrimSpace(x)) compared to an integer, in either operand order.
func isTrimSpaceEmptiness(b *ast.BinaryExpr) bool {
	switch b.Op {
	case token.EQL, token.NEQ, token.GTR, token.LSS, token.GEQ, token.LEQ:
	default:
		return false
	}
	for _, pair := range [][2]ast.Expr{{b.X, b.Y}, {b.Y, b.X}} {
		if isStringsTrimSpace(pair[0]) {
			if lit, ok := pair[1].(*ast.BasicLit); ok && lit.Kind == token.STRING && (lit.Value == `""` || lit.Value == "``") {
				return true
			}
		}
		if c, ok := pair[0].(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "len" && len(c.Args) == 1 && isStringsTrimSpace(c.Args[0]) {
				if lit, ok := pair[1].(*ast.BasicLit); ok && lit.Kind == token.INT {
					return true
				}
			}
		}
	}
	return false
}

// TestNoPresenceAsContent bans the signal behind the `--page-token ""` bug:
// deciding on a value's content by whether a flag was passed (Changed) or by
// strings.TrimSpace emptiness. Content goes through hasVisibleRune or the
// central blank guards; a legitimate presence check needs a reasoned entry.
func TestNoPresenceAsContent(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	changedSeen := 0
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		for _, v := range presenceScan(fset, file, path) {
			switch v.rule {
			case "trimspace":
				t.Errorf("%s:%d: strings.TrimSpace emptiness check; use hasVisibleRune or the central blank guards", v.key, v.line)
			case "changed":
				changedSeen++
				if _, ok := changedFlagAllowlist[v.flag]; ok && v.flag != "" {
					seen["flag "+v.flag] = true
					continue
				}
				if _, ok := changedAllowlist[v.key]; ok {
					seen[v.key] = true
					continue
				}
				t.Errorf("%s:%d: .Changed(%q) outside the allowlists; if content matters use hasVisibleRune or a blank guard, else add a reasoned entry saying why presence is the signal", v.key, v.line, v.flag)
			}
		}
	}
	if changedSeen < 30 {
		t.Fatalf("found only %d Changed calls; this scan is not looking at what it thinks it is", changedSeen)
	}
	var stale []string
	for k, reason := range changedAllowlist {
		if reason == "" {
			t.Errorf("changedAllowlist[%q] has no reason", k)
		}
		if !seen[k] {
			stale = append(stale, k)
		}
	}
	for k, reason := range changedFlagAllowlist {
		if reason == "" {
			t.Errorf("changedFlagAllowlist[%q] has no reason", k)
		}
		if !seen["flag "+k] {
			stale = append(stale, "flag "+k)
		}
	}
	sort.Strings(stale)
	for _, k := range stale {
		t.Errorf("stale allowlist entry %s: no matching call remains; remove it", k)
	}
}

func scanSnippet(t *testing.T, src string) []presenceViolation {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", "package cmd\n"+src, 0)
	if err != nil {
		t.Fatal(err)
	}
	return presenceScan(fset, file, "x.go")
}

func TestPresenceScanCatchesPlantedViolations(t *testing.T) {
	cases := []struct {
		name, src, rule string
		want            int
	}{
		{"trimspace eq", `func f(s string) bool { return strings.TrimSpace(s) == "" }`, "trimspace", 1},
		{"trimspace neq", `func f(s string) bool { return strings.TrimSpace(s) != "" }`, "trimspace", 1},
		{"trimspace reversed", `func f(s string) bool { return "" == strings.TrimSpace(s) }`, "trimspace", 1},
		{"len eq zero", `func f(s string) bool { return len(strings.TrimSpace(s)) == 0 }`, "trimspace", 1},
		{"len gt zero", `func f(s string) bool { return len(strings.TrimSpace(s)) > 0 }`, "trimspace", 1},
		{"changed in closure", `var c = func(cmd *cobra.Command) bool { return cmd.Flags().Changed("x") }`, "changed", 1},
		{"changed computed", `func f(f *pflag.FlagSet, n string) bool { return f.Changed(n) }`, "changed", 1},
		{"trimspace raw string", "func f(s string) bool { return strings.TrimSpace(s) == `` }", "trimspace", 1},
		{"changed field", `func f(fl *pflag.FlagSet) bool { return fl.Lookup("x").Changed }`, "changed", 1},
		{"clean", `func f(s string) bool { return hasVisibleRune(s) && strings.TrimSpace(s) == s }`, "", 0},
	}
	for _, c := range cases {
		got := 0
		for _, v := range scanSnippet(t, c.src) {
			if v.rule == c.rule {
				got++
			}
		}
		if got != c.want {
			t.Errorf("%s: %d %q violations, want %d", c.name, got, c.rule, c.want)
		}
	}
}
