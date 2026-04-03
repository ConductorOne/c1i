package config

import (
	"fmt"
	"net/url"
	"strings"
)

// ParseURL normalizes any of the following inputs to "https://{host}" (no trailing slash):
//   - Full URL: "https://c1internal.conductor.one/" → "https://c1internal.conductor.one"
//   - Raw domain: "c1internal.conductor.one" → "https://c1internal.conductor.one"
//   - Legacy short name: "c1internal" → "https://c1internal.conductor.one"
func ParseURL(input string) string {
	input = strings.TrimSpace(input)
	if strings.Contains(input, "://") {
		u, err := url.Parse(input)
		if err == nil && u.Host != "" {
			return "https://" + u.Host
		}
	}
	if strings.Contains(input, ".") {
		return "https://" + input
	}
	return fmt.Sprintf("https://%s.conductor.one", input)
}

// KeychainService returns the keychain service name for a given base URL.
func KeychainService(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err == nil && u.Host != "" {
		return "c1i/" + u.Host
	}
	return "c1i/" + baseURL
}

// LegacyKeychainService returns the old-style keychain service name if the host
// is a *.conductor.one domain, or empty string otherwise.
func LegacyKeychainService(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return ""
	}
	if strings.HasSuffix(u.Host, ".conductor.one") {
		shortName := strings.TrimSuffix(u.Host, ".conductor.one")
		if shortName != "" {
			return "c1i/" + shortName
		}
	}
	return ""
}
