package reconcile

import (
	"context"
	"fmt"
	"io"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/jkandasa/containerctl/internal/config"
	rt "github.com/jkandasa/containerctl/internal/runtime"
)

type Result struct {
	Created   []string
	Recreated []string
	Removed   []string
	Skipped   []string
	Failed    []string
}

func (r *Result) HasFailures() bool { return len(r.Failed) > 0 }

func Apply(ctx context.Context, plan *Plan, runtime rt.Runtime, w io.Writer) (*Result, error) {
	res := &Result{}

	// 1. reconcile networks
	for _, na := range plan.Networks {
		switch na.Action {
		case ActionCreate:
			labels := managedNetLabels(plan.Project, na.Name)
			maps.Copy(labels, na.Spec.Labels)
			if _, err := runtime.CreateNetwork(ctx, rt.NetworkSpec{
				Name:   na.FullName,
				Driver: na.Spec.Driver,
				Labels: labels,
			}); err != nil {
				fmt.Fprintf(w, "  network %-20s created   → error: %v\n", na.Name, err)
				return res, fmt.Errorf("create network %s: %w", na.FullName, err)
			}
			fmt.Fprintf(w, "  network %-20s created\n", na.Name)
		case ActionRemove:
			if err := runtime.RemoveNetwork(ctx, na.FullName); err != nil {
				fmt.Fprintf(w, "  network %-20s removed   → error: %v\n", na.Name, err)
				return res, fmt.Errorf("remove network %s: %w", na.FullName, err)
			}
			fmt.Fprintf(w, "  network %-20s removed\n", na.Name)
		}
	}

	// 2. execute container actions in topo order
	for i := range plan.Containers {
		ca := &plan.Containers[i]
		switch ca.Action {
		case ActionCreate:
			if err := createAndStart(ctx, plan.Project, ca.Spec, runtime); err != nil {
				res.Failed = append(res.Failed, ca.Name)
				fmt.Fprintf(w, "  %-20s created   → error: %v\n", ca.Name, err)
				continue
			}
			res.Created = append(res.Created, ca.Name)
			fmt.Fprintf(w, "  %-20s created   → running\n", ca.Name)

		case ActionRecreate:
			if err := stopAndRemove(ctx, ca.RunningID, runtime); err != nil {
				res.Failed = append(res.Failed, ca.Name)
				fmt.Fprintf(w, "  %-20s recreated → error: %v\n", ca.Name, err)
				continue
			}
			if err := createAndStart(ctx, plan.Project, ca.Spec, runtime); err != nil {
				res.Failed = append(res.Failed, ca.Name)
				fmt.Fprintf(w, "  %-20s recreated → error: %v\n", ca.Name, err)
				continue
			}
			res.Recreated = append(res.Recreated, ca.Name)
			fmt.Fprintf(w, "  %-20s recreated → running\n", ca.Name)

		case ActionRemove:
			if ca.RunningID != "" {
				if err := stopAndRemove(ctx, ca.RunningID, runtime); err != nil {
					res.Failed = append(res.Failed, ca.Name)
					fmt.Fprintf(w, "  %-20s removed   → error: %v\n", ca.Name, err)
					continue
				}
			}
			res.Removed = append(res.Removed, ca.Name)
			fmt.Fprintf(w, "  %-20s removed\n", ca.Name)

		case ActionSkip:
			res.Skipped = append(res.Skipped, ca.Name)
			fmt.Fprintf(w, "  %-20s skip\n", ca.Name)

		case ActionDisabled:
			res.Skipped = append(res.Skipped, ca.Name)
			fmt.Fprintf(w, "  %-20s disabled\n", ca.Name)

		case ActionDeclaredOff:
			res.Skipped = append(res.Skipped, ca.Name)
			fmt.Fprintf(w, "  %-20s off\n", ca.Name)
		}
	}

	return res, nil
}

// PullImages pulls all images needed for Create/Recreate actions in parallel (bounded by workers).
func PullImages(ctx context.Context, plan *Plan, runtime rt.Runtime, workers int) []error {
	type job struct {
		image string
		name  string
	}
	var jobs []job
	seen := map[string]bool{}
	for _, ca := range plan.Containers {
		if (ca.Action == ActionCreate || ca.Action == ActionRecreate) && ca.Spec != nil {
			img := ca.Spec.Image
			if !seen[img] {
				seen[img] = true
				jobs = append(jobs, job{image: img, name: ca.Name})
			}
		}
	}

	if len(jobs) == 0 {
		return nil
	}

	type result struct {
		name string
		err  error
	}
	ch := make(chan result, len(jobs))
	sem := make(chan struct{}, workers)

	for _, j := range jobs {
		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			err := runtime.Pull(ctx, j.image)
			ch <- result{name: j.name, err: err}
		}()
	}

	var errs []error
	for range jobs {
		r := <-ch
		if r.err != nil {
			errs = append(errs, fmt.Errorf("pull image for %s: %w", r.name, r.err))
		}
	}
	return errs
}

