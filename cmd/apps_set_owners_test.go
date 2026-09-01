package cmd

import (
	"encoding/json"
	"testing"
)

// TestBuildSetOwnersBody pins the exact wire JSON for one and many owners: the
// key is "userIds" (not "user_ids"), unwrapped, order preserved.
func TestBuildSetOwnersBody(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"single", []string{"user-1111111111111111111111"}, `{"userIds":["user-1111111111111111111111"]}`},
		{"multiple", []string{"a", "b"}, `{"userIds":["a","b"]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(buildSetOwnersBody(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tc.want {
				t.Errorf("body = %s, want %s", b, tc.want)
			}
		})
	}
}

// TestAllOwnersPresent pins the --wait success condition: every wanted id
// must appear in the current ownerids response, extras are fine, order and
// duplicates don't matter, and an empty want list is trivially satisfied.
func TestAllOwnersPresent(t *testing.T) {
	cases := []struct {
		name string
		want []string
		got  []string
		ok   bool
	}{
		{"empty want", nil, []string{"a"}, true},
		{"exact match", []string{"a"}, []string{"a"}, true},
		{"subset of got", []string{"a"}, []string{"a", "b"}, true},
		{"multiple all present, different order", []string{"a", "b"}, []string{"b", "a", "c"}, true},
		{"missing one", []string{"a", "b"}, []string{"a"}, false},
		{"got empty", []string{"a"}, nil, false},
		{"both empty", nil, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := allOwnersPresent(tc.want, tc.got); got != tc.ok {
				t.Errorf("allOwnersPresent(%v, %v) = %v, want %v", tc.want, tc.got, got, tc.ok)
			}
		})
	}
}
