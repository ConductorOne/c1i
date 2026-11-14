package config

import (
	"strings"
	"testing"
)

// A rejected URL is echoed in the error; secret-named parameters are masked
// and the rest stays.
func TestParseURLErrorRedactsSecrets(t *testing.T) {
	for _, in := range []string{"acme?token=SEKR3T&ok=1", "https://h.example:badport?token=SEKR3T&ok=1"} {
		_, _, err := ParseURL(in)
		if err == nil {
			t.Fatalf("ParseURL(%q): want an error", in)
		}
		if s := err.Error(); strings.Contains(s, "SEKR3T") || !strings.Contains(s, "ok=1") {
			t.Errorf("ParseURL(%q) error = %q", in, s)
		}
	}
}
