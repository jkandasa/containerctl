package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTruncateFileEmptiesInPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ctr-json.log")
	if err := os.WriteFile(path, []byte("old line\nanother\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := truncateFile(path); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("log file still has %d bytes: %q", len(got), got)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(info, after) {
		t.Fatal("truncate replaced the file instead of emptying the same inode")
	}
}

func TestTruncateFileMissing(t *testing.T) {
	err := truncateFile(filepath.Join(t.TempDir(), "missing.log"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
