package upgrade

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFileSHA256AndDigestHex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin")
	if err := os.WriteFile(path, []byte("hello"), 0o755); err != nil {
		t.Fatal(err)
	}
	sum, err := FileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte("hello"))
	if sum != hex.EncodeToString(want[:]) {
		t.Fatalf("got %s", sum)
	}
	if digestHex("sha256:"+sum) != sum {
		t.Fatal("digestHex should strip sha256: prefix")
	}
	if digestHex("deadbeef") != "" {
		t.Fatal("short digest is invalid")
	}
}

func TestInstallerReplace(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "containerctl")
	if err := os.WriteFile(dest, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	payload := []byte("new-binary-contents")
	sum := sha256.Sum256(payload)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	inst := &Installer{HTTP: srv.Client()}
	err := inst.Replace(context.Background(), dest, &Asset{
		BrowserDownloadURL: srv.URL + "/containerctl-linux-amd64",
		Size:               int64(len(payload)),
		Digest:             "sha256:" + hex.EncodeToString(sum[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("dest = %q", got)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatal("replaced binary is not executable")
	}
}

func TestInstallerReplaceSizeMismatch(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "containerctl")
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("short"))
	}))
	t.Cleanup(srv.Close)

	inst := &Installer{HTTP: srv.Client()}
	err := inst.Replace(context.Background(), dest, &Asset{
		BrowserDownloadURL: srv.URL,
		Size:               100,
	})
	if err == nil {
		t.Fatal("expected size mismatch")
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "old" {
		t.Fatalf("dest should be unchanged, got %q", got)
	}
}
