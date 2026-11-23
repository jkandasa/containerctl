package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAuthFileResolvedAgainstStackDir(t *testing.T) {
	dir := t.TempDir()
	credDir := filepath.Join(dir, "creds")
	if err := os.MkdirAll(credDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(credDir, "auth.json"), []byte(`{"auths":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stackPath := writeStack(t, dir, `project: test
authfile: creds/auth.json
containers:
  - name: app
    image: alpine:latest
`)

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	s, err := Load(stackPath)
	if err != nil {
		t.Fatalf("Load from another directory: %v", err)
	}
	want := filepath.Join(dir, "creds", "auth.json")
	if abs, err := filepath.Abs(want); err == nil {
		want = abs
	}
	if s.AuthFile != want {
		t.Fatalf("AuthFile=%q want %q", s.AuthFile, want)
	}
}

func TestAuthFileMissingStillLoads(t *testing.T) {
	dir := t.TempDir()
	stackPath := writeStack(t, dir, `project: test
authfile: missing-auth.json
containers:
  - name: app
    image: alpine:latest
`)
	s, err := Load(stackPath)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "missing-auth.json")
	if s.AuthFile != want {
		t.Fatalf("AuthFile=%q want %q", s.AuthFile, want)
	}
}

func TestAuthFileBlockScalarTrimmed(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"auths":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stackPath := writeStack(t, dir, "project: test\nauthfile: |\n  auth.json\ncontainers:\n  - name: app\n    image: alpine:latest\n")
	s, err := Load(stackPath)
	if err != nil {
		t.Fatal(err)
	}
	if s.AuthFile != authPath {
		t.Fatalf("AuthFile=%q want %q", s.AuthFile, authPath)
	}
}

func TestAuthFileAbsoluteKept(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"auths":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stackPath := writeStack(t, dir, "project: test\nauthfile: "+authPath+"\ncontainers:\n  - name: app\n    image: alpine:latest\n")
	s, err := Load(stackPath)
	if err != nil {
		t.Fatal(err)
	}
	want := authPath
	if abs, err := filepath.Abs(authPath); err == nil {
		want = abs
	}
	if s.AuthFile != want {
		t.Fatalf("AuthFile=%q want %q", s.AuthFile, want)
	}
}

func TestAuthFileLegacyKey(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(authPath, []byte(`{"auths":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stackPath := writeStack(t, dir, `project: test
auth_file: auth.json
containers:
  - name: app
    image: alpine:latest
`)
	s, err := Load(stackPath)
	if err != nil {
		t.Fatal(err)
	}
	if s.AuthFile != authPath {
		t.Fatalf("AuthFile=%q want %q", s.AuthFile, authPath)
	}
}

func TestAuthFileBothKeysDisagree(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.json", "b.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(`{"auths":{}}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stackPath := writeStack(t, dir, `project: test
authfile: a.json
auth_file: b.json
containers:
  - name: app
    image: alpine:latest
`)
	_, err := Load(stackPath)
	if err == nil {
		t.Fatal("expected error when authfile and auth_file differ")
	}
}
