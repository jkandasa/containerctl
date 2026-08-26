package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// FileSHA256 returns the lowercase hex sha256 of path.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func digestHex(d string) string {
	d = strings.TrimSpace(d)
	d = strings.TrimPrefix(strings.ToLower(d), "sha256:")
	if len(d) != 64 {
		return ""
	}
	for i := 0; i < len(d); i++ {
		c := d[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return ""
		}
	}
	return d
}

// Installer downloads a release asset and replaces dest atomically.
type Installer struct {
	HTTP *http.Client
}

func (in *Installer) http() *http.Client {
	if in != nil && in.HTTP != nil {
		return in.HTTP
	}
	return &http.Client{Timeout: 2 * time.Minute}
}

// Replace downloads asset into dest (the running executable), preserving mode.
func (in *Installer) Replace(ctx context.Context, dest string, asset *Asset) error {
	if asset == nil || asset.BrowserDownloadURL == "" {
		return fmt.Errorf("no download URL")
	}
	dest, err := filepath.EvalSymlinks(dest)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", dest, err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		return fmt.Errorf("stat %s: %w", dest, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "containerctl")
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := in.http().Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	// Write next to dest, then rename over it. Do not open dest for write:
	// Linux returns ETXTBSY ("text file busy") for a running executable.
	// Rename only updates the directory entry; the old inode stays mapped.
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, ".containerctl-upgrade-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	h := sha256.New()
	n, err := io.Copy(tmp, io.TeeReader(resp.Body, h))
	if err != nil {
		_ = tmp.Close()
		return fmt.Errorf("download: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if asset.Size > 0 && n != asset.Size {
		return fmt.Errorf("download: got %d bytes, expected %d", n, asset.Size)
	}
	if want := digestHex(asset.Digest); want != "" {
		if got := hex.EncodeToString(h.Sum(nil)); got != want {
			return fmt.Errorf("download: sha256 mismatch")
		}
	}
	if err := os.Chmod(tmpName, info.Mode()); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	return replaceFile(tmpName, dest)
}

func replaceFile(src, dest string) error {
	if runtime.GOOS == "windows" {
		old := dest + ".old"
		_ = os.Remove(old)
		if err := os.Rename(dest, old); err != nil {
			return fmt.Errorf("replace: %w", err)
		}
		if err := os.Rename(src, dest); err != nil {
			_ = os.Rename(old, dest)
			return fmt.Errorf("replace: %w", err)
		}
		_ = os.Remove(old)
		return nil
	}
	if err := os.Rename(src, dest); err != nil {
		return fmt.Errorf("replace: %w", err)
	}
	return nil
}
