package cmd

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

const usersListReply = `{"list":[{"user":{"id":"1","email":"a@b.c","displayName":"A"}}],"nextPageToken":""}`

// A --fields spec that is set but names no usable path used to switch
// projection off: full rows at exit 0, and the matched-nothing check never
// ran. Each channel (flag, C1I_FIELDS, ~/.c1i.yaml) must refuse it the same way.
func TestFieldsSpecWithNoUsablePathRefusedOnEveryChannel(t *testing.T) {
	bad := []string{",", ".", " , ", "id.", ".id", "a..b", "id,email.", "\u200b,"}
	type channel struct {
		name    string
		wantErr string
		apply   func(t *testing.T, spec string) []string
	}
	channels := []channel{
		{"flag", `invalid --fields`, func(t *testing.T, spec string) []string {
			useConfigFile(t, "")
			return []string{"users", "list", "--fields", spec}
		}},
		{"env", `invalid C1I_FIELDS`, func(t *testing.T, spec string) []string {
			useConfigFile(t, "")
			t.Setenv("C1I_FIELDS", spec)
			return []string{"users", "list"}
		}},
		{"config file", `in ~/.c1i.yaml`, func(t *testing.T, spec string) []string {
			useConfigFile(t, "fields: \""+spec+"\"\n")
			return []string{"users", "list"}
		}},
		// A YAML list or map reads as "" to viper, which meant no projection.
		{"config file list", `want a single value`, func(t *testing.T, spec string) []string {
			useConfigFile(t, "fields: [\""+spec+"\", id]\n")
			return []string{"users", "list"}
		}},
	}
	for _, ch := range channels {
		specs := bad
		if ch.name == "config file list" {
			specs = []string{"id"}
		}
		if ch.name == "env" || ch.name == "config file" {
			// A blank flag is the blank rule's job; env and config have no other check.
			specs = append(append([]string{}, bad...), " ")
		}
		for _, spec := range specs {
			t.Run(ch.name+" "+spec, func(t *testing.T) {
				clearEnvFlags(t)
				t.Setenv("C1I_FIELDS", "")
				args := ch.apply(t, spec)
				srv := newMutServer(t, fixedReply(200, usersListReply))
				res := runListCLI(t, srv.srv, args...)
				if res.exit != exitUsage || res.err == nil || !strings.Contains(res.err.Error(), ch.wantErr) {
					t.Fatalf("exit %d, err %v; want exit 2 containing %q", res.exit, res.err, ch.wantErr)
				}
				if n := len(srv.requests()); n != 0 {
					t.Errorf("%d request(s) sent, want none", n)
				}
			})
		}
	}
}

// Specs that name at least one path keep working, and --fields "" still
// overrides C1I_FIELDS. wantKeys nil means the full, unprojected row.
func TestFieldsSpecValidForms(t *testing.T) {
	cases := []struct {
		name, env string
		args      []string
		wantKeys  []string
	}{
		{"one path", "", []string{"--fields", "id"}, []string{"id"}},
		{"trailing comma", "", []string{"--fields", "id,"}, []string{"id"}},
		{"empty entry between", "", []string{"--fields", "id,,email"}, []string{"email", "id"}},
		{"env projection", "id", nil, []string{"id"}},
		{"empty flag overrides env", "id", []string{"--fields", ""}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnvFlags(t)
			useConfigFile(t, "")
			t.Setenv("C1I_FIELDS", tc.env)
			srv := newMutServer(t, fixedReply(200, usersListReply))
			res := runListCLI(t, srv.srv, append([]string{"users", "list"}, tc.args...)...)
			if res.exit != 0 {
				t.Fatalf("exit %d, err %v", res.exit, res.err)
			}
			var row map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSpace(res.stdout)), &row); err != nil {
				t.Fatalf("stdout %q: %v", res.stdout, err)
			}
			var keys []string
			for k := range row {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if tc.wantKeys == nil {
				if len(keys) <= 2 {
					t.Errorf("keys = %v, want the full row", keys)
				}
			} else if strings.Join(keys, ",") != strings.Join(tc.wantKeys, ",") {
				t.Errorf("keys = %v, want %v", keys, tc.wantKeys)
			}
		})
	}
}
