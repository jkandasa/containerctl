package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/moby/term"

	cerrdefs "github.com/containerd/errdefs"

	"github.com/jkandasa/containerctl/internal/registry"
	rt "github.com/jkandasa/containerctl/internal/runtime"
)

type Client struct {
	cli      *dockerclient.Client
	authFile string
}

func (c *Client) SetAuthFile(path string) { c.authFile = path }

func New(socketPath string) (*Client, error) {
	opts := []dockerclient.Opt{dockerclient.WithAPIVersionNegotiation()}
	if socketPath != "" {
		opts = append(opts, dockerclient.WithHost("unix://"+socketPath))
	}
	cli, err := dockerclient.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &Client{cli: cli}, nil
}

func (c *Client) Name() string { return "docker" }

func (c *Client) Close() error { return c.cli.Close() }

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.cli.Ping(ctx)
	return err
}

func (c *Client) EngineVersion(ctx context.Context) (rt.EngineInfo, error) {
	v, err := c.cli.ServerVersion(ctx)
	if err != nil {
		return rt.EngineInfo{}, err
	}
	return rt.EngineInfo{
		Version:       v.Version,
		APIVersion:    v.APIVersion,
		MinAPIVersion: v.MinAPIVersion,
		Platform:      v.Platform.Name,
		OS:            v.Os,
		Arch:          v.Arch,
		KernelVersion: v.KernelVersion,
	}, nil
}

func (c *Client) LocalImageMeta(ctx context.Context, img string) (rt.ImageMeta, error) {
	info, err := c.cli.ImageInspect(ctx, img)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return rt.ImageMeta{}, nil
		}
		return rt.ImageMeta{}, fmt.Errorf("inspect image %s: %w", img, err)
	}
	var digest string
	// RepoDigests entries look like "docker.io/library/nginx@sha256:abc..."
	for _, d := range info.RepoDigests {
		if i := strings.Index(d, "@"); i >= 0 {
			digest = d[i+1:]
			break
		}
	}
	return rt.ImageMeta{Digest: digest, Size: info.Size}, nil
}

func (c *Client) RemoteImageDigest(ctx context.Context, img string) (string, error) {
	u, p := credentialsFor(c.authFile, img)
	var creds *registry.Credentials
	if u != "" {
		creds = &registry.Credentials{Username: u, Password: p}
	}
	return registry.RemoteDigest(ctx, img, creds)
}

