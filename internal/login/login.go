package login

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ConductorOne/c1i/internal/client"
	"github.com/ConductorOne/c1i/internal/transport"
)

// C1iClientID is the public OAuth client ID for the c1i CLI.
const C1iClientID = "juQSPDsPrdMDpPpR6fGdeLLSs8g"

// requestTimeout bounds a single HTTP call in the device flow (device
// authorization, one token poll, or personal-client creation) — generous for
// a simple request/response, but finite so a hung auth host can't stall
// `c1i auth login` forever. transport.DefaultTimeout (10 minutes, sized for a
// long-running MCP tool call) would technically also bound it, but a login
// call has no reason to need anywhere near that long, so this overrides it
// with a tighter, purpose-fit value. A var, not a const, so a test can lower
// it instead of waiting out the real duration.
var requestTimeout = 30 * time.Second

type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri_complete"`
	ExpiresIn       int64  `json:"expires_in"`
	Interval        int64  `json:"interval"`
}

type Credentials struct {
	ClientID     string
	ClientSecret string
}

// StartDeviceFlow initiates the OAuth device authorization flow.
func StartDeviceFlow(ctx context.Context, baseURL string, opts ...transport.Option) (*DeviceCode, error) {
	deviceURL := baseURL + "/auth/v1/device_authorization"

	vals := url.Values{}
	vals.Set("client_id", C1iClientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceURL, strings.NewReader(vals.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := transport.New(nil, append(opts, transport.WithTimeout(requestTimeout))...).Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		// Wrap as APIError so cmd/errors.go maps the status to the exit-code
		// taxonomy (auth/rate-limited/server) instead of collapsing to exit 1.
		return nil, fmt.Errorf("device authorization failed: %w", &client.APIError{Method: http.MethodPost, Path: req.URL.Path, StatusCode: resp.StatusCode, Body: string(resp.Body)})
	}

	var code DeviceCode
	if err := json.Unmarshal(resp.Body, &code); err != nil {
		return nil, fmt.Errorf("failed to parse device code response: %w", err)
	}

	if code.VerificationURI == "" {
		return nil, fmt.Errorf("server returned empty verification URI")
	}

	parsedURI, err := url.Parse(code.VerificationURI)
	if err != nil || strings.ToLower(parsedURI.Scheme) != "https" {
		return nil, fmt.Errorf("server returned verification URI with disallowed scheme (only https is permitted): %s", code.VerificationURI)
	}

	return &code, nil
}

// PollForToken polls the token endpoint until the user approves or the code
// expires. Each poll goes through the shared transport (timeout, user-agent,
// debug tracing, path/redirect guards) but with no retry layer of its own:
// the polling loop below IS this call's retry strategy, on the RFC 8628
// interval, so a second retry layer underneath would just double up delays.
// A 5xx therefore fails this poll immediately rather than being retried
// in-place — see the status handling below, which treats it identically to
// any other unparseable non-2xx body.
func PollForToken(ctx context.Context, baseURL string, code *DeviceCode, opts ...transport.Option) (*Credentials, error) {
	tokenURL := baseURL + "/auth/v1/token"
	t := transport.New(nil, append(opts, transport.WithMaxRetries(0), transport.WithTimeout(requestTimeout))...)

	vals := url.Values{}
	vals.Set("client_id", C1iClientID)
	vals.Set("device_code", code.DeviceCode)
	vals.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

	interval := code.Interval
	if interval < 1 {
		interval = 5
	}
	deadline := time.Now().Add(time.Duration(code.ExpiresIn) * time.Second)

	for {
		select {
		case <-time.After(time.Duration(interval) * time.Second):
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("device code expired")
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(vals.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := t.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			apiErr := func() error {
				return fmt.Errorf("token request failed: %w", &client.APIError{
					Method: http.MethodPost, Path: req.URL.Path, StatusCode: resp.StatusCode, Body: string(resp.Body),
				})
			}

			var errResp struct {
				Error       string `json:"error"`
				Description string `json:"error_description"`
			}
			// Only a body carrying a non-empty "error" is an OAuth error
			// response (RFC 6749 §5.2). A failure whose JSON merely happens to
			// unmarshal cleanly into this struct — e.g. a 5xx returning
			// {"message":"..."} — leaves Error empty and must NOT be read as
			// one, or a server failure would classify as an auth failure
			// (exit 3) instead of exit 6.
			if err := json.Unmarshal(resp.Body, &errResp); err != nil || errResp.Error == "" {
				return nil, apiErr()
			}

			// A 5xx is a server failure whatever its body says, so it stays on
			// the transport taxonomy (exit 6). This is checked BEFORE the
			// pending/slow_down cases deliberately: those two mean "keep
			// polling", and RFC 8628 has the server return them with HTTP 400,
			// so a 5xx carrying one is a malfunctioning server rather than a
			// grant still in progress. Letting it reach the switch would keep
			// polling through a real outage and then report the far less useful
			// "device code expired" at exit 1.
			if resp.StatusCode >= http.StatusInternalServerError {
				return nil, apiErr()
			}

			switch errResp.Error {
			case "authorization_pending":
				continue
			case "slow_down":
				interval += 5
				continue
			}

			// A genuine OAuth error (e.g. access_denied, expired_token) means
			// the caller is not authenticated — classify as an auth failure
			// (exit 3), not a generic error. Fall back to the error code when
			// the server omits a human-readable description.
			detail := errResp.Description
			if detail == "" {
				detail = errResp.Error
			}
			return nil, &client.AuthError{Err: fmt.Errorf("authorization failed: %s", detail)}
		}

		var tokenResp struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.Unmarshal(resp.Body, &tokenResp); err != nil {
			return nil, fmt.Errorf("failed to parse token response: %w", err)
		}

		return createPersonalClient(ctx, baseURL, tokenResp.AccessToken, opts...)
	}
}

func createPersonalClient(ctx context.Context, baseURL, accessToken string, opts ...transport.Option) (*Credentials, error) {
	pccURL := baseURL + "/api/v1/iam/personal_clients"

	reqBody, _ := json.Marshal(map[string]string{
		"display_name": "Created by c1i",
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pccURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := transport.New(nil, append(opts, transport.WithTimeout(requestTimeout))...).Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to create personal client: %w", &client.APIError{Method: http.MethodPost, Path: req.URL.Path, StatusCode: resp.StatusCode, Body: string(resp.Body)})
	}

	var clientResp struct {
		Client struct {
			ClientID string `json:"clientId"`
		} `json:"client"`
		ClientSecret string `json:"clientSecret"`
	}
	if err := json.Unmarshal(resp.Body, &clientResp); err != nil {
		return nil, fmt.Errorf("failed to parse client response: %w", err)
	}

	return &Credentials{
		ClientID:     clientResp.Client.ClientID,
		ClientSecret: clientResp.ClientSecret,
	}, nil
}
