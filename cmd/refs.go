package cmd

import (
	"fmt"

	"github.com/ConductorOne/c1i/internal/client"
)

// refNotFoundError is a pre-send check's "no such <what>". It unwraps to the
// lookup's 404, so exitCode gives 4 (the server's answer for the same mistake)
// and the JSON error keeps the request's method, path, status and body, while
// the text stays one line.
type refNotFoundError struct {
	msg      string
	notFound *client.APIError
}

func (e *refNotFoundError) Error() string { return e.msg }
func (e *refNotFoundError) Unwrap() error { return e.notFound }

func refNotFound(flag, id, what string, notFound *client.APIError) error {
	return &refNotFoundError{fmt.Sprintf("--%s %q: no such %s", flag, id, what), notFound}
}
