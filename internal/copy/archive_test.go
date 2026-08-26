package copy

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestPackAndExtractFileToFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(src, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := sourceInfo(src, false)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := packLocal(info)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()

	dst := filepath.Join(dir, "out.txt")
	n, err := extractToHost(rc, info, dst)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len("hello world")) {
		t.Fatalf("extracted %d bytes, want %d", n, len("hello world"))
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestPackAndExtractFileIntoDir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	destDir := filepath.Join(dir, "dest")
	if err := os.Mkdir(destDir, 0o755); err != nil {
		t.Fatal(err)
	}

	info, err := sourceInfo(src, false)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := packLocal(info)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()

	if _, err := extractToHost(rc, info, destDir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destDir, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "payload" {
		t.Fatalf("got %q", got)
	}
}

func TestPackAndExtractDirectory(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "tree")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := sourceInfo(src, false)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := packLocal(info)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()

	dest := filepath.Join(dir, "out")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := extractToHost(rc, info, dest); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dest, "tree", "a.txt")); err != nil || string(got) != "A" {
		t.Fatalf("a.txt: %q %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(dest, "tree", "sub", "b.txt")); err != nil || string(got) != "B" {
		t.Fatalf("b.txt: %q %v", got, err)
	}
}

func TestCopyContentsTrailingDot(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "tree")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "only.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := sourceInfo(src+string(filepath.Separator)+".", false)
	if err != nil {
		t.Fatal(err)
	}
	if !info.CopyContents {
		t.Fatal("expected CopyContents")
	}
	rc, err := packLocal(info)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()

	dest := filepath.Join(dir, "out")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := extractToHost(rc, info, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "tree")); !os.IsNotExist(err) {
		t.Fatal("expected contents only, not the tree directory")
	}
	got, err := os.ReadFile(filepath.Join(dest, "only.txt"))
	if err != nil || string(got) != "x" {
		t.Fatalf("only.txt: %q %v", got, err)
	}
}

func TestFollowLinkPacksTarget(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(real, []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "alias")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	info, err := sourceInfo(link, true)
	if err != nil {
		t.Fatal(err)
	}
	if info.RebaseName != "alias" {
		t.Fatalf("RebaseName=%q want alias", info.RebaseName)
	}
	rc, err := packLocal(info)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()

	// Without rebase the tar entry is named after the resolved file.
	// extractToHost + RebaseName should restore the original name when dest is a dir.
	if info.RebaseName != "" {
		rc = rebaseReader(rc, filepath.Base(info.Path), info.RebaseName)
		info.Path = filepath.Join(filepath.Dir(info.Path), info.RebaseName)
		info.RebaseName = ""
	}

	dest := filepath.Join(dir, "out")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := extractToHost(rc, info, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "alias"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "target" {
		t.Fatalf("got %q", got)
	}
}

func TestSanitizeTarNameRejectsTraversal(t *testing.T) {
	if _, err := sanitizeTarName("../etc/passwd"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := sanitizeTarName("foo/../../etc/passwd"); err == nil {
		t.Fatal("expected error")
	}
	got, err := sanitizeTarName("./ok/file.txt")
	if err != nil || got != "ok/file.txt" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestUntarRejectsTraversal(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{Name: "../evil", Mode: 0o644, Size: 4}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("boom")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if _, err := untar(bytes.NewReader(buf.Bytes()), dest); err == nil {
		t.Fatal("expected traversal error")
	}
}

func TestCannotCopyDirOntoFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "tree")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "file")
	if err := os.WriteFile(dst, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := sourceInfo(src, false)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := packLocal(info)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()

	if _, err := extractToHost(rc, info, dst); err == nil {
		t.Fatal("expected error copying dir onto file")
	}
}

func TestRewriteBase(t *testing.T) {
	if got := rewriteBase("data/a.txt", "data", "appdata"); got != "appdata/a.txt" {
		t.Fatalf("got %q", got)
	}
	if got := rewriteBase("data", "data", "appdata"); got != "appdata" {
		t.Fatalf("got %q", got)
	}
	if got := rewriteBase("other", "data", "appdata"); got != "other" {
		t.Fatalf("got %q", got)
	}
}

func TestRebaseReader(t *testing.T) {
	var in bytes.Buffer
	tw := tar.NewWriter(&in)
	hdr := &tar.Header{Name: "old/file.txt", Mode: 0o644, Size: 3}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	rc := rebaseReader(&in, "old", "new")
	defer rc.Close()
	tr := tar.NewReader(rc)
	got, err := tr.Next()
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "new/file.txt" {
		t.Fatalf("name %q", got.Name)
	}
	body, err := io.ReadAll(tr)
	if err != nil || string(body) != "abc" {
		t.Fatalf("body %q %v", body, err)
	}
}
