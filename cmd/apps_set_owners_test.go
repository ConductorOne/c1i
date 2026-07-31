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
		{"single", []string{"3CPjiTqq4nDUa3cE8A8VdNu3rqL"}, `{"userIds":["3CPjiTqq4nDUa3cE8A8VdNu3rqL"]}`},
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
