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
func (e *refNotFoundError) Unwrap() error {
	if e.notFound == nil {
		return nil
	}
	return e.notFound
}

func refNotFound(flag, id, what string, notFound *client.APIError) error {
	return &refNotFoundError{fmt.Sprintf("--%s %q: no such %s", flag, id, what), notFound}
}

// refNotFoundLocal is refNotFound for a reference c1i itself failed to find in
// a successful response (a name absent from a list): no request returned 404,
// so the error carries no method, path or status.
func refNotFoundLocal(flag, id, what string) error {
	return &refNotFoundError{msg: fmt.Sprintf("--%s %q: no such %s", flag, id, what)}
}