func createAndStart(ctx context.Context, project string, c *config.Container, runtime rt.Runtime) error {
	spec, err := containerSpec(project, c)
	if err != nil {
		return err
	}
	id, err := runtime.CreateContainer(ctx, spec)
	if err != nil {
		return err
	}
	return runtime.StartContainer(ctx, id)
}

func stopAndRemove(ctx context.Context, id string, runtime rt.Runtime) error {
	if err := runtime.StopContainer(ctx, id, 10*time.Second); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	return runtime.RemoveContainer(ctx, id, false)
}

// ContainerSpecFrom builds a runtime ContainerSpec from a config Container.
func ContainerSpecFrom(project string, c *config.Container) (rt.ContainerSpec, error) {
	return containerSpec(project, c)
}

// effectiveHostname returns the declared hostname, or the logical container name
// when none is set. Docker's embedded DNS resolves containers by both their full
// container name AND hostname within a user-defined network, so this lets other
// containers reach home-services_mosquitto simply as "mosquitto".
func effectiveHostname(c *config.Container) string {
	if c.Hostname != "" {
		return c.Hostname
	}
	return c.Name
}

func containerSpec(project string, c *config.Container) (rt.ContainerSpec, error) {
	labels := managedContainerLabels(project, c.Name, config.Hash(c))
	for k, v := range c.Labels {
		if !strings.HasPrefix(k, "containerctl.") {
			labels[k] = v
		}
	}

	ports, err := parsePorts(c.Ports)
	if err != nil {
		return rt.ContainerSpec{}, fmt.Errorf("containers[%s]: %w", c.Name, err)
	}

	mounts, err := parseMounts(c.Volumes)
	if err != nil {
		return rt.ContainerSpec{}, fmt.Errorf("containers[%s]: %w", c.Name, err)
	}

	resources, err := parseResources(c.Resources)
	if err != nil {
		return rt.ContainerSpec{}, fmt.Errorf("containers[%s]: %w", c.Name, err)
	}

	var hc *rt.Healthcheck
	if c.Healthcheck != nil {
		hc, err = parseHealthcheck(c.Healthcheck)
		if err != nil {
			return rt.ContainerSpec{}, fmt.Errorf("containers[%s]: %w", c.Name, err)
		}
	}

	// resolve network full names (project_name)
	nets := make([]string, len(c.Networks))
	for i, n := range c.Networks {
		nets[i] = config.NetworkFullName(project, n)
	}

	return rt.ContainerSpec{
		Name:          config.ContainerFullName(project, c.Name),
		Image:         c.Image,
		Command:       c.Command,
		Entrypoint:    c.Entrypoint,
		Env:           c.Env,
		Labels:        labels,
		Ports:         ports,
		Mounts:        mounts,
		Networks:       nets,
		NetworkAliases: c.NetworkAliases,
		Resources:     resources,
		Healthcheck:   hc,
		RestartPolicy: c.Restart,
		User:          c.User,
		WorkingDir:    c.WorkingDir,
		Hostname:      effectiveHostname(c),
		DNS:           c.DNS,
		CapAdd:        c.CapAdd,
		CapDrop:       c.CapDrop,
		Privileged:    c.Privileged,
		SecurityOpt:   c.SecurityOpt,
		ReadOnly:      c.ReadOnly,
		Tmpfs:         c.Tmpfs,
	}, nil
}


func managedContainerLabels(project, name, hash string) map[string]string {
	return map[string]string{
		rt.LabelManaged:     "true",
		rt.LabelProject:     project,
		rt.LabelName:        name,
		rt.LabelConfigHash:  hash,
		rt.LabelSpecVersion: rt.SpecVersion,
	}
}

func managedNetLabels(project, name string) map[string]string {
	return map[string]string{
		rt.LabelManaged:     "true",
		rt.LabelProject:     project,
		rt.LabelName:        name,
		rt.LabelSpecVersion: rt.SpecVersion,
	}
}

