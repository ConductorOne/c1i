package login

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// C1iClientID is the public OAuth client ID for the c1i CLI.
const C1iClientID = "juQSPDsPrdMDpPpR6fGdeLLSs8g"

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
func StartDeviceFlow(ctx context.Context, baseURL string) (*DeviceCode, error) {
	deviceURL := baseURL + "/auth/v1/device_authorization"

	vals := url.Values{}
	vals.Set("client_id", C1iClientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceURL, strings.NewReader(vals.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device authorization failed: %s", string(body))
	}

	var code DeviceCode
	if err := json.Unmarshal(body, &code); err != nil {
		return nil, fmt.Errorf("failed to parse device code response: %w", err)
	}

	if code.VerificationURI == "" {
		return nil, fmt.Errorf("server returned empty verification URI")
	}

	return &code, nil
}

// PollForToken polls the token endpoint until the user approves or the code expires.
func PollForToken(ctx context.Context, baseURL string, code *DeviceCode) (*Credentials, error) {
	tokenURL := baseURL + "/auth/v1/token"

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

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			var errResp struct {
				Error       string `json:"error"`
				Description string `json:"error_description"`
			}
			if err := json.Unmarshal(body, &errResp); err != nil {
				return nil, fmt.Errorf("token request failed: %s", string(body))
			}

			switch errResp.Error {
			case "authorization_pending":
				continue
			case "slow_down":
				interval += 5
				continue
			default:
				return nil, fmt.Errorf("authorization failed: %s", errResp.Description)
			}
		}

		var tokenResp struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.Unmarshal(body, &tokenResp); err != nil {
			return nil, fmt.Errorf("failed to parse token response: %w", err)
		}

		return createPersonalClient(ctx, baseURL, tokenResp.AccessToken)
	}
}

func createPersonalClient(ctx context.Context, baseURL, accessToken string) (*Credentials, error) {
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to create personal client: %s", string(body))
	}

	var clientResp struct {
		Client struct {
			ClientID string `json:"clientId"`
		} `json:"client"`
		ClientSecret string `json:"clientSecret"`
	}
	if err := json.Unmarshal(body, &clientResp); err != nil {
		return nil, fmt.Errorf("failed to parse client response: %w", err)
	}

	return &Credentials{
		ClientID:     clientResp.Client.ClientID,
		ClientSecret: clientResp.ClientSecret,
	}, nil
}
