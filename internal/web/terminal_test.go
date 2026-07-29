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
