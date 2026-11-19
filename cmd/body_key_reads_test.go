package cmd

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// A guard on user JSON reads keys through bodyField and friends
// (body_fields.go), so a snake_case key cannot skip it. This walk fails on a
// multi-word key READ by literal or package const (m["fooBar"], m[keyConst],
// incl. comma-ok and type assertions), and on a multi-word `case` or `==`
// against a ranged map key, anywhere in cmd/ outside bodyKeyReadExemptions.
// Writes (m["k"] = v) are not reads. Not caught: a key in a local variable,
// alias or table (m[k]), a function-local const, a range value, EqualFold or a
// folded switch, or a literal passed to a helper. Inline JSON flags (api --body, functions invoke --input,
// --claims) are out of scope: api is the raw escape hatch, the others are
// free-form.

// bodyKeyReadExemption lists the keys a function may read exactly, and why
// the map is not user JSON.
type bodyKeyReadExemption struct {
	keys   []string
	reason string
}

// bodyKeyReadExemptions is keyed by "file.go:func" (a package-level var for
// a closure in its initializer, "Type.method" for a method). A new key in an
// exempt function fails like a new function does.
var bodyKeyReadExemptions = map[string]bodyKeyReadExemption{
	"deleted.go:deletedAtPath":                                          {[]string{"deletedAt"}, "a server response, which is always camelCase"},
	"api.go:extractListAndToken":                                        {[]string{"nextPageToken"}, "a server response, which is always camelCase"},
	"body_keys.go:refuseCLIOwnedBodyKeys":                               {[]string{"c1i_owned_body_fields"}, "a cobra flag annotation, not JSON"},
	"flags.go:flagGiven":                                                {[]string{"c1i_empty_allowed"}, "a cobra flag annotation, not JSON"},
	"flags.go:pageSizeFlag":                                             {[]string{"c1i_page_size_max"}, "a cobra flag annotation, not JSON"},
	"flags.go:protoDurationFlag":                                        {[]string{"c1i_proto_duration"}, "a cobra flag annotation, not JSON"},
	"flags.go:validateHeaderFlags":                                      {[]string{"c1i_header_text"}, "a cobra flag annotation, not JSON"},
	"root.go:validateBlankFlags":                                        {[]string{"c1i_blank_hint", "c1i_empty_allowed"}, "a cobra flag annotation, not JSON"},
	"apps_usage_controls.go:dropNullUsageControls":                      {[]string{"appUsageControls"}, "a server response, which is always camelCase"},
	"api.go:nextPageTokenFromMap":                                       {[]string{"nextPageToken"}, "a server response, which is always camelCase"},
	"attribute_mappings.go:attributeMappingOf":                          {[]string{"userAttributeMapping"}, "a server response, which is always camelCase"},
	"attribute_mappings.go:attributeMappingRow":                         {[]string{"attributeType", "evaluationErrorCount", "userAttributeMapping"}, "a server response, which is always camelCase"},
	"attribute_mappings_update.go:attributeMappingUpdateSpecFor":        {[]string{"attributeType", "displayName"}, "the mapping read from the server"},
	"attribute_mappings_update.go:buildAttributeMappingUpdate":          {[]string{"attributeType", "profileTypeIds"}, "a server response, which is always camelCase"},
	"auth_whoami.go:authWhoamiCmd":                                      {[]string{"userId"}, "the token introspection response"},
	"auth_whoami.go:summarize":                                          {[]string{"principleId", "userId"}, "the token introspection response"},
	"entitlements_automation.go:conditionRefs":                          {[]string{"entitlementRefs"}, "a map c1i builds from flags"},
	"entitlements_account_provisioning.go:entitlementProvisionerPolicy": {[]string{"provisionerPolicy"}, "a server response, which is always camelCase"},
	"entitlements_automation.go:entitlementsAutomationUpdateCmd":        {[]string{"displayName"}, "a map c1i builds from flags"},
	"entitlements_update.go:entitlementUpdateSpec":                      {[]string{"emergencyGrantPolicyId"}, "a map c1i builds from flags"},
	"findings.go:findingTransformationRuleRow":                          {[]string{"evaluationOrder"}, "a server response, which is always camelCase"},
	"grants_accounts.go:grantBindingRow":                                {[]string{"grantSources"}, "a server response, which is always camelCase"},
	"hooks_list.go:hookRow":                                             {[]string{"managedByGuardrails"}, "a server response, which is always camelCase"},
	"mcp_servers_test_connection.go:probeResultError":                   {[]string{"failureReason"}, "a server response, which is always camelCase"},
	"paginate.go:parseKeyed":                                            {[]string{"nextPageToken"}, "a server response, which is always camelCase"},
	"requests_create_grant.go:grantBaselinePreRead":                     {[]string{"identityUserId"}, "a map c1i builds from flags"},
	"requests_create_grant.go:requestsCreateGrantCmd":                   {[]string{"identityUserId"}, "a map c1i builds from flags"},
	"role_mining.go:roleMiningAnalysisRow":                              {[]string{"cohortSize", "suggestionsGenerated"}, "a server response, which is always camelCase"},
	"role_mining.go:roleMiningRunRow":                                   {[]string{"cohortsAnalyzed", "suggestionsGenerated", "totalUsers"}, "a server response, which is always camelCase"},
	"role_mining.go:roleMiningSuggestionRow":                            {[]string{"cohortSize", "usersWithAll"}, "a server response, which is always camelCase"},
	"roles.go:roleRow":                                                  {[]string{"systemApiOnly", "systemBuiltin"}, "a server response, which is always camelCase"},
	"service_principals_trusts.go:spTrustsCreateCmd":                    {[]string{"scopedRoleIds"}, "a map c1i builds from flags"},
	"service_principals_trusts.go:spTrustsTestTokenCmd":                 {[]string{"overallResult"}, "a server response, which is always camelCase"},
	"settings_ai_governance.go:lifecycleUpdate":                         {[]string{"defaultClientLifecycle"}, "a map c1i builds from flags"},
	"xaa_access_profiles.go:xaaAccessProfileRow":                        {[]string{"scopeCount"}, "a server response, which is always camelCase"},
	"xaa_resource_servers.go:xaaResourceServerRow":                      {[]string{"modifyClaimsHook"}, "a server response, which is always camelCase"},
}

