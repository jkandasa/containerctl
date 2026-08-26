package web

import "testing"

func TestGenerateOutPath(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantPath string
		wantOK   bool
	}{
		{
			name:   "no flag",
			args:   []string{"generate", "--project", "home"},
			wantOK: false,
		},
		{
			name:     "separate value",
			args:     []string{"generate", "-O", "/srv/stack.yaml"},
			wantPath: "/srv/stack.yaml",
			wantOK:   true,
		},
		{
			name:     "attached value",
			args:     []string{"generate", "-O/srv/stack.yaml"},
			wantPath: "/srv/stack.yaml",
			wantOK:   true,
		},
		{
			name:     "long form",
			args:     []string{"generate", "--out", "/srv/stack.yaml"},
			wantPath: "/srv/stack.yaml",
			wantOK:   true,
		},
		{
			name:     "long form with equals",
			args:     []string{"generate", "--out=/srv/stack.yaml"},
			wantPath: "/srv/stack.yaml",
			wantOK:   true,
		},
		{
			// Reported as given so the caller rejects it rather than letting
			// the flag through unchecked.
			name:   "flag without value",
			args:   []string{"generate", "-O"},
			wantOK: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path, ok := generateOutPath(tc.args)
			if ok != tc.wantOK || path != tc.wantPath {
				t.Errorf("generateOutPath(%v) = %q, %v; want %q, %v", tc.args, path, ok, tc.wantPath, tc.wantOK)
			}
		})
	}
}

func TestCpContainerNames(t *testing.T) {
	tests := []struct {
		parts []string
		want  []string
	}{
		{[]string{"cp", "postgres:/etc/hostname", "-"}, []string{"postgres"}},
		{[]string{"cp", "-", "app:/restore"}, []string{"app"}},
		{[]string{"cp", "-a", "app:/etc/x", "-"}, []string{"app"}},
		{[]string{"cp", "./file", "app:/etc/file"}, []string{"app"}},
		{[]string{"cp", "postgres:/dump.sql", "app:/backup/dump.sql"}, []string{"postgres", "app"}},
		{[]string{"cp", "app:/a", "app:/b"}, []string{"app"}},
		{[]string{"cp"}, nil},
	}
	for _, tc := range tests {
		got := cpContainerNames(tc.parts)
		if len(got) != len(tc.want) {
			t.Errorf("cpContainerNames(%v) = %v, want %v", tc.parts, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("cpContainerNames(%v) = %v, want %v", tc.parts, got, tc.want)
				break
			}
		}
	}
}

func TestCpTouchesHostPath(t *testing.T) {
	if !cpTouchesHostPath([]string{"cp", "app:/etc/x", "./out"}) {
		t.Error("expected host path")
	}
	if !cpTouchesHostPath([]string{"cp", "./in", "app:/etc/x"}) {
		t.Error("expected host path")
	}
	if cpTouchesHostPath([]string{"cp", "app:/etc/x", "-"}) {
		t.Error("stdout is not a host path")
	}
	if cpTouchesHostPath([]string{"cp", "-", "app:/restore"}) {
		t.Error("stdin is not a host path")
	}
	if cpTouchesHostPath([]string{"cp", "-a", "-q", "app:/etc/x", "-"}) {
		t.Error("flags plus stdout should not count as a host path")
	}
	if cpTouchesHostPath([]string{"cp", "postgres:/dump.sql", "app:/backup/dump.sql"}) {
		t.Error("container-to-container is not a host path")
	}
}

func TestIsStackFilePath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/srv/stack.yaml", true},
		{"/srv/stack.yml", true},
		{"/srv/STACK.YAML", true},
		{"/home/user/.bashrc", false},
		{"/root/.ssh/authorized_keys", false},
		{"/etc/passwd", false},
		{"/srv/stack.yaml.bak", false},
		{"/srv/stack", false},
	}
	for _, tc := range tests {
		if got := isStackFilePath(tc.path); got != tc.want {
			t.Errorf("isStackFilePath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
