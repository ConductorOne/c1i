package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Apply downloads asset, verifies its sha256, extracts the c1i binary, and
// atomically replaces the running executable at execPath. It never overwrites
// execPath unless the replacement is fully staged and verified, so a failed or
// interrupted upgrade leaves the current binary intact.
func (c *Client) Apply(ctx context.Context, asset Asset, execPath string) error {
	raw, err := c.download(ctx, asset.Href)
	if err != nil {
		return err
	}
	if err := verifySHA256(raw, asset.SHA256); err != nil {
		return err
	}
	bin, err := extractBinary(raw, asset.Filename)
	if err != nil {
		return err
	}
	return replaceExecutable(execPath, bin)
}

func (c *Client) download(ctx context.Context, url string) ([]byte, error) {
	if err := c.validateURL(url); err != nil {
		return nil, err
	}
	body, err := c.get(ctx, c.downloadDoer(), url)
	if err != nil {
		return nil, err
	}
	// Belt-and-suspenders: the backing transport is bounded to MaxArtifactBytes,
	// but a test Doer or an unbounded transport is not, so re-check here.
	if len(body) > MaxArtifactBytes {
		return nil, fmt.Errorf("downloading %s: artifact exceeds %d bytes", url, MaxArtifactBytes)
	}
	return body, nil
}

func verifySHA256(data []byte, want string) error {
	if want == "" {
		return fmt.Errorf("manifest asset has no sha256; refusing to install unverified binary")
	}
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch: downloaded %s, manifest says %s", got, want)
	}
	return nil
}

// extractBinary pulls the c1i executable out of a release archive. filename
// names the archive (…-linux-amd64.tar.gz or …-darwin-arm64.zip); the binary
// inside is named "c1i".
func extractBinary(archive []byte, filename string) ([]byte, error) {
	switch {
	case strings.HasSuffix(filename, ".tar.gz") || strings.HasSuffix(filename, ".tgz"):
		return fromTarGz(archive)
	case strings.HasSuffix(filename, ".zip"):
		return fromZip(archive)
	default:
		return nil, fmt.Errorf("unsupported archive %q", filename)
	}
}

func fromTarGz(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading tar: %w", err)
		}
		if isC1iEntry(hdr.Name) && hdr.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(tr, MaxArtifactBytes))
		}
	}
	return nil, fmt.Errorf("no c1i binary found in archive")
}

func fromZip(archive []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("reading zip: %w", err)
	}
	for _, f := range zr.File {
		// IsRegular() skips symlink/dir/device entries (mirrors fromTarGz's
		// tar.TypeReg check), so a crafted archive can't smuggle a symlink
		// named c1i in place of the real binary.
		if isC1iEntry(f.Name) && f.Mode().IsRegular() {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("opening %s in zip: %w", f.Name, err)
			}
			defer func() { _ = rc.Close() }()
			return io.ReadAll(io.LimitReader(rc, MaxArtifactBytes))
		}
	}
	return nil, fmt.Errorf("no c1i binary found in archive")
}

// isC1iEntry matches the c1i executable whether it sits at the archive root or
// under a directory, on either archive style.
func isC1iEntry(name string) bool {
	base := path.Base(filepath.ToSlash(name))
	return base == "c1i" || base == "c1i.exe"
}

// replaceExecutable atomically swaps newBinary in for the file at execPath. The
// new binary is written to a temp file in the SAME directory (so the final
// rename stays on one filesystem and is atomic), made executable, then renamed
// over execPath — which POSIX permits even while the old binary is running.
func replaceExecutable(execPath string, newBinary []byte) error {
	dir := filepath.Dir(execPath)
	tmp, err := os.CreateTemp(dir, ".c1i-upgrade-*")
	if err != nil {
		return fmt.Errorf("staging upgrade in %s: %w (is the install directory writable?)", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(newBinary); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("writing staged binary: %w", err)
	}
	// Flush the staged bytes to disk before the rename so a crash can't leave a
	// renamed-but-empty file that shadows the working binary.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("syncing staged binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("closing staged binary: %w", err)
	}
	// Match a normal executable's mode; preserve the existing binary's mode if
	// we can read it, else fall back to 0755.
	mode := os.FileMode(0o755)
	if fi, err := os.Stat(execPath); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		cleanup()
		return fmt.Errorf("setting mode on staged binary: %w", err)
	}
	if err := os.Rename(tmpName, execPath); err != nil {
		cleanup()
		return fmt.Errorf("replacing %s: %w", execPath, err)
	}
	// Best-effort: fsync the containing directory so the rename itself is
	// durable. A failure here doesn't undo a successful rename, so don't fail
	// the upgrade over it.
	syncDir(dir)
	return nil
}

// syncDir flushes a directory's own metadata (the rename entry) to disk. Errors
// are ignored: some platforms/filesystems don't permit opening a directory for
// sync, and the rename has already succeeded.
func syncDir(dir string) {
	d, err := os.Open(dir) // #nosec G304 -- dir is filepath.Dir(execPath), the install directory, not attacker input
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
