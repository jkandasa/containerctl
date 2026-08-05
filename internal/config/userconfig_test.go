package config

import (
	"os"
	"path/filepath"
	"testing"
)

func withTempConfigHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

func TestSetAndCurrentStackPath(t *testing.T) {
	withTempConfigHome(t)

	stack := filepath.Join(t.TempDir(), "my-stack.yaml")
	if err := os.WriteFile(stack, []byte("project: t\ncontainers: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := CurrentStackPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("CurrentStackPath() = %q, want empty before set", got)
	}

	abs, err := SetCurrentStackPath(stack)
	if err != nil {
		t.Fatal(err)
	}
	if abs != stack {
		// both should be absolute
		want, _ := filepath.Abs(stack)
		if abs != want {
			t.Fatalf("SetCurrentStackPath() = %q, want %q", abs, want)
		}
	}

	got, err = CurrentStackPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != abs {
		t.Fatalf("CurrentStackPath() = %q, want %q", got, abs)
	}

	// relative path is stored absolute
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Dir(stack))
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	abs2, err := SetCurrentStackPath(filepath.Base(stack))
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(abs2) {
		t.Fatalf("expected absolute path, got %q", abs2)
	}
}

func TestSetCurrentStackPathMissing(t *testing.T) {
	withTempConfigHome(t)
	_, err := SetCurrentStackPath(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestClearCurrentStackPath(t *testing.T) {
	withTempConfigHome(t)
	stack := filepath.Join(t.TempDir(), "s.yaml")
	if err := os.WriteFile(stack, []byte("project: t\ncontainers: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SetCurrentStackPath(stack); err != nil {
		t.Fatal(err)
	}
	if err := ClearCurrentStackPath(); err != nil {
		t.Fatal(err)
	}
	got, err := CurrentStackPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("after clear, CurrentStackPath() = %q, want empty", got)
	}
}

func TestResolveStackFile(t *testing.T) {
	withTempConfigHome(t)
	stack := filepath.Join(t.TempDir(), "used.yaml")
	if err := os.WriteFile(stack, []byte("project: t\ncontainers: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SetCurrentStackPath(stack); err != nil {
		t.Fatal(err)
	}

	// Explicit -f wins.
	got, err := ResolveStackFile("other.yaml", true)
	if err != nil {
		t.Fatal(err)
	}
	if got != "other.yaml" {
		t.Fatalf("explicit file: got %q, want other.yaml", got)
	}

	// Saved stack path when flag not changed.
	got, err = ResolveStackFile("", false)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Abs(stack)
	if got != want {
		t.Fatalf("stack path: got %q, want %q", got, want)
	}

	// Default when nothing saved.
	if err := ClearCurrentStackPath(); err != nil {
		t.Fatal(err)
	}
	got, err = ResolveStackFile("", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != DefaultStackFile {
		t.Fatalf("default: got %q, want %q", got, DefaultStackFile)
	}
}
