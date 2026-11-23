package docker

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistryAuthExplicitFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	encoded := base64.StdEncoding.EncodeToString([]byte("user:secret"))
	body := `{"auths":{"example.invalid":{"auth":"` + encoded + `"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := registryAuth(path, "example.invalid/app:1")
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Fatal("expected registry auth from explicit file")
	}
}

func TestRegistryAuthExplicitFileMissing(t *testing.T) {
	_, err := registryAuth(filepath.Join(t.TempDir(), "missing.json"), "example.invalid/app:1")
	if err == nil {
		t.Fatal("expected error for missing auth file")
	}
	if !strings.Contains(err.Error(), "auth file") {
		t.Fatalf("error %q should mention auth file", err)
	}
}

func TestRegistryAuthExplicitFileBadEncoding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	body := `{"auths":{"example.invalid":{"auth":"not-valid!!!"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := registryAuth(path, "example.invalid/app:1")
	if err == nil {
		t.Fatal("expected error for undecodable auth entry")
	}
}

func TestRegistryAuthExplicitFileInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := registryAuth(path, "example.invalid/app:1")
	if err == nil {
		t.Fatal("expected error for invalid auth file")
	}
}
