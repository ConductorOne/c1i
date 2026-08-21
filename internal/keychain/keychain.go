// Package keychain stores and retrieves C1 API credentials. It tries three
// backends, in order: environment variables (read-only), the OS keyring
// (Keychain on macOS, Credential Manager on Windows, Secret Service on
// Linux), and a 0600 file under os.UserConfigDir(). The file backend is used
// when the OS keyring is unavailable — typical on headless Linux servers,
// containers, CI runners, and WSL without a desktop environment.
package keychain

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

// Backend identifies where credentials were read from or written to.
type Backend string

const (
	BackendEnv     Backend = "env"
	BackendKeyring Backend = "keyring"
	BackendFile    Backend = "file"
)

const (
	envClientID     = "C1I_CLIENT_ID"
	envClientSecret = "C1I_CLIENT_SECRET" // #nosec G101 -- env var name, not a credential value

	acctClientID     = "client_id"
	acctClientSecret = "client_secret"
)

// ErrNoCredentials indicates the file backend has no stored credential for a
// service. It lets Load tell "never logged in" (file store genuinely empty)
// apart from "credential exists in the keyring but the keyring is currently
// unreachable" — loadFile's error text alone can't be classified with
// errors.Is, so it wraps this sentinel.
var ErrNoCredentials = errors.New("no credentials found")

// Store persists credentials. Tries the OS keyring first; falls back to a
// 0600 file when no keyring is available. Env vars are never written.
// Returns the backend used so callers can inform the user.
func Store(service, clientID, clientSecret string) (Backend, error) {
	_, _ = Delete(service)

	if err := storeKeyring(service, clientID, clientSecret); err == nil {
		return BackendKeyring, nil
	} else if !isKeyringUnavailable(err) {
		return "", fmt.Errorf("storing in keyring: %w", err)
	}

	if err := storeFile(service, clientID, clientSecret); err != nil {
		return "", err
	}
	return BackendFile, nil
}

// Load returns credentials for service. Precedence: env vars (if both are
// set), OS keyring, file fallback. Returns the backend used.
func Load(service string) (string, string, Backend, error) {
	if id, sec := os.Getenv(envClientID), os.Getenv(envClientSecret); id != "" && sec != "" {
		return id, sec, BackendEnv, nil
	}

	id, sec, keyringErr := loadKeyring(service)
	unavailable := false
	switch {
	case keyringErr == nil:
		return id, sec, BackendKeyring, nil
	case errors.Is(keyringErr, keyring.ErrNotFound):
		// Fall through to file: nothing was ever stored.
	case isKeyringUnavailable(keyringErr):
		// Fall through to file, but remember why: a credential may exist in
		// the keyring and simply be unreachable right now. If the file store
		// also comes up empty, that distinction changes what we tell the user.
		unavailable = true
	default:
		return "", "", "", fmt.Errorf("loading from keyring: %w", keyringErr)
	}

	id, sec, fileErr := loadFile(service)
	if fileErr != nil {
		if unavailable && errors.Is(fileErr, ErrNoCredentials) {
			return "", "", "", fmt.Errorf(
				"no credentials found for %s in the file store, and the OS keyring is currently unavailable (%w) — "+
					"if a credential was saved to the keyring earlier, it is unreachable until the keyring is "+
					"available again; running 'c1i auth login' now stores a new credential in the file store, "+
					"not the keyring",
				service, keyringErr)
		}
		return "", "", "", fileErr
	}
	return id, sec, BackendFile, nil
}

// Delete removes credentials from every writable backend (keyring + file).
// Env vars are not touched. Best-effort: missing entries are not errors.
// Keyring delete errors are swallowed (the entry may simply not exist or the
// keyring may be unavailable); file errors are surfaced because they indicate
// a local FS problem the caller should know about. The returned bool reports
// whether anything was actually removed, so callers can distinguish a real
// logout from a no-op.
func Delete(service string) (bool, error) {
	removed := keyring.Delete(service, acctClientID) == nil
	if keyring.Delete(service, acctClientSecret) == nil {
		removed = true
	}
	fileRemoved, err := deleteFile(service)
	return removed || fileRemoved, err
}

