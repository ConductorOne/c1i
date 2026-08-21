package client

import "github.com/ConductorOne/c1i/internal/transport"

// APIError, PathError, RedirectError, and RedirectLoopError are defined in
// internal/transport (the auth-independent layer beneath this client) and
// aliased here so existing callers (cmd/errors.go's errors.As chain,
// internal/login, internal/mcpgateway) keep working unchanged: an alias is
// the same type, so errors.As(err, &client.PathError{}) still matches a
// *transport.PathError returned from deeper in the stack.
type (
	APIError          = transport.APIError
	PathError         = transport.PathError
	RedirectError     = transport.RedirectError
	RedirectLoopError = transport.RedirectLoopError
)

// AuthError wraps failures to load credentials or mint a token — i.e. the
// caller is not authenticated, as opposed to an API request being rejected.
// Callers use errors.As to map it to an auth-specific exit code. Unlike the
// aliases above, this is not a transport concern: it classifies *why* a
// transport-level failure happened (a rejected client_credentials grant, or
// no stored credentials at all), which only this OAuth-aware layer knows.
type AuthError struct{ Err error }

func (e *AuthError) Error() string { return e.Err.Error() }
func (e *AuthError) Unwrap() error { return e.Err }