func (c *Client) ContainerStats(ctx context.Context, id string) (rt.ContainerUsage, error) {
	resp, err := c.cli.ContainerStats(ctx, id, false)
	if err != nil {
		return rt.ContainerUsage{}, err
	}
	defer resp.Body.Close()

	var s struct {
		CPUStats struct {
			CPUUsage struct {
				TotalUsage  uint64   `json:"total_usage"`
				PercpuUsage []uint64 `json:"percpu_usage"`
			} `json:"cpu_usage"`
			ThrottlingData struct {
				ThrottledPeriods uint64 `json:"throttled_periods"`
				ThrottledTime    uint64 `json:"throttled_time"`
			} `json:"throttling_data"`
			SystemUsage uint64 `json:"system_cpu_usage"`
			OnlineCPUs  uint32 `json:"online_cpus"`
		} `json:"cpu_stats"`
		PreCPUStats struct {
			CPUUsage    struct{ TotalUsage uint64 `json:"total_usage"` } `json:"cpu_usage"`
			SystemUsage uint64 `json:"system_cpu_usage"`
		} `json:"precpu_stats"`
		MemoryStats struct {
			Usage    uint64            `json:"usage"`
			Failcnt  uint64            `json:"failcnt"`
			Stats    map[string]uint64 `json:"stats"`
		} `json:"memory_stats"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return rt.ContainerUsage{}, err
	}

	// CPU percentage across all cores
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemUsage) - float64(s.PreCPUStats.SystemUsage)
	numCPUs := float64(s.CPUStats.OnlineCPUs)
	if numCPUs == 0 {
		numCPUs = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
	}
	if numCPUs == 0 {
		numCPUs = 1
	}
	cpuPct := 0.0
	if sysDelta > 0 && cpuDelta > 0 {
		cpuPct = (cpuDelta / sysDelta) * numCPUs * 100.0
	}

	// Working-set memory: subtract inactive file cache (cgroups v2) or cache (v1)
	memUsed := int64(s.MemoryStats.Usage)
	if v, ok := s.MemoryStats.Stats["inactive_file"]; ok {
		memUsed -= int64(v)
	} else if v, ok := s.MemoryStats.Stats["cache"]; ok {
		memUsed -= int64(v)
	}
	if memUsed < 0 {
		memUsed = 0
	}

	return rt.ContainerUsage{
		CPUPercent:          cpuPct,
		CPUThrottledPeriods: s.CPUStats.ThrottlingData.ThrottledPeriods,
		CPUThrottledTimeNs:  s.CPUStats.ThrottlingData.ThrottledTime,
		MemoryUsed:          memUsed,
		MemoryFailCount:     s.MemoryStats.Failcnt,
	}, nil
}

func (c *Client) CheckTagUpdates(ctx context.Context, img string, max int) (*registry.TagUpdates, error) {
	u, p := credentialsFor(c.authFile, img)
	var creds *registry.Credentials
	if u != "" {
		creds = &registry.Credentials{Username: u, Password: p}
	}
	return registry.CheckTagUpdates(ctx, img, max, creds)
}

func (c *Client) Pull(ctx context.Context, img string) error {
	rc, err := c.cli.ImagePull(ctx, img, image.PullOptions{
		RegistryAuth: registryAuth(c.authFile, img),
	})
	if err != nil {
		return fmt.Errorf("pull %s: %w", img, err)
	}
	defer rc.Close()
	_, err = io.Copy(io.Discard, rc)
	return err
}

func (c *Client) CreateContainer(ctx context.Context, spec rt.ContainerSpec) (string, error) {
	cfg := &container.Config{
		Image:      spec.Image,
		Cmd:        spec.Command,
		Entrypoint: spec.Entrypoint,
		Env:        envMapToSlice(spec.Env),
		Labels:     spec.Labels,
		User:       spec.User,
		WorkingDir: spec.WorkingDir,
		Hostname:   spec.Hostname,
		Tty:        false,
	}

	if spec.Healthcheck != nil {
		cfg.Healthcheck = &container.HealthConfig{
			Test:        spec.Healthcheck.Test,
			Interval:    spec.Healthcheck.Interval,
			Timeout:     spec.Healthcheck.Timeout,
			StartPeriod: spec.Healthcheck.StartPeriod,
			Retries:     spec.Healthcheck.Retries,
		}
	}

	portBindings, exposedPorts, err := buildPorts(spec.Ports)
	if err != nil {
		return "", fmt.Errorf("build ports for %s: %w", spec.Name, err)
	}
	cfg.ExposedPorts = exposedPorts

	var pidsLimit *int64
	if spec.Resources.PidsLimit > 0 {
		pidsLimit = &spec.Resources.PidsLimit
	}

	tmpfsMap := make(map[string]string, len(spec.Tmpfs))
	for _, p := range spec.Tmpfs {
		tmpfsMap[p] = ""
	}

	hostCfg := &container.HostConfig{
		PortBindings:  portBindings,
		Binds:         buildBinds(spec.Mounts),
		RestartPolicy: container.RestartPolicy{Name: parseRestartPolicy(spec.RestartPolicy)},
		DNS:           spec.DNS,
		GroupAdd:      spec.GroupAdd,
		CapAdd:        spec.CapAdd,
		CapDrop:       spec.CapDrop,
		Privileged:    spec.Privileged,
		SecurityOpt:   spec.SecurityOpt,
		ReadonlyRootfs: spec.ReadOnly,
		Tmpfs:         tmpfsMap,
		Resources: container.Resources{
			NanoCPUs:  spec.Resources.NanoCPUs,
			Memory:    spec.Resources.MemoryBytes,
			PidsLimit: pidsLimit,
		},
	}

	// use first network in NetworkingConfig; connect others after creation
	var netCfg *network.NetworkingConfig
	if len(spec.Networks) > 0 {
		netCfg = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				spec.Networks[0]: {Aliases: spec.NetworkAliases},
			},
		}
	}

	resp, err := c.cli.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, spec.Name)
	if err != nil {
		return "", fmt.Errorf("create container %s: %w", spec.Name, err)
	}

	// connect additional networks (skip if none or only one, which is in netCfg already)
	for _, netName := range spec.Networks[min(1, len(spec.Networks)):] {
		ep := &network.EndpointSettings{Aliases: spec.NetworkAliases}
		if err := c.cli.NetworkConnect(ctx, netName, resp.ID, ep); err != nil {
			// best-effort cleanup
			_ = c.cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
			return "", fmt.Errorf("connect %s to network %s: %w", spec.Name, netName, err)
		}
	}

	return resp.ID, nil
}

func (c *Client) StartContainer(ctx context.Context, id string) error {
	return c.cli.ContainerStart(ctx, id, container.StartOptions{})
}

func (c *Client) StopContainer(ctx context.Context, id string, timeout time.Duration) error {
	secs := int(timeout.Seconds())
	if secs <= 0 {
		secs = 10
	}
	return c.cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &secs})
}

func (c *Client) RemoveContainer(ctx context.Context, id string, force bool) error {
	return c.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: force})
}

func (c *Client) InspectContainer(ctx context.Context, nameOrID string) (*rt.ContainerInfo, error) {
	info, err := c.cli.ContainerInspect(ctx, nameOrID)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	state := "unknown"
	if info.State != nil {
		state = info.State.Status
	}
	var startedAt time.Time
	if info.State != nil && info.State.StartedAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, info.State.StartedAt); err == nil {
			startedAt = t.Local()
		}
	}
	exitCode := 0
	if info.State != nil {
		exitCode = info.State.ExitCode
	}
	var lastRestart time.Time
	if info.RestartCount > 0 && info.State != nil && info.State.FinishedAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, info.State.FinishedAt); err == nil && t.Year() > 1 {
			lastRestart = t.Local()
		}
	}
	var resources rt.ContainerResources
	if info.HostConfig != nil {
		resources = rt.ContainerResources{
			NanoCPUs:    info.HostConfig.NanoCPUs,
			MemoryBytes: info.HostConfig.Memory,
			PidsLimit:   pidsLimitVal(info.HostConfig.PidsLimit),
		}
	}
	var createdAt time.Time
	if info.Created != "" {
		if t, err := time.Parse(time.RFC3339Nano, info.Created); err == nil {
			createdAt = t.Local()
		}
	}
	name := strings.TrimPrefix(info.Name, "/")
	return &rt.ContainerInfo{
		ID:           info.ID,
		Name:         name,
		Image:        info.Config.Image,
		State:        state,
		Labels:       info.Config.Labels,
		CreatedAt:    createdAt,
		StartedAt:    startedAt,
		ExitCode:     exitCode,
		RestartCount: info.RestartCount,
		LastRestart:  lastRestart,
		Resources:    resources,
	}, nil
}

func (c *Client) ListContainers(ctx context.Context, f rt.Filters) ([]rt.ContainerInfo, error) {
	args := filters.NewArgs()
	for k, v := range f.Labels {
		args.Add("label", k+"="+v)
	}
	for _, name := range f.Names {
		args.Add("name", name)
	}
	list, err := c.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: args})
	if err != nil {
		return nil, err
	}
	out := make([]rt.ContainerInfo, 0, len(list))
	for _, ctr := range list {
		name := ""
		if len(ctr.Names) > 0 {
			name = strings.TrimPrefix(ctr.Names[0], "/")
		}
		var createdAt time.Time
		if ctr.Created > 0 {
			createdAt = time.Unix(ctr.Created, 0)
		}
		var ports []rt.PortBinding
		seenPorts := map[string]bool{}
		for _, p := range ctr.Ports {
			if p.PublicPort == 0 {
				continue
			}
			// Normalise IP: treat 0.0.0.0 and :: as "all interfaces" (no IP prefix).
			ip := p.IP
			if ip == "0.0.0.0" || ip == "::" {
				ip = ""
			}
			// Docker reports one entry per address family; deduplicate by
			// hostPort:containerPort/proto so each binding appears only once.
			key := fmt.Sprintf("%s:%d:%d/%s", ip, p.PublicPort, p.PrivatePort, p.Type)
			if seenPorts[key] {
				continue
			}
			seenPorts[key] = true
			// Track container port as published so we don't repeat it as exposed-only.
			seenPorts[fmt.Sprintf("c:%d/%s", p.PrivatePort, p.Type)] = true
			ports = append(ports, rt.PortBinding{
				HostIP:        ip,
				HostPort:      fmt.Sprintf("%d", p.PublicPort),
				ContainerPort: fmt.Sprintf("%d", p.PrivatePort),
				Protocol:      p.Type,
			})
		}
		// Exposed-only ports (internal only, no host binding).
		for _, p := range ctr.Ports {
			if p.PublicPort != 0 {
				continue
			}
			key := fmt.Sprintf("c:%d/%s", p.PrivatePort, p.Type)
			if seenPorts[key] {
				continue
			}
			seenPorts[key] = true
			ports = append(ports, rt.PortBinding{
				ContainerPort: fmt.Sprintf("%d", p.PrivatePort),
				Protocol:      p.Type,
			})
		}
		var mounts []rt.ContainerMount
		for _, m := range ctr.Mounts {
			mounts = append(mounts, rt.ContainerMount{
				Type:        string(m.Type),
				Name:        m.Name,
				Source:      m.Source,
				Destination: m.Destination,
				ReadOnly:    !m.RW,
			})
		}
		var netInfos []rt.ContainerNetworkInfo
		if ctr.NetworkSettings != nil {
			for n, ep := range ctr.NetworkSettings.Networks {
				info := rt.ContainerNetworkInfo{Name: n}
				if ep != nil {
					info.IPAddress = ep.IPAddress
					info.Gateway = ep.Gateway
				}
				netInfos = append(netInfos, info)
			}
		}
		out = append(out, rt.ContainerInfo{
			ID:           ctr.ID,
			Name:         name,
			Image:        ctr.Image,
			ImageID:      ctr.ImageID,
			Mounts:       mounts,
			NetworkInfos: netInfos,
			State:        ctr.State,
			Labels:       ctr.Labels,
			CreatedAt:    createdAt,
			Ports:        ports,
		})
	}
	return out, nil
}

func (c *Client) Logs(ctx context.Context, id string, opts rt.LogOptions) (io.ReadCloser, error) {
	tail := "all"
	if opts.Tail >= 0 {
		tail = strconv.Itoa(opts.Tail)
	}
	since := ""
	if !opts.Since.IsZero() {
		since = opts.Since.Format(time.RFC3339)
	}
	rc, err := c.cli.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     opts.Follow,
		Timestamps: opts.Timestamps,
		Tail:       tail,
		Since:      since,
	})
	if err != nil {
		return nil, err
	}

	// Containers without a TTY use a multiplexed stream with 8-byte frame
	// headers. Demux through a pipe so the caller gets a clean byte stream.
	// TTY containers use a raw stream that passes through unchanged.
	info, inspectErr := c.cli.ContainerInspect(ctx, id)
	if inspectErr == nil && info.Config != nil && info.Config.Tty {
		return rc, nil
	}
	pr, pw := io.Pipe()
	go func() {
		_, copyErr := stdcopy.StdCopy(pw, pw, rc)
		rc.Close()
		pw.CloseWithError(copyErr)
	}()
	return pr, nil
}

func (c *Client) Exec(ctx context.Context, id string, opts rt.ExecOptions) (int, error) {
	cmd := opts.Command
	if len(cmd) == 0 {
		cmd = []string{"/bin/sh"}
	}

	var initialSize *[2]uint
	if opts.Tty && opts.StdinFd != 0 {
		if ws, err := term.GetWinsize(opts.StdinFd); err == nil {
			sz := [2]uint{uint(ws.Height), uint(ws.Width)}
			initialSize = &sz
		}
	}

	execResp, err := c.cli.ContainerExecCreate(ctx, id, container.ExecOptions{
		Cmd:          cmd,
		AttachStdin:  opts.Interactive,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          opts.Tty,
		Env:          opts.Env,
		ConsoleSize:  initialSize,
	})
	if err != nil {
		return -1, fmt.Errorf("exec create: %w", err)
	}

	attach, err := c.cli.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{
		Tty:         opts.Tty,
		ConsoleSize: initialSize,
	})
	if err != nil {
		return -1, fmt.Errorf("exec attach: %w", err)
	}
	defer attach.Close()

	if opts.Tty && opts.StdinFd != 0 {
		resizeCtx, cancelResize := context.WithCancel(ctx)
		defer cancelResize()
		watchResize(resizeCtx, opts.StdinFd, func(rows, cols uint16) {
			_ = c.cli.ContainerExecResize(resizeCtx, execResp.ID, container.ResizeOptions{
				Height: uint(rows),
				Width:  uint(cols),
			})
		})
	}

	done := make(chan error, 1)
	go func() {
		if opts.Tty {
			_, err = io.Copy(opts.Stdout, attach.Reader)
		} else {
			_, err = stdcopy.StdCopy(opts.Stdout, opts.Stderr, attach.Reader)
		}
		done <- err
	}()

	if opts.Interactive && opts.Stdin != nil {
		go func() {
			io.Copy(attach.Conn, opts.Stdin)
			attach.CloseWrite()
		}()
	}

	<-done

	inspect, err := c.cli.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return -1, fmt.Errorf("exec inspect: %w", err)
	}
	return inspect.ExitCode, nil
}

func (c *Client) CreateNetwork(ctx context.Context, spec rt.NetworkSpec) (string, error) {
	resp, err := c.cli.NetworkCreate(ctx, spec.Name, network.CreateOptions{
		Driver: spec.Driver,
		Labels: spec.Labels,
	})
	if err != nil {
		return "", fmt.Errorf("create network %s: %w", spec.Name, err)
	}
	return resp.ID, nil
}

func (c *Client) RemoveNetwork(ctx context.Context, nameOrID string) error {
	return c.cli.NetworkRemove(ctx, nameOrID)
}

func (c *Client) ListNetworks(ctx context.Context, f rt.Filters) ([]rt.NetworkInfo, error) {
	args := filters.NewArgs()
	for k, v := range f.Labels {
		args.Add("label", k+"="+v)
	}
	if f.Dangling != nil {
		if *f.Dangling {
			args.Add("dangling", "true")
		} else {
			args.Add("dangling", "false")
		}
	}
	list, err := c.cli.NetworkList(ctx, network.ListOptions{Filters: args})
	if err != nil {
		return nil, err
	}
	out := make([]rt.NetworkInfo, 0, len(list))
	for _, n := range list {
		out = append(out, rt.NetworkInfo{
			ID:     n.ID,
			Name:   n.Name,
			Driver: n.Driver,
			Labels: n.Labels,
		})
	}
	return out, nil
}

func (c *Client) NetworkExists(ctx context.Context, name string) (bool, error) {
	args := filters.NewArgs()
	args.Add("name", name)
	list, err := c.cli.NetworkList(ctx, network.ListOptions{Filters: args})
	if err != nil {
		return false, err
	}
	for _, n := range list {
		if n.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (c *Client) ListImages(ctx context.Context) ([]rt.ImageInfo, error) {
	imgs, err := c.cli.ImageList(ctx, image.ListOptions{All: false})
	if err != nil {
		return nil, err
	}
	out := make([]rt.ImageInfo, 0, len(imgs))
	for _, img := range imgs {
		id := strings.TrimPrefix(img.ID, "sha256:")
		if len(id) > 12 {
			id = id[:12]
		}
		var digest string
		for _, d := range img.RepoDigests {
			if i := strings.Index(d, "@"); i >= 0 {
				digest = d[i+1:]
				break
			}
		}
		out = append(out, rt.ImageInfo{
			ID:      id,
			Tags:    img.RepoTags,
			Digest:  digest,
			Size:    img.Size,
			Created: time.Unix(img.Created, 0),
		})
	}
	return out, nil
}

func (c *Client) RemoveImage(ctx context.Context, id string, force bool) error {
	_, err := c.cli.ImageRemove(ctx, id, image.RemoveOptions{Force: force, PruneChildren: true})
	return err
}

func (c *Client) ListVolumes(ctx context.Context, f rt.Filters) ([]rt.VolumeInfo, error) {
	args := filters.NewArgs()
	if f.Dangling != nil {
		if *f.Dangling {
			args.Add("dangling", "true")
		} else {
			args.Add("dangling", "false")
		}
	}
	resp, err := c.cli.VolumeList(ctx, volume.ListOptions{Filters: args})
	if err != nil {
		return nil, err
	}
	out := make([]rt.VolumeInfo, 0, len(resp.Volumes))
	for _, v := range resp.Volumes {
		out = append(out, rt.VolumeInfo{
			Name:       v.Name,
			Driver:     v.Driver,
			Mountpoint: v.Mountpoint,
			Labels:     v.Labels,
		})
	}
	return out, nil
}

func (c *Client) RemoveVolume(ctx context.Context, name string, force bool) error {
	return c.cli.VolumeRemove(ctx, name, force)
}

func (c *Client) VolumeSizes(ctx context.Context) (map[string]int64, error) {
	du, err := c.cli.DiskUsage(ctx, types.DiskUsageOptions{
		Types: []types.DiskUsageObject{types.VolumeObject},
	})
	if err != nil {
		return nil, err
	}
	m := make(map[string]int64, len(du.Volumes))
	for _, v := range du.Volumes {
		if v.UsageData != nil {
			m[v.Name] = v.UsageData.Size // -1 when driver doesn't report
		}
	}
	return m, nil
}

func envMapToSlice(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

func buildPorts(ports []rt.PortBinding) (nat.PortMap, nat.PortSet, error) {
	pm := nat.PortMap{}
	ps := nat.PortSet{}
	for _, p := range ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		containerPort, err := nat.NewPort(proto, p.ContainerPort)
		if err != nil {
			return nil, nil, err
		}
		ps[containerPort] = struct{}{}
		pm[containerPort] = []nat.PortBinding{{
			HostIP:   p.HostIP,
			HostPort: p.HostPort,
		}}
	}
	return pm, ps, nil
}

func buildBinds(mounts []rt.Mount) []string {
	out := make([]string, 0, len(mounts))
	for _, m := range mounts {
		if m.Type == "bind" || m.Type == "volume" || m.Type == "" {
			s := m.Source + ":" + m.Target
			if m.ReadOnly {
				s += ":ro"
			}
			out = append(out, s)
		}
	}
	return out
}

func pidsLimitVal(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func parseRestartPolicy(s string) container.RestartPolicyMode {
	switch s {
	case "always":
		return container.RestartPolicyAlways
	case "on-failure":
		return container.RestartPolicyOnFailure
	case "unless-stopped":
		return container.RestartPolicyUnlessStopped
	default:
		return container.RestartPolicyDisabled
	}
}
