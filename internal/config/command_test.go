package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSplitCommand(t *testing.T) {
	tests := []struct {
		in      string
		want    []string
		wantErr bool
	}{
		{"serve --port 8080", []string{"serve", "--port", "8080"}, false},
		{"  serve   --port  8080  ", []string{"serve", "--port", "8080"}, false},
		{`serve --msg "hello world"`, []string{"serve", "--msg", "hello world"}, false},
		{`serve --msg 'hello world'`, []string{"serve", "--msg", "hello world"}, false},
		{`sh -c "echo hi"`, []string{"sh", "-c", "echo hi"}, false},
		{`path\ with\ spaces`, []string{"path with spaces"}, false},
		{"", nil, false},
		{`"unclosed`, nil, true},
		{`trailing\`, nil, true},
	}
	for _, tc := range tests {
		got, err := splitCommand(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("splitCommand(%q) err = nil, want error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("splitCommand(%q) err = %v", tc.in, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitCommand(%q) = %#v, want %#v", tc.in, got, tc.want)
		}
	}
}

func TestCommandListUnmarshalYAML(t *testing.T) {
	type wrap struct {
		Command    CommandList `yaml:"command,omitempty"`
		Entrypoint CommandList `yaml:"entrypoint,omitempty"`
	}

	tests := []struct {
		name string
		yaml string
		cmd  []string
		ep   []string
	}{
		{
			name: "string command",
			yaml: "command: serve --port 8080 --log-level info\n",
			cmd:  []string{"serve", "--port", "8080", "--log-level", "info"},
		},
		{
			name: "list command",
			yaml: "command: [\"serve\", \"--port\", \"8080\"]\n",
			cmd:  []string{"serve", "--port", "8080"},
		},
		{
			name: "block list command",
			yaml: "command:\n  - serve\n  - --port\n  - \"8080\"\n",
			cmd:  []string{"serve", "--port", "8080"},
		},
		{
			name: "string entrypoint",
			yaml: "entrypoint: /app/start.sh\n",
			ep:   []string{"/app/start.sh"},
		},
		{
			name: "quoted args in string",
			yaml: "command: myapp --name \"foo bar\"\n",
			cmd:  []string{"myapp", "--name", "foo bar"},
		},
		{
			name: "both string forms",
			yaml: "entrypoint: /entry.sh\ncommand: run --verbose\n",
			ep:   []string{"/entry.sh"},
			cmd:  []string{"run", "--verbose"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var w wrap
			if err := yaml.Unmarshal([]byte(tc.yaml), &w); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual([]string(w.Command), tc.cmd) {
				t.Errorf("Command = %#v, want %#v", []string(w.Command), tc.cmd)
			}
			if !reflect.DeepEqual([]string(w.Entrypoint), tc.ep) {
				t.Errorf("Entrypoint = %#v, want %#v", []string(w.Entrypoint), tc.ep)
			}
		})
	}
}

func TestLoadCommandStringForm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stack.yaml")
	content := `project: demo
containers:
  - name: app
    image: app:1
    command: serve --port 8080
    entrypoint: /app/start.sh --init
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	stack, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c := stack.Containers[0]
	wantCmd := []string{"serve", "--port", "8080"}
	wantEp := []string{"/app/start.sh", "--init"}
	if !reflect.DeepEqual([]string(c.Command), wantCmd) {
		t.Errorf("Command = %#v, want %#v", []string(c.Command), wantCmd)
	}
	if !reflect.DeepEqual([]string(c.Entrypoint), wantEp) {
		t.Errorf("Entrypoint = %#v, want %#v", []string(c.Entrypoint), wantEp)
	}
}
