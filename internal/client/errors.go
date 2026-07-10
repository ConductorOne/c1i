package client

import "fmt"

// APIError is returned when the C1 API responds with a non-2xx status. It
// carries the structured pieces so callers can branch on StatusCode and render
// machine-readable errors, while Error() preserves the original human string.
type APIError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API %s %s returned %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}

// AuthError wraps failures to load credentials or mint a token — i.e. the
// caller is not authenticated, as opposed to an API request being rejected.
// Callers use errors.As to map it to an auth-specific exit code.
type AuthError struct{ Err error }

func (e *AuthError) Error() string { return e.Err.Error() }
func (e *AuthError) Unwrap() error { return e.Err }