func parsePorts(ports []string) ([]rt.PortBinding, error) {
	out := make([]rt.PortBinding, 0, len(ports))
	for _, p := range ports {
		pb, err := parsePort(p)
		if err != nil {
			return nil, fmt.Errorf("invalid port %q: %w", p, err)
		}
		out = append(out, pb)
	}
	return out, nil
}

func parsePort(s string) (rt.PortBinding, error) {
	proto := "tcp"
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		proto = s[idx+1:]
		s = s[:idx]
	}

	parts := strings.Split(s, ":")
	switch len(parts) {
	case 1:
		return rt.PortBinding{ContainerPort: parts[0], Protocol: proto}, nil
	case 2:
		return rt.PortBinding{HostPort: parts[0], ContainerPort: parts[1], Protocol: proto}, nil
	case 3:
		return rt.PortBinding{HostIP: parts[0], HostPort: parts[1], ContainerPort: parts[2], Protocol: proto}, nil
	default:
		return rt.PortBinding{}, fmt.Errorf("expected [IP:]HOST:CONTAINER[/proto]")
	}
}

func parseMounts(volumes []string) ([]rt.Mount, error) {
	out := make([]rt.Mount, 0, len(volumes))
	for _, v := range volumes {
		m, err := parseMount(v)
		if err != nil {
			return nil, fmt.Errorf("invalid volume %q: %w", v, err)
		}
		out = append(out, m)
	}
	return out, nil
}

func parseMount(s string) (rt.Mount, error) {
	parts := strings.SplitN(s, ":", 3)
	switch len(parts) {
	case 1:
		return rt.Mount{Type: "volume", Target: parts[0]}, nil
	case 2:
		t := mountType(parts[0])
		return rt.Mount{Type: t, Source: parts[0], Target: parts[1]}, nil
	case 3:
		t := mountType(parts[0])
		ro := parts[2] == "ro" || parts[2] == "readonly"
		return rt.Mount{Type: t, Source: parts[0], Target: parts[1], ReadOnly: ro}, nil
	default:
		return rt.Mount{}, fmt.Errorf("expected SRC:DST[:MODE]")
	}
}

func mountType(src string) string {
	if strings.HasPrefix(src, "/") || strings.HasPrefix(src, "./") || strings.HasPrefix(src, "../") {
		return "bind"
	}
	return "volume"
}

func parseResources(r config.Resources) (rt.Resources, error) {
	var res rt.Resources
	if r.CPUs != "" {
		f, err := strconv.ParseFloat(r.CPUs, 64)
		if err != nil {
			return res, fmt.Errorf("invalid cpus %q: %w", r.CPUs, err)
		}
		res.NanoCPUs = int64(f * 1e9)
	}
	if r.Memory != "" {
		b, err := parseMemory(r.Memory)
		if err != nil {
			return res, fmt.Errorf("invalid memory %q: %w", r.Memory, err)
		}
		res.MemoryBytes = b
	}
	res.PidsLimit = r.PidsLimit
	return res, nil
}

func parseMemory(s string) (int64, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, nil
	}
	units := map[byte]int64{'k': 1024, 'm': 1024 * 1024, 'g': 1024 * 1024 * 1024, 't': 1024 * 1024 * 1024 * 1024}
	last := s[len(s)-1]
	if mul, ok := units[last]; ok {
		v, err := strconv.ParseFloat(s[:len(s)-1], 64)
		if err != nil {
			return 0, err
		}
		return int64(v * float64(mul)), nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	return v, err
}

func parseHealthcheck(h *config.Healthcheck) (*rt.Healthcheck, error) {
	hc := &rt.Healthcheck{
		Test:    h.Test,
		Retries: h.Retries,
	}
	var err error
	if h.Interval != "" {
		if hc.Interval, err = time.ParseDuration(h.Interval); err != nil {
			return nil, fmt.Errorf("healthcheck.interval: %w", err)
		}
	}
	if h.Timeout != "" {
		if hc.Timeout, err = time.ParseDuration(h.Timeout); err != nil {
			return nil, fmt.Errorf("healthcheck.timeout: %w", err)
		}
	}
	if h.Start != "" {
		if hc.StartPeriod, err = time.ParseDuration(h.Start); err != nil {
			return nil, fmt.Errorf("healthcheck.start_period: %w", err)
		}
	}
	return hc, nil
}