// EnvCredentialsSet reports whether both credential env vars are set. When they
// are, Load uses them regardless of keyring/file contents, and Delete cannot
// remove them — so callers (e.g. logout) can warn the user accordingly.
func EnvCredentialsSet() bool {
	return os.Getenv(envClientID) != "" && os.Getenv(envClientSecret) != ""
}

// FilePath returns the path the file backend uses for service, even if no
// file currently exists. Useful for telling the user where credentials were
// written when the keyring was unavailable.
func FilePath(service string) (string, error) {
	return filePath(service)
}

func storeKeyring(service, clientID, clientSecret string) error {
	if err := keyring.Set(service, acctClientID, clientID); err != nil {
		return err
	}
	if err := keyring.Set(service, acctClientSecret, clientSecret); err != nil {
		_ = keyring.Delete(service, acctClientID)
		return err
	}
	return nil
}

func loadKeyring(service string) (string, string, error) {
	id, err := keyring.Get(service, acctClientID)
	if err != nil {
		return "", "", err
	}
	sec, err := keyring.Get(service, acctClientSecret)
	if err != nil {
		return "", "", err
	}
	return id, sec, nil
}

// isKeyringUnavailable reports whether err indicates the OS has no usable
// keyring backend (no D-Bus session, no Secret Service provider, etc.) — as
// opposed to a real failure like a corrupt entry. Linux is the messy case:
// go-keyring returns ErrUnsupportedPlatform in some versions but bubbles up
// raw D-Bus errors in others, so we also string-match the well-known cases.
func isKeyringUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, keyring.ErrUnsupportedPlatform) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "org.freedesktop.secrets") ||
		strings.Contains(msg, "Cannot autolaunch D-Bus") ||
		strings.Contains(msg, "DBUS_SESSION_BUS_ADDRESS") ||
		strings.Contains(msg, "dbus-launch") ||
		strings.Contains(msg, "no usable backend")
}

type fileCreds struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func filePath(service string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating config dir: %w", err)
	}
	return filepath.Join(dir, "c1i", "credentials", sanitizeService(service)+".json"), nil
}

// sanitizeService maps a service name (e.g. "c1i/example.conductor.one") to a
// safe filename component. Service names originate from a parsed URL host so
// in practice contain only letters/digits/dots/hyphens plus the "c1i/" prefix,
// but we strip path separators and ".." defensively to keep the file under
// the credentials directory regardless of caller input.
func sanitizeService(s string) string {
	r := strings.NewReplacer(
		string(filepath.Separator), "_",
		"/", "_",
		"\\", "_",
		":", "_",
		"..", "_",
	)
	return r.Replace(s)
}

func storeFile(service, clientID, clientSecret string) error {
	p, err := filePath(service)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("creating credentials dir: %w", err)
	}
	b, err := json.Marshal(fileCreds{ClientID: clientID, ClientSecret: clientSecret}) // #nosec G117 -- serializing for the 0600 credentials file, not logging or a response
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("writing credentials: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("finalizing credentials: %w", err)
	}
	return nil
}

func loadFile(service string) (string, string, error) {
	p, err := filePath(service)
	if err != nil {
		return "", "", err
	}
	b, err := os.ReadFile(p) // #nosec G304 -- service derives from --url, but sanitizeService strips path-traversal characters before the join
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", fmt.Errorf("%w for %s: run 'c1i auth login'", ErrNoCredentials, service)
		}
		return "", "", fmt.Errorf("reading credentials: %w", err)
	}
	var c fileCreds
	if err := json.Unmarshal(b, &c); err != nil {
		return "", "", fmt.Errorf("parsing credentials: %w", err)
	}
	if c.ClientID == "" || c.ClientSecret == "" {
		return "", "", fmt.Errorf("credentials file %s is incomplete", p)
	}
	return c.ClientID, c.ClientSecret, nil
}

// deleteFile removes the file-backend credential for service. The returned
// bool reports whether a file was actually removed (false if none existed).
func deleteFile(service string) (bool, error) {
	p, err := filePath(service)
	if err != nil {
		return false, err
	}
	if err := os.Remove(p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("deleting credentials: %w", err)
	}
	return true, nil
}
