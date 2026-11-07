package cmd

import (
	"testing"
)

func TestParseKeyValueFlag(t *testing.T) {
	m, err := parseKeyValueFlag([]string{"A=1", "B=x=y"}, "header")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A value may itself contain '=' — only the first '=' splits key from value.
	if m["A"] != "1" || m["B"] != "x=y" {
		t.Errorf("parsed = %v, want A=1 B=x=y", m)
	}

	if _, err := parseKeyValueFlag([]string{"nope"}, "header"); err == nil {
		t.Error("expected error for value with no '='")
	}
	if _, err := parseKeyValueFlag([]string{"=v"}, "header"); err == nil {
		t.Error("expected error for empty key")
	}

	m, err = parseKeyValueFlag(nil, "header")
	if err != nil || m != nil {
		t.Errorf("empty input should return (nil, nil), got (%v, %v)", m, err)
	}
}
