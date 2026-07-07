package keychain

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/zalando/go-keyring"
)

const (
	testService = "c1i/test.example.com"
	testID      = "test-client-id"
	testSecret  = "test-client-secret"
)

// withTempConfigDir redirects os.UserConfigDir() at the platform-appropriate
// env var to a temp dir, so the file backend reads/writes under the test dir.
func withTempConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", dir)
	case "darwin":
		t.Setenv("HOME", dir)
	default:
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
	return dir
}

// clearEnv removes the credential env vars for the duration of the test.
func clearEnv(t *testing.T) {
	t.Helper()
	t.Setenv(envClientID, "")
	t.Setenv(envClientSecret, "")
	_ = os.Unsetenv(envClientID)
	_ = os.Unsetenv(envClientSecret)
}

func TestStoreLoadKeyringHappyPath(t *testing.T) {
	keyring.MockInit()
	withTempConfigDir(t)
	clearEnv(t)

	backend, err := Store(testService, testID, testSecret)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if backend != BackendKeyring {
		t.Fatalf("Store backend = %q, want %q", backend, BackendKeyring)
	}

	id, sec, b, err := Load(testService)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if id != testID || sec != testSecret {
		t.Fatalf("Load = (%q, %q), want (%q, %q)", id, sec, testID, testSecret)
	}
	if b != BackendKeyring {
		t.Fatalf("Load backend = %q, want %q", b, BackendKeyring)
	}
}

func TestEnvOverridesKeyring(t *testing.T) {
	keyring.MockInit()
	withTempConfigDir(t)

	if _, err := Store(testService, "keyring-id", "keyring-sec"); err != nil {
		t.Fatalf("Store: %v", err)
	}

	t.Setenv(envClientID, "env-id")
	t.Setenv(envClientSecret, "env-sec")

	id, sec, backend, err := Load(testService)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if id != "env-id" || sec != "env-sec" {
		t.Fatalf("Load = (%q, %q), want env values", id, sec)
	}
	if backend != BackendEnv {
		t.Fatalf("backend = %q, want %q", backend, BackendEnv)
	}
}

func TestEnvIgnoredWhenOnlyOneSet(t *testing.T) {
	keyring.MockInit()
	withTempConfigDir(t)
	clearEnv(t)

	if _, err := Store(testService, testID, testSecret); err != nil {
		t.Fatalf("Store: %v", err)
	}

	t.Setenv(envClientID, "env-id")
	// envClientSecret deliberately not set

	id, sec, backend, err := Load(testService)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if id != testID || sec != testSecret {
		t.Fatalf("expected stored values when only one env var set, got (%q, %q)", id, sec)
	}
	if backend != BackendKeyring {
		t.Fatalf("backend = %q, want %q", backend, BackendKeyring)
	}
}

func TestFileFallbackWhenKeyringUnavailable(t *testing.T) {
	keyring.MockInitWithError(keyring.ErrUnsupportedPlatform)
	dir := withTempConfigDir(t)
	clearEnv(t)

	backend, err := Store(testService, testID, testSecret)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if backend != BackendFile {
		t.Fatalf("Store backend = %q, want %q", backend, BackendFile)
	}

	// Verify the file was written with the right contents and lives under the
	// platform config dir with a sanitized filename.
	want := filepath.Join(configRoot(dir), "c1i", "credentials", "c1i_test.example.com.json")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected credentials file at %s: %v", want, err)
	}

	id, sec, b, err := Load(testService)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if id != testID || sec != testSecret {
		t.Fatalf("Load = (%q, %q), want (%q, %q)", id, sec, testID, testSecret)
	}
	if b != BackendFile {
		t.Fatalf("Load backend = %q, want %q", b, BackendFile)
	}
}

// configRoot returns the subdirectory of the test root that os.UserConfigDir
// resolves to on the current platform.
func configRoot(testDir string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(testDir, "Library", "Application Support")
	default:
		return testDir
	}
}

func TestFileFallbackBubblesRealKeyringErrors(t *testing.T) {
	keyring.MockInitWithError(errors.New("keyring is corrupted"))
	withTempConfigDir(t)
	clearEnv(t)

	if _, err := Store(testService, testID, testSecret); err == nil {
		t.Fatalf("expected non-availability error to surface, got nil")
	}
}

func TestDeleteClearsBothBackends(t *testing.T) {
	keyring.MockInit()
	withTempConfigDir(t)
	clearEnv(t)

	// Plant credentials in both backends.
	if _, err := Store(testService, testID, testSecret); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := storeFile(testService, "file-id", "file-sec"); err != nil {
		t.Fatalf("storeFile: %v", err)
	}

	if err := Delete(testService); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, _, _, err := Load(testService); err == nil {
		t.Fatalf("expected Load to fail after Delete")
	}
}

func TestSanitizeService(t *testing.T) {
	cases := map[string]string{
		"c1i/example.conductor.one": "c1i_example.conductor.one",
		"c1i\\windowsy":             "c1i_windowsy",
		"with:colon":                "with_colon",
		"plain":                     "plain",
		"../../etc/passwd":          "____etc_passwd",
	}
	for in, want := range cases {
		if got := sanitizeService(in); got != want {
			t.Errorf("sanitizeService(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsKeyringUnavailable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unsupported sentinel", keyring.ErrUnsupportedPlatform, true},
		{"freedesktop secrets", errors.New("The name org.freedesktop.secrets was not provided by any .service files"), true},
		{"dbus autolaunch", errors.New("Cannot autolaunch D-Bus without X11 $DISPLAY"), true},
		{"missing dbus session", errors.New("DBUS_SESSION_BUS_ADDRESS not set"), true},
		{"dbus-launch not found", errors.New(`exec: "dbus-launch": executable file not found in $PATH`), true},
		{"no usable backend", errors.New("no usable backend found"), true},
		{"unrelated", errors.New("disk full"), false},
		{"not found", keyring.ErrNotFound, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isKeyringUnavailable(tc.err); got != tc.want {
				t.Errorf("isKeyringUnavailable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestFileBackendRejectsMalformed(t *testing.T) {
	dir := withTempConfigDir(t)
	clearEnv(t)
	keyring.MockInitWithError(keyring.ErrUnsupportedPlatform)

	p := filepath.Join(configRoot(dir), "c1i", "credentials", "c1i_test.example.com.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := Load(testService); err == nil {
		t.Fatalf("expected error on malformed credentials file")
	}
}

func TestFileBackendRejectsIncomplete(t *testing.T) {
	dir := withTempConfigDir(t)
	clearEnv(t)
	keyring.MockInitWithError(keyring.ErrUnsupportedPlatform)

	// Write a file with empty client_secret to simulate corruption.
	p := filepath.Join(configRoot(dir), "c1i", "credentials", "c1i_test.example.com.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"client_id":"x","client_secret":""}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := Load(testService); err == nil {
		t.Fatalf("expected error on incomplete credentials file")
	}
}
