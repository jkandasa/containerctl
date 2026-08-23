package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFilesRequiresDataPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	content := `project: demo
files:
  - name: conf
    content: |
      hello
containers:
  - name: app
    image: app:1
    files:
      - source: conf
        target: /etc/conf
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error when data_path is empty")
	}
	if !strings.Contains(err.Error(), "data_path is required") {
		t.Fatalf("error %q should mention data_path", err.Error())
	}
}

func TestLoadFilesResolvesAndMaterializes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	content := `project: demo
data_path: ./data
files:
  - name: app-conf
    content: |
      log_level: info
containers:
  - name: app
    image: app:1
    files:
      - source: app-conf
        target: /etc/app/config.yaml
        mode: "0640"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	stack, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if stack.DataPath != filepath.Join(dir, "data") {
		t.Fatalf("DataPath=%q want %q", stack.DataPath, filepath.Join(dir, "data"))
	}
	c := stack.Containers[0]
	if len(c.Files) != 1 {
		t.Fatalf("files len=%d", len(c.Files))
	}
	fm := c.Files[0]
	wantHost := filepath.Join(dir, "data", "files", "app-conf")
	if fm.HostPath != wantHost {
		t.Errorf("HostPath=%q want %q", fm.HostPath, wantHost)
	}
	if fm.Content != "log_level: info\n" {
		t.Errorf("Content=%q", fm.Content)
	}
	if !fm.RO {
		t.Error("RO default should be true")
	}
	if fm.Mode != "0640" {
		t.Errorf("Mode=%q", fm.Mode)
	}

	if err := MaterializeFiles(&c); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(wantHost)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "log_level: info\n" {
		t.Errorf("host file content=%q", got)
	}
	st, err := os.Stat(wantHost)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o640 {
		t.Errorf("mode=%o want 0640", st.Mode().Perm())
	}
}

func TestLoadFilesReadOnlyFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	content := `project: demo
data_path: data
files:
  - name: state
    content: "{}"
containers:
  - name: app
    image: app:1
    files:
      - source: state
        target: /data/state.json
        read_only: false
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	stack, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if stack.Containers[0].Files[0].RO {
		t.Error("read_only: false should set RO=false")
	}
}

func TestLoadFilesUnknownSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	content := `project: demo
data_path: data
containers:
  - name: app
    image: app:1
    files:
      - source: missing
        target: /etc/x
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for unknown source")
	}
}

func TestLoadFilesRelativeTargetRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	content := `project: demo
data_path: data
files:
  - name: conf
    content: x
containers:
  - name: app
    image: app:1
    files:
      - source: conf
        target: relative/path
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for relative target")
	}
}

func TestHashIncludesFileContent(t *testing.T) {
	c1 := &Container{
		Image: "app:1",
		Files: []FileMount{{
			Source: "conf", Target: "/etc/c", Mode: "0644", RO: true, Content: "a",
		}},
	}
	c2 := &Container{
		Image: "app:1",
		Files: []FileMount{{
			Source: "conf", Target: "/etc/c", Mode: "0644", RO: true, Content: "b",
		}},
	}
	if Hash(c1) == Hash(c2) {
		t.Error("different file content must change hash")
	}
	c3 := &Container{
		Image: "app:1",
		Files: []FileMount{{
			Source: "conf", Target: "/etc/c", Mode: "0644", RO: true, Content: "a",
		}},
	}
	if Hash(c1) != Hash(c3) {
		t.Error("identical files must same hash")
	}
}

func TestMaterializeOverwrites(t *testing.T) {
	dir := t.TempDir()
	host := filepath.Join(dir, "files", "conf")
	if err := os.MkdirAll(filepath.Dir(host), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(host, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Container{
		Name: "app",
		Files: []FileMount{{
			Source: "conf", HostPath: host, Content: "new", Mode: "0644",
		}},
	}
	if err := MaterializeFiles(c); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(host)
	if string(got) != "new" {
		t.Errorf("got %q want new", got)
	}
}

func TestParseFileMode(t *testing.T) {
	tests := []struct {
		in   string
		want os.FileMode
	}{
		{"", 0o644},
		{"644", 0o644},
		{"0644", 0o644},
		{"0o600", 0o600},
		{"0400", 0o400},
	}
	for _, tc := range tests {
		got, err := parseFileMode(tc.in)
		if err != nil {
			t.Errorf("parseFileMode(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseFileMode(%q)=%o want %o", tc.in, got, tc.want)
		}
	}
	if _, err := parseFileMode("xyz"); err == nil {
		t.Error("expected error for invalid mode")
	}
}

func TestUsesFiles(t *testing.T) {
	s := &Stack{Containers: []Container{{Name: "a", Image: "i"}}}
	if s.UsesFiles() {
		t.Error("empty should be false")
	}
	s.Files = []FileDef{{Name: "x"}}
	if !s.UsesFiles() {
		t.Error("top-level files should be true")
	}
	s2 := &Stack{Containers: []Container{{Name: "a", Image: "i", Files: []FileMount{{Source: "x"}}}}}
	if !s2.UsesFiles() {
		t.Error("container files should be true")
	}
}
