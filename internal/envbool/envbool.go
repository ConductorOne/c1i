// Package envbool is the one boolean parser for env vars, config values and
// flags, so every source accepts and rejects the same spellings.
package envbool

import (
	"errors"
	"strconv"
)

// Parse accepts what strconv.ParseBool does (1, t, T, TRUE, true, True, 0, ...).
func Parse(s string) (bool, error) {
	b, err := strconv.ParseBool(s)
	if err != nil {
		return false, errors.New("must be a boolean (true/false, 1/0)")
	}
	return b, nil
}
