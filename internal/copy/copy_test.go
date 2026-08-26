package copy

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	rt "github.com/jkandasa/containerctl/internal/runtime"
)

type fakeFS struct {
	stat    map[string]rt.PathStat
	statErr map[string]error
	files   map[string][]byte // path → file content packed as a single-entry tar
	putID   string
	putDest string
	putData []byte
	putOpts rt.CopyToOptions

	// copyFromOpen is true between CopyFromContainer and Close of its reader.
	copyFromOpen          bool
	failIfCopyToWhileOpen bool
	copyToWhileOpen       bool
}

type closeNotify struct {
	io.Reader
	onClose func()
}

func (c *closeNotify) Close() error {
	if c.onClose != nil {
		c.onClose()
		c.onClose = nil
	}
	return nil
}

func (f *fakeFS) StatPath(_ context.Context, _, path string) (*rt.PathStat, error) {
	if err, ok := f.statErr[path]; ok {
		return nil, err
	}
	st, ok := f.stat[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &st, nil
}

func (f *fakeFS) CopyFromContainer(_ context.Context, _, srcPath string) (io.ReadCloser, rt.PathStat, error) {
	data, ok := f.files[srcPath]
	if !ok {
		return nil, rt.PathStat{}, os.ErrNotExist
	}
	st := f.stat[srcPath]
	if st.Name == "" {
		st.Name = filepath.Base(srcPath)
		st.Size = int64(len(data))
		st.Mode = 0o644
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{Name: filepath.Base(srcPath), Mode: 0o644, Size: int64(len(data))}
	if err := tw.WriteHeader(hdr); err != nil {
		return nil, rt.PathStat{}, err
	}
	if _, err := tw.Write(data); err != nil {
		return nil, rt.PathStat{}, err
	}
	if err := tw.Close(); err != nil {
		return nil, rt.PathStat{}, err
	}
	f.copyFromOpen = true
	return &closeNotify{
		Reader: bytes.NewReader(buf.Bytes()),
		onClose: func() {
			f.copyFromOpen = false
		},
	}, st, nil
}

func (f *fakeFS) CopyToContainer(_ context.Context, id, destPath string, content io.Reader, opts rt.CopyToOptions) error {
	if f.copyFromOpen {
		f.copyToWhileOpen = true
		if f.failIfCopyToWhileOpen {
			return fmt.Errorf("copy-to while copy-from still open")
		}
	}
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	f.putID = id
	f.putDest = destPath
	f.putData = data
	f.putOpts = opts
	return nil
}

func TestFromContainerToHostFile(t *testing.T) {
	fs := &fakeFS{
		files: map[string][]byte{"/etc/hostname": []byte("box\n")},
		stat: map[string]rt.PathStat{
			"/etc/hostname": {Name: "hostname", Size: 4, Mode: 0o644},
		},
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "hostname")
	n, err := FromContainer(context.Background(), fs, "id", "/etc/hostname", dest, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("size %d", n)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "box\n" {
		t.Fatalf("got %q", got)
	}
}

func TestFromContainerStdout(t *testing.T) {
	fs := &fakeFS{
		files: map[string][]byte{"/etc/hostname": []byte("box\n")},
		stat:  map[string]rt.PathStat{"/etc/hostname": {Name: "hostname", Size: 4, Mode: 0o644}},
	}
	var out bytes.Buffer
	if _, err := FromContainer(context.Background(), fs, "id", "/etc/hostname", "-", Options{}, &out); err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(&out)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Name != "hostname" {
		t.Fatalf("tar name %q", hdr.Name)
	}
	body, _ := io.ReadAll(tr)
	if string(body) != "box\n" {
		t.Fatalf("body %q", body)
	}
}

func TestFromContainerFollowLink(t *testing.T) {
	fs := &fakeFS{
		files: map[string][]byte{"/real/file": []byte("data")},
		stat: map[string]rt.PathStat{
			"/link":      {Name: "link", Mode: os.ModeSymlink | 0o777, LinkTarget: "/real/file"},
			"/real/file": {Name: "file", Size: 4, Mode: 0o644},
		},
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "out")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := FromContainer(context.Background(), fs, "id", "/link", dest, Options{FollowLink: true}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "link"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "data" {
		t.Fatalf("got %q", got)
	}
}

func TestToContainerPacksFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(src, []byte("k: v\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := &fakeFS{
		stat: map[string]rt.PathStat{
			"/etc/app": {Name: "app", Mode: os.ModeDir | 0o755},
		},
	}
	n, err := ToContainer(context.Background(), fs, "id", src, "/etc/app", Options{Archive: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len("k: v\n")) {
		t.Fatalf("size %d", n)
	}
	if fs.putDest != "/etc/app" {
		t.Fatalf("dest %q", fs.putDest)
	}
	if !fs.putOpts.CopyUIDGID {
		t.Fatal("expected CopyUIDGID from --archive")
	}
	tr := tar.NewReader(bytes.NewReader(fs.putData))
	hdr, err := tr.Next()
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Name != "config.yaml" {
		t.Fatalf("tar name %q", hdr.Name)
	}
}

func TestToContainerRenameFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "local.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := &fakeFS{stat: map[string]rt.PathStat{}}
	if _, err := ToContainer(context.Background(), fs, "id", src, "/etc/renamed.txt", Options{Overwrite: true}, nil); err != nil {
		t.Fatal(err)
	}
	if fs.putDest != "/etc" {
		t.Fatalf("dest dir %q", fs.putDest)
	}
	if !fs.putOpts.AllowOverwriteDirWithFile {
		t.Fatal("expected overwrite flag")
	}
	tr := tar.NewReader(bytes.NewReader(fs.putData))
	hdr, err := tr.Next()
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Name != "renamed.txt" {
		t.Fatalf("tar name %q want renamed.txt", hdr.Name)
	}
}

func TestToContainerStdinRequiresDir(t *testing.T) {
	fs := &fakeFS{
		stat: map[string]rt.PathStat{"/tmp/file": {Name: "file", Mode: 0o644}},
	}
	_, err := ToContainer(context.Background(), fs, "id", "-", "/tmp/file", Options{}, bytes.NewReader(nil))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBetweenContainersFileToDir(t *testing.T) {
	fs := &fakeFS{
		files: map[string][]byte{"/etc/hostname": []byte("box\n")},
		stat: map[string]rt.PathStat{
			"/etc/hostname": {Name: "hostname", Size: 4, Mode: 0o644},
			"/backup":       {Name: "backup", Mode: os.ModeDir | 0o755},
		},
	}
	n, err := BetweenContainers(context.Background(), fs, "src", "/etc/hostname", "dst", "/backup", Options{Archive: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("size %d", n)
	}
	if fs.putID != "dst" {
		t.Fatalf("put id %q", fs.putID)
	}
	if fs.putDest != "/backup" {
		t.Fatalf("dest %q", fs.putDest)
	}
	if !fs.putOpts.CopyUIDGID {
		t.Fatal("expected --archive to set CopyUIDGID")
	}
	tr := tar.NewReader(bytes.NewReader(fs.putData))
	hdr, err := tr.Next()
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Name != "hostname" {
		t.Fatalf("tar name %q", hdr.Name)
	}
	body, _ := io.ReadAll(tr)
	if string(body) != "box\n" {
		t.Fatalf("body %q", body)
	}
}

func TestBetweenContainersRename(t *testing.T) {
	fs := &fakeFS{
		files: map[string][]byte{"/var/log/app.log": []byte("line\n")},
		stat: map[string]rt.PathStat{
			"/var/log/app.log": {Name: "app.log", Size: 5, Mode: 0o644},
		},
	}
	if _, err := BetweenContainers(context.Background(), fs, "src", "/var/log/app.log", "dst", "/tmp/renamed.log", Options{Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	if fs.putDest != "/tmp" {
		t.Fatalf("dest dir %q", fs.putDest)
	}
	if !fs.putOpts.AllowOverwriteDirWithFile {
		t.Fatal("expected overwrite flag")
	}
	tr := tar.NewReader(bytes.NewReader(fs.putData))
	hdr, err := tr.Next()
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Name != "renamed.log" {
		t.Fatalf("tar name %q", hdr.Name)
	}
}

func TestBetweenContainersFollowLink(t *testing.T) {
	fs := &fakeFS{
		files: map[string][]byte{"/real/file": []byte("data")},
		stat: map[string]rt.PathStat{
			"/link":      {Name: "link", Mode: os.ModeSymlink | 0o777, LinkTarget: "/real/file"},
			"/real/file": {Name: "file", Size: 4, Mode: 0o644},
			"/out":       {Name: "out", Mode: os.ModeDir | 0o755},
		},
	}
	if _, err := BetweenContainers(context.Background(), fs, "src", "/link", "dst", "/out", Options{FollowLink: true}); err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(bytes.NewReader(fs.putData))
	hdr, err := tr.Next()
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Name != "link" {
		t.Fatalf("tar name %q want link", hdr.Name)
	}
}

func TestBetweenContainersSamePath(t *testing.T) {
	fs := &fakeFS{}
	_, err := BetweenContainers(context.Background(), fs, "id", "/etc/x", "id", "/etc/x", Options{})
	if err == nil {
		t.Fatal("expected error for same path")
	}
}

func TestBetweenContainersSameContainerClosesSourceFirst(t *testing.T) {
	fs := &fakeFS{
		failIfCopyToWhileOpen: true,
		files:                 map[string][]byte{"/etc/hostname": []byte("box\n")},
		stat: map[string]rt.PathStat{
			"/etc/hostname": {Name: "hostname", Size: 4, Mode: 0o644},
			"/backup":       {Name: "backup", Mode: os.ModeDir | 0o755},
		},
	}
	if _, err := BetweenContainers(context.Background(), fs, "same", "/etc/hostname", "same", "/backup", Options{}); err != nil {
		t.Fatal(err)
	}
	if fs.copyToWhileOpen {
		t.Fatal("copy-to started while copy-from was still open")
	}
	if fs.putID != "same" || fs.putDest != "/backup" {
		t.Fatalf("put %s %s", fs.putID, fs.putDest)
	}
}

func TestBetweenContainersDistinctStillStreams(t *testing.T) {
	fs := &fakeFS{
		files: map[string][]byte{"/etc/hostname": []byte("box\n")},
		stat: map[string]rt.PathStat{
			"/etc/hostname": {Name: "hostname", Size: 4, Mode: 0o644},
			"/backup":       {Name: "backup", Mode: os.ModeDir | 0o755},
		},
	}
	if _, err := BetweenContainers(context.Background(), fs, "src", "/etc/hostname", "dst", "/backup", Options{}); err != nil {
		t.Fatal(err)
	}
	// Distinct containers may overlap; we only require the copy succeeded.
	if fs.putID != "dst" {
		t.Fatalf("put id %q", fs.putID)
	}
}
