//go:build windows

package selfupdate

// LockExecutable is a no-op because Windows upgrades never self-replace.
func LockExecutable(string) (func(), error) {
	return func() {}, nil
}
