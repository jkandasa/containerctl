package reconcile

import (
	"reflect"
	"testing"

	"github.com/jkandasa/containerctl/internal/config"
	rt "github.com/jkandasa/containerctl/internal/runtime"
)

func TestParsePort(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    rt.PortBinding
		wantErr bool
	}{
		{
			name:  "container port only",
			input: "8080",
			want:  rt.PortBinding{ContainerPort: "8080", Protocol: "tcp"},
		},
		{
			name:  "host and container",
			input: "8080:80",
			want:  rt.PortBinding{HostPort: "8080", ContainerPort: "80", Protocol: "tcp"},
		},
		{
			name:  "ip host container",
			input: "127.0.0.1:8080:80",
			want:  rt.PortBinding{HostIP: "127.0.0.1", HostPort: "8080", ContainerPort: "80", Protocol: "tcp"},
		},
		{
			name:  "udp protocol",
			input: "53:53/udp",
			want:  rt.PortBinding{HostPort: "53", ContainerPort: "53", Protocol: "udp"},
		},
		{
			name:  "ip with udp",
			input: "0.0.0.0:5353:53/udp",
			want:  rt.PortBinding{HostIP: "0.0.0.0", HostPort: "5353", ContainerPort: "53", Protocol: "udp"},
		},
		{
			name:    "too many colons",
			input:   "1:2:3:4",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePort(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("parsePort(%q) expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("parsePort(%q) unexpected error: %v", tc.input, err)
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parsePort(%q) = %+v, want %+v", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseMount(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    rt.Mount
		wantErr bool
	}{
		{
			name:  "named volume only (target)",
			input: "data",
			want:  rt.Mount{Type: "volume", Target: "data"},
		},
		{
			name:  "bind mount absolute",
			input: "/host/path:/container/path",
			want:  rt.Mount{Type: "bind", Source: "/host/path", Target: "/container/path"},
		},
		{
			name:  "named volume with target",
			input: "pgdata:/var/lib/postgresql/data",
			want:  rt.Mount{Type: "volume", Source: "pgdata", Target: "/var/lib/postgresql/data"},
		},
		{
			name:  "bind with read only",
			input: "/config:/app/config:ro",
			want:  rt.Mount{Type: "bind", Source: "/config", Target: "/app/config", ReadOnly: true},
		},
		{
			name:  "relative bind",
			input: "./data:/app/data",
			want:  rt.Mount{Type: "bind", Source: "./data", Target: "/app/data"},
		},
		{
			name:  "readonly full word",
			input: "/secrets:/run/secrets:readonly",
			want:  rt.Mount{Type: "bind", Source: "/secrets", Target: "/run/secrets", ReadOnly: true},
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			input:   "   ",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMount(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestMountType(t *testing.T) {
	tests := []struct {
		src  string
		want string
	}{
		{"/absolute", "bind"},
		{"./relative", "bind"},
		{"../parent", "bind"},
		{"myvolume", "volume"},
		{"data_subdir", "volume"},
	}

	for _, tc := range tests {
		if got := mountType(tc.src); got != tc.want {
			t.Errorf("mountType(%q) = %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestParseMemory(t *testing.T) {
	tests := []struct {
		input   string
		want    int64
		wantErr bool
	}{
		{"", 0, false},
		{"512", 512, false},
		{"1k", 1024, false},
		{"2m", 2 * 1024 * 1024, false},
		{"1.5g", int64(1.5 * 1024 * 1024 * 1024), false},
		{"1t", 1024 * 1024 * 1024 * 1024, false},
		{"invalid", 0, true},
		{"10x", 0, true},
	}

	for _, tc := range tests {
		got, err := parseMemory(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseMemory(%q) expected error", tc.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseMemory(%q) unexpected error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseMemory(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestParseResources(t *testing.T) {
	r := config.Resources{
		CPUs:      "2.5",
		Memory:    "512m",
		PidsLimit: 100,
	}
	got, err := parseResources(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.NanoCPUs != 2500000000 {
		t.Errorf("NanoCPUs = %d", got.NanoCPUs)
	}
	if got.MemoryBytes != 512*1024*1024 {
		t.Errorf("MemoryBytes = %d", got.MemoryBytes)
	}
	if got.PidsLimit != 100 {
		t.Errorf("PidsLimit = %d", got.PidsLimit)
	}
}
