package cmd

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jkandasa/containerctl/internal/config"
	rt "github.com/jkandasa/containerctl/internal/runtime"
)

func TestFormatPortBinding(t *testing.T) {
	tests := []struct {
		name  string
		input rt.PortBinding
		want  string
	}{
		{
			name:  "host and container",
			input: rt.PortBinding{HostPort: "8080", ContainerPort: "80", Protocol: "tcp"},
			want:  "8080:80",
		},
		{
			name:  "with host ip",
			input: rt.PortBinding{HostIP: "127.0.0.1", HostPort: "8080", ContainerPort: "80", Protocol: "tcp"},
			want:  "127.0.0.1:8080:80",
		},
		{
			name:  "udp keeps protocol",
			input: rt.PortBinding{HostPort: "5353", ContainerPort: "53", Protocol: "udp"},
			want:  "5353:53/udp",
		},
		{
			name:  "exposed only",
			input: rt.PortBinding{ContainerPort: "9000", Protocol: "tcp"},
			want:  "9000",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatPortBinding(tc.input); got != tc.want {
				t.Errorf("formatPortBinding(%+v) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestFormatMemoryBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{2 * 1024 * 1024 * 1024, "2g"},
		{32 * 1024 * 1024, "32m"},
		{512 * 1024, "512k"},
		{1000, "1000b"},
	}
	for _, tc := range tests {
		if got := formatMemoryBytes(tc.input); got != tc.want {
			t.Errorf("formatMemoryBytes(%d) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestIsAnonymousVolume(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{strings.Repeat("a1", 32), true},
		{strings.Repeat("A1", 32), false}, // uppercase is not a docker volume ID
		{"mosquitto_data", false},
		{strings.Repeat("a1", 31), false}, // too short
	}
	for _, tc := range tests {
		if got := isAnonymousVolume(tc.input); got != tc.want {
			t.Errorf("isAnonymousVolume(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestSelectContainers(t *testing.T) {
	all := []rt.ContainerInfo{{Name: "a"}, {Name: "b"}}

	got, err := selectContainers(all, nil)
	if err != nil || len(got) != 2 {
		t.Errorf("selectContainers(all, nil) = %v, %v; want both containers", got, err)
	}

	got, err = selectContainers(all, []string{"b"})
	if err != nil || len(got) != 1 || got[0].Name != "b" {
		t.Errorf("selectContainers(all, [b]) = %v, %v; want [b]", got, err)
	}

	if _, err := selectContainers(all, []string{"a", "missing"}); err == nil {
		t.Error("selectContainers with an unknown name: expected error, got nil")
	}
}

func TestDedupeContainerNames(t *testing.T) {
	results := []generateResult{
		{container: config.Container{Name: "api"}},
		{container: config.Container{Name: "api"}},
		{container: config.Container{Name: "api"}},
		{container: config.Container{Name: "web"}},
	}
	dedupeContainerNames(results)

	want := []string{"api", "api-2", "api-3", "web"}
	for i, w := range want {
		if got := results[i].container.Name; got != w {
			t.Errorf("results[%d].Name = %q, want %q", i, got, w)
		}
	}
	if len(results[1].warnings) == 0 {
		t.Error("renamed container has no warning attached")
	}
}

func TestContainerInfoToConfig(t *testing.T) {
	info := &rt.ContainerInfo{
		ID:   "0123456789abcdef",
		Name: "home_web",
		Labels: map[string]string{
			rt.LabelManaged:                  "true",
			rt.LabelName:                     "web",
			"com.docker.compose.project":     "home",
			"org.opencontainers.image.title": "nginx", // image default, filtered
			"owner":                          "jk",
		},
		Image:      "nginx:1.27",
		Env:        []string{"PATH=/usr/bin", "TZ=Asia/Kolkata"}, // PATH matches image
		Command:    []string{"nginx", "-g", "daemon off;"},       // matches image
		Entrypoint: []string{"/entry.sh"},                        // differs from image
		Hostname:   "web",                                        // same as logical name
		User:       "root",                                       // matches image
		WorkingDir: "/srv",                                       // differs from image
		Ports: []rt.PortBinding{
			{HostPort: "9001", ContainerPort: "9001", Protocol: "tcp"},
			{HostIP: "127.0.0.1", HostPort: "8080", ContainerPort: "80", Protocol: "tcp"},
			{ContainerPort: "443", Protocol: "tcp"}, // exposed only, dropped
		},
		Mounts: []rt.ContainerMount{
			{Type: "volume", Name: strings.Repeat("ab", 32), Destination: "/var/cache"},
			{Type: "bind", Source: "/etc/nginx.conf", Destination: "/etc/nginx/nginx.conf", ReadOnly: true},
			{Type: "volume", Name: "site_data", Destination: "/srv/data"},
			{Type: "tmpfs", Destination: "/run"},
		},
		NetworkInfos: []rt.ContainerNetworkInfo{
			{Name: "home_proxy"},
			{Name: "external_net"},
			{Name: "bridge"},
		},
		NetworkAliases: map[string][]string{
			"home_proxy":   {"web", "0123456789ab", "www"},
			"external_net": {"home_web"},
		},
		RestartPolicy: "unless-stopped",
		Tmpfs:         []string{"/run", "/tmp"},
		Resources:     rt.ContainerResources{NanoCPUs: 500_000_000, MemoryBytes: 32 * 1024 * 1024},
		Healthcheck: &rt.Healthcheck{
			Test:     []string{"CMD", "curl", "-f", "http://localhost/"},
			Interval: 30 * time.Second,
			Retries:  3,
		},
	}
	imgCfg := &rt.ImageConfig{
		Cmd:        []string{"nginx", "-g", "daemon off;"},
		Entrypoint: []string{"/docker-entrypoint.sh"},
		Env:        []string{"PATH=/usr/bin"},
		Labels:     map[string]string{"org.opencontainers.image.title": "nginx"},
		User:       "root",
	}

	res := containerInfoToConfig(info, imgCfg, "home")
	c := res.container

	if c.Name != "web" {
		t.Errorf("Name = %q, want %q (from the containerctl.name label)", c.Name, "web")
	}
	if c.Command != nil {
		t.Errorf("Command = %v, want nil (matches image default)", c.Command)
	}
	if !reflect.DeepEqual([]string(c.Entrypoint), []string{"/entry.sh"}) {
		t.Errorf("Entrypoint = %v, want [/entry.sh]", c.Entrypoint)
	}
	if !reflect.DeepEqual(c.Env, map[string]string{"TZ": "Asia/Kolkata"}) {
		t.Errorf("Env = %v, want only TZ (PATH matches image default)", c.Env)
	}
	if c.User != "" {
		t.Errorf("User = %q, want empty (matches image default)", c.User)
	}
	if c.WorkingDir != "/srv" {
		t.Errorf("WorkingDir = %q, want /srv", c.WorkingDir)
	}
	if c.Hostname != "" {
		t.Errorf("Hostname = %q, want empty (apply derives it from the name)", c.Hostname)
	}
	if !reflect.DeepEqual(c.Labels, map[string]string{"owner": "jk"}) {
		t.Errorf("Labels = %v, want only owner (containerctl.*, compose and image labels dropped)", c.Labels)
	}
	// Sorted by container port; the exposed-only 443 is dropped.
	if want := []string{"127.0.0.1:8080:80", "9001:9001"}; !reflect.DeepEqual(c.Ports, want) {
		t.Errorf("Ports = %v, want %v", c.Ports, want)
	}
	// Sorted by destination; tmpfs and anonymous volumes excluded.
	if want := []string{"/etc/nginx.conf:/etc/nginx/nginx.conf:ro", "site_data:/srv/data"}; !reflect.DeepEqual(c.Volumes, want) {
		t.Errorf("Volumes = %v, want %v", c.Volumes, want)
	}
	if want := []string{strings.Repeat("ab", 32) + ":/var/cache"}; !reflect.DeepEqual(res.anonVols, want) {
		t.Errorf("anonVols = %v, want %v", res.anonVols, want)
	}
	// Project prefix stripped, built-in bridge skipped, sorted by host name.
	if want := []string{"external_net", "proxy"}; !reflect.DeepEqual(c.Networks, want) {
		t.Errorf("Networks = %v, want %v", c.Networks, want)
	}
	if want := []netRef{{logical: "external_net", actual: "external_net"}, {logical: "proxy", actual: "home_proxy"}}; !reflect.DeepEqual(res.networks, want) {
		t.Errorf("networks = %+v, want %+v", res.networks, want)
	}
	// "web" (name), "0123456789ab" (short ID) and "home_web" (container name)
	// are runtime-generated aliases, not user configuration.
	if want := []string{"www"}; !reflect.DeepEqual(c.NetworkAliases, want) {
		t.Errorf("NetworkAliases = %v, want %v", c.NetworkAliases, want)
	}
	if !reflect.DeepEqual(c.Tmpfs, []string{"/run", "/tmp"}) {
		t.Errorf("Tmpfs = %v, want [/run /tmp]", c.Tmpfs)
	}
	if c.Resources.CPUs != "0.5" || c.Resources.Memory != "32m" {
		t.Errorf("Resources = %+v, want cpus 0.5 memory 32m", c.Resources)
	}
	if c.Healthcheck == nil || c.Healthcheck.Interval != "30s" || c.Healthcheck.Timeout != "" {
		t.Errorf("Healthcheck = %+v, want interval 30s and no timeout", c.Healthcheck)
	}
}

func TestContainerInfoToConfigNoImageConfig(t *testing.T) {
	info := &rt.ContainerInfo{
		ID:            "abcdefabcdef01",
		Name:          "standalone",
		Image:         "busybox",
		Command:       []string{"sleep", "1d"},
		Hostname:      "abcdefabcdef", // runtime default: short container ID
		RestartPolicy: "no",
	}

	c := containerInfoToConfig(info, nil, "myproject").container

	if c.Name != "standalone" {
		t.Errorf("Name = %q, want standalone", c.Name)
	}
	if !reflect.DeepEqual([]string(c.Command), []string{"sleep", "1d"}) {
		t.Errorf("Command = %v, want [sleep 1d] (no image config to compare against)", c.Command)
	}
	if c.Hostname != "" {
		t.Errorf("Hostname = %q, want empty (runtime default)", c.Hostname)
	}
	if c.Restart != "" {
		t.Errorf("Restart = %q, want empty (docker reports \"no\" when unset)", c.Restart)
	}
}

func TestInjectAnonVolumeComments(t *testing.T) {
	results := []generateResult{
		{container: config.Container{Name: "a"}, anonVols: []string{"vol1:/data"}},
		{container: config.Container{Name: "b"}},
	}
	in := "project: p\ncontainers:\n  - name: a\n    image: x\n  - name: b\n    image: y\n"

	got := injectAnonVolumeComments(in, results)

	want := "project: p\ncontainers:\n  - name: a\n    image: x\n" +
		"    # Anonymous volumes (auto-generated by the runtime) - replace with explicit paths:\n" +
		"    # - vol1:/data\n" +
		"  - name: b\n    image: y\n"
	if got != want {
		t.Errorf("injectAnonVolumeComments():\n%s\nwant:\n%s", got, want)
	}

	// Comments for the last container must land before any trailing top-level key.
	results[1].anonVols = []string{"vol2:/cache"}
	got = injectAnonVolumeComments(in+"runtime: podman\n", results)
	if !strings.HasSuffix(got, "# - vol2:/cache\nruntime: podman\n") {
		t.Errorf("trailing top-level key not respected:\n%s", got)
	}
}
