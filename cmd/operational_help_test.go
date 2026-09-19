package cmd

import (
	"strings"
	"testing"
)

// TestOperationalHelpStatesDecisionBoundaries protects the short help that an
// agent commonly reads before it loads docs agents. These statements prevent
// plausible but unsafe assumptions: silently falling back to another tenant,
// treating a gateway tool call as dry-runnable, accepting a single API page as
// complete, or treating an OpenAPI-spec miss as proof that C1 has no operation.
func TestOperationalHelpStatesDecisionBoundaries(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "tenant URL precedence",
			text: rootCmd.PersistentFlags().Lookup("url").Usage,
			want: []string{"C1 tenant URL", "Precedence: --url, C1I_URL, then ~/.c1i.yaml"},
		},
		{
			name: "embedded agent guide tenant precedence",
			text: agentsTemplate,
			want: []string{"`--url`, `C1I_URL`, then `url:`", "`~/.c1i.yaml`"},
		},
		{
			name: "dry-run REST scope",
			text: rootCmd.PersistentFlags().Lookup("dry-run").Usage,
			want: []string{"C1 REST mutations", "mcp gateway call rejects it"},
		},
		{
			name: "raw API escape hatch and pagination",
			text: apiCmd.Long,
			want: []string{"no first-class command", "unless --paginate is set", "partial-result warning"},
		},
		{
			name: "public OpenAPI endpoint discovery",
			text: docsEndpointsCmd.Long,
			want: []string{"public C1 OpenAPI spec", "real no-match", "does not prove no C1 operation exists"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text := strings.Join(strings.Fields(tc.text), " ")
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("help missing %q:\n%s", want, tc.text)
				}
			}
		})
	}
}
