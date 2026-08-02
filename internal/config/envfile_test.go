package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeStack(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEnvFileListForm(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "secrets.env"), []byte("ADMIN_TOKEN=secret123\nFOO=from_file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writeStack(t, dir, `project: test
data_path: ./data
containers:
  - name: app
    image: alpine:latest
    env_file:
      - "secrets.env"
    env:
      FOO: from_inline
      TZ: UTC
`)

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	c := s.ContainerByName("app")
	if c == nil {
		t.Fatal("container not found")
	}
	if c.Env["ADMIN_TOKEN"] != "secret123" {
		t.Errorf("ADMIN_TOKEN=%q want secret123", c.Env["ADMIN_TOKEN"])
	}
	if c.Env["FOO"] != "from_inline" {
		t.Errorf("FOO=%q want from_inline (inline overrides file)", c.Env["FOO"])
	}
	if c.Env["TZ"] != "UTC" {
		t.Errorf("TZ=%q", c.Env["TZ"])
	}
	if len(c.EnvFile) != 0 {
		t.Errorf("EnvFile should be cleared after resolve, got %v", c.EnvFile)
	}
}

func TestEnvFileStringForm(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "secrets.env")
	if err := os.WriteFile(envPath, []byte("A=1\nB=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Compose-compatible single-string form (no list).
	path := writeStack(t, dir, `project: test
containers:
  - name: app
    image: alpine:latest
    env_file: secrets.env
`)

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load string form: %v", err)
	}
	c := s.Containers[0]
	if c.Env["A"] != "1" || c.Env["B"] != "2" {
		t.Errorf("Env=%v want A=1 B=2", c.Env)
	}
}

func TestEnvFileRelativeToStackDirWithoutDataPath(t *testing.T) {
	proj := t.TempDir()
	if err := os.WriteFile(filepath.Join(proj, "secrets.env"), []byte("FROM_STACK_DIR=yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stackPath := writeStack(t, proj, `project: test
containers:
  - name: app
    image: alpine:latest
    env_file:
      - secrets.env
`)

	// Change CWD away from the project — paths must still resolve via stack dir.
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig) //nolint:errcheck

	s, err := Load(stackPath)
	if err != nil {
		t.Fatalf("Load from other CWD: %v", err)
	}
	if s.Containers[0].Env["FROM_STACK_DIR"] != "yes" {
		t.Errorf("Env=%v", s.Containers[0].Env)
	}
}

func TestEnvFileRelativeDataPathFromOtherCWD(t *testing.T) {
	proj := t.TempDir()
	dataDir := filepath.Join(proj, "data", "bitwarden")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "secrets.env"), []byte("ADMIN_TOKEN=secret_from_file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stackPath := writeStack(t, proj, `project: test
data_path: ./data
containers:
  - name: bitwarden
    image: vaultwarden/server:latest
    env_file:
      - bitwarden/secrets.env
`)

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig) //nolint:errcheck

	s, err := Load(stackPath)
	if err != nil {
		t.Fatalf("Load with relative data_path from other CWD: %v", err)
	}
	c := s.ContainerByName("bitwarden")
	if c == nil {
		t.Fatal("container not found")
	}
	if c.Env["ADMIN_TOKEN"] != "secret_from_file" {
		t.Errorf("ADMIN_TOKEN=%q DataPath=%s", c.Env["ADMIN_TOKEN"], s.DataPath)
	}
	// data_path should be absolute under the stack file's directory
	wantPrefix := proj
	if abs, err := filepath.Abs(proj); err == nil {
		wantPrefix = abs
	}
	if s.DataPath != filepath.Join(wantPrefix, "data") {
		t.Errorf("DataPath=%q want %q", s.DataPath, filepath.Join(wantPrefix, "data"))
	}
}

func TestParseEnvFileFeatures(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "mixed.env")
	content := `# comment
FOO=bar
export BAR=baz
QUOTED="hello world"
SINGLE='keep $raw'
EMPTY=
HOST_ONLY
WITH_EQ=a=b=c
`
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOST_ONLY", "from_host")

	got, err := parseEnvFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"FOO":      "bar",
		"BAR":      "baz",
		"QUOTED":   "hello world",
		"SINGLE":   "keep $raw",
		"EMPTY":    "",
		"HOST_ONLY": "from_host",
		"WITH_EQ":  "a=b=c",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %s: got %q want %q", k, got[k], v)
		}
	}
}

func TestParseEnvFileKeyWithoutHostValueIsOmitted(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "onlykey.env")
	if err := os.WriteFile(envPath, []byte("MISSING_ON_HOST\nSET=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Ensure the key is not in the environment
	t.Setenv("MISSING_ON_HOST", "")
	os.Unsetenv("MISSING_ON_HOST")

	got, err := parseEnvFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["MISSING_ON_HOST"]; ok {
		t.Errorf("MISSING_ON_HOST should be omitted when unset on host, got %v", got)
	}
	if got["SET"] != "1" {
		t.Errorf("SET=%q", got["SET"])
	}
}

func TestEnvFileMultipleFilesLaterWins(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.env"), []byte("K=from_a\nA=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.env"), []byte("K=from_b\nB=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writeStack(t, dir, `project: test
containers:
  - name: app
    image: alpine:latest
    env_file:
      - a.env
      - b.env
`)
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	env := s.Containers[0].Env
	if env["K"] != "from_b" {
		t.Errorf("K=%q want from_b", env["K"])
	}
	if env["A"] != "1" || env["B"] != "2" {
		t.Errorf("Env=%v", env)
	}
}

func TestEnvFileMissingReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := writeStack(t, dir, `project: test
containers:
  - name: app
    image: alpine:latest
    env_file: does-not-exist.env
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing env_file")
	}
}
