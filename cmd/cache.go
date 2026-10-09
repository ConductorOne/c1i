package cmd

import (
	"os"
	"path/filepath"
)

// cacheFiles is every file c1i keeps in ~/.c1i/cache. Anything else there is
// left over from an older release and is deleted when the cache is written, so
// renaming a cache file needs no cleanup rule of its own. Add a new cache file
// here or it will be deleted on every refresh.
var cacheFiles = map[string]bool{cacheFileName: true}

// pruneCacheDir is best-effort: cleanup must never fail the command. It skips
// subdirectories, a symlinked dir (Lstat sees a link, not a dir) whose target
// may hold files c1i doesn't own, and a relative dir, which is what an unset
// HOME yields and would sweep the working directory instead.
func pruneCacheDir(dir string) {
	if !filepath.IsAbs(dir) {
		return
	}
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || cacheFiles[e.Name()] {
			continue
		}
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
}
