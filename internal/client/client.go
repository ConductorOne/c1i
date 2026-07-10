package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime/debug"

	"github.com/ConductorOne/c1i/internal/config"
	"github.com/ConductorOne/c1i/internal/keychain"
	"github.com/ConductorOne/c1i/internal/tokensource"
	"golang.org/x/oauth2"
)

var userAgent = func() string {
	version := "dev"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	return "c1.ai/c1i (version=" + version + ")"
}()

type userAgentTripper struct {
	next http.RoundTripper
}

func (uat *userAgentTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", userAgent)
	return uat.next.RoundTrip(req)
}

type Client struct {
	httpClient *http.Client
	baseURL    string
}

func New(ctx context.Context, baseURL string) (*Client, error) {
	service := config.KeychainService(baseURL)
	clientID, clientSecret, _, err := keychain.Load(service)
	if err != nil {
		// Try legacy keychain key for *.conductor.one domains.
		legacyService := config.LegacyKeychainService(baseURL)
		if legacyService != "" && legacyService != service {
			clientID, clientSecret, _, err = keychain.Load(legacyService)
			if err == nil {
				// Migrate: store under new key and delete old.
				_, _ = keychain.Store(service, clientID, clientSecret)
				_, _ = keychain.Delete(legacyService)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("loading credentials: %w", err)
		}
	}

	tokenSource, err := tokensource.NewTokenSource(ctx, clientID, clientSecret, baseURL)
	if err != nil {
		return nil, fmt.Errorf("creating token source: %w", err)
	}

	oauthClient := oauth2.NewClient(ctx, tokenSource)
	oauthClient.Transport = &userAgentTripper{next: oauthClient.Transport}

	return &Client{
		httpClient: oauthClient,
		baseURL:    baseURL,
	}, nil
}

func (c *Client) Get(ctx context.Context, path string, queryParams map[string]string) ([]byte, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, err
	}
	if len(queryParams) > 0 {
		q := u.Query()
		for k, v := range queryParams {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func (c *Client) Post(ctx context.Context, path string, body any) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *Client) Put(ctx context.Context, path string, body any) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c *Client) Delete(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API %s %s returned %d: %s", req.Method, req.URL.Path, resp.StatusCode, string(body))
	}
	return body, nil
}