// multiWordKey reports whether lit has a second spelling the server accepts.
func multiWordKey(lit string) bool { return foldMaskName(lit) != lit }

// literalKeyReads returns each multi-word literal-key read in path, keyed by
// "file.go:func".
func literalKeyReads(path string, consts map[string]string) (map[string][]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	file := filepath.Base(path)
	add := func(owner string, e ast.Expr) {
		var key string
		switch x := e.(type) {
		case *ast.BasicLit:
			k, err := strconv.Unquote(x.Value)
			if x.Kind != token.STRING || err != nil {
				return
			}
			key = k
		case *ast.Ident:
			k, ok := consts[x.Name]
			if !ok {
				return
			}
			key = k
		default:
			return
		}
		if !multiWordKey(key) {
			return
		}
		name := file + ":" + owner
		out[name] = append(out[name], key)
	}
	scan := func(owner string, root ast.Node) {
		writes := map[*ast.IndexExpr]bool{}
		rangeKeys := map[string]bool{}
		ast.Inspect(root, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				for _, l := range x.Lhs {
					if ix, ok := l.(*ast.IndexExpr); ok && x.Tok == token.ASSIGN {
						writes[ix] = true
					}
				}
			case *ast.RangeStmt:
				if id, ok := x.Key.(*ast.Ident); ok {
					rangeKeys[id.Name] = true
				}
			case *ast.IndexExpr:
				if !writes[x] {
					add(owner, x.Index)
				}
			case *ast.BinaryExpr:
				if x.Op != token.EQL && x.Op != token.NEQ {
					return true
				}
				if id, ok := x.X.(*ast.Ident); ok && rangeKeys[id.Name] {
					add(owner, x.Y)
				}
				if id, ok := x.Y.(*ast.Ident); ok && rangeKeys[id.Name] {
					add(owner, x.X)
				}
			case *ast.SwitchStmt:
				id, ok := x.Tag.(*ast.Ident)
				if !ok || !rangeKeys[id.Name] {
					return true
				}
				for _, s := range x.Body.List {
					for _, e := range s.(*ast.CaseClause).List {
						add(owner, e)
					}
				}
			}
			return true
		})
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) == 1 {
				t := d.Recv.List[0].Type
				if s, ok := t.(*ast.StarExpr); ok {
					t = s.X
				}
				if id, ok := t.(*ast.Ident); ok {
					name = id.Name + "." + name
				}
			}
			scan(name, d)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if vs, ok := spec.(*ast.ValueSpec); ok && len(vs.Names) > 0 {
					scan(vs.Names[0].Name, vs)
				}
			}
		}
	}
	return out, nil
}

// packageStringConsts returns the package-level string constants in files.
func packageStringConsts(files []string) (map[string]string, error) {
	out := map[string]string{}
	for _, path := range files {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return nil, err
		}
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, spec := range g.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if v, err := strconv.Unquote(lit.Value); err == nil {
							out[name.Name] = v
						}
					}
				}
			}
		}
	}
	return out, nil
}

func cmdSourceFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			out = append(out, f)
		}
	}
	return out
}

// unexemptedKeyReads returns each read the exemptions do not cover, and each
// exemption no read uses.
func unexemptedKeyReads(reads map[string][]string, exempt map[string]bodyKeyReadExemption) (bad, stale []string) {
	used := map[string]bool{}
	for owner, keys := range reads {
		allowed := map[string]bool{}
		for _, k := range exempt[owner].keys {
			allowed[k] = true
		}
		for _, k := range keys {
			if !allowed[k] {
				bad = append(bad, fmt.Sprintf("%s reads %q", owner, k))
			}
			used[owner+"\x00"+k] = true
		}
	}
	for owner, e := range exempt {
		for _, k := range e.keys {
			if !used[owner+"\x00"+k] {
				stale = append(stale, fmt.Sprintf("%s %q", owner, k))
			}
		}
	}
	sort.Strings(bad)
	sort.Strings(stale)
	return bad, stale
}

func TestBodyKeyReadsAreFolded(t *testing.T) {
	reads := map[string][]string{}
	files := cmdSourceFiles(t)
	consts, err := packageStringConsts(files)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		got, err := literalKeyReads(f, consts)
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range got {
			reads[k] = append(reads[k], v...)
		}
	}
	for owner, e := range bodyKeyReadExemptions {
		if strings.TrimSpace(e.reason) == "" {
			t.Errorf("exemption %s has no reason", owner)
		}
	}
	bad, stale := unexemptedKeyReads(reads, bodyKeyReadExemptions)
	for _, b := range bad {
		t.Errorf("%s: read user JSON with bodyField/bodyFieldAs (body_fields.go); if the map is not user JSON, add the key to bodyKeyReadExemptions with a reason", b)
	}
	for _, s := range stale {
		t.Errorf("bodyKeyReadExemptions lists %s, which no longer reads it; remove it", s)
	}
}

// TestLiteralKeyReadsSelfCheck pins what the walk flags: reads by literal or
// const, comma-ok reads, assertions, and switch cases and == over a ranged
// key; not writes. A stale
// exemption and a new key in an exempt function both fail.
func TestLiteralKeyReadsSelfCheck(t *testing.T) {
	src := `package cmd
func f(m map[string]any) {
	m["writeOnly"] = 1
	_ = m["readKey"]
	_, _ = m["commaOk"]
	_, _ = m["assert_key"].(string)
	_ = m["single"]
	_ = m[constKey]
	for k := range m {
		switch k {
		case "caseKey", "plain":
		}
		if k == "eqKey" || "revKey" != k || k == "flat" {
		}
	}
}
const constKey = "const_key"
var v = func(m map[string]any) any { return m["inVar"] }
`
	path := filepath.Join(t.TempDir(), "x.go")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	consts, err := packageStringConsts([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	reads, err := literalKeyReads(path, consts)
	if err != nil {
		t.Fatal(err)
	}
	for _, keys := range reads {
		sort.Strings(keys)
	}
	got := fmt.Sprint(reads)
	want := `map[x.go:f:[assert_key caseKey commaOk const_key eqKey readKey revKey] x.go:v:[inVar]]`
	if got != want {
		t.Errorf("reads = %s, want %s", got, want)
	}
	bad, stale := unexemptedKeyReads(reads, map[string]bodyKeyReadExemption{
		"x.go:f": {keys: []string{"assert_key", "caseKey", "commaOk", "const_key", "eqKey", "revKey", "gone"}, reason: "r"},
	})
	if fmt.Sprint(bad) != `[x.go:f reads "readKey" x.go:v reads "inVar"]` || fmt.Sprint(stale) != `[x.go:f "gone"]` {
		t.Errorf("bad = %v, stale = %v", bad, stale)
	}
}
