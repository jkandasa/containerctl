package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	"github.com/jkandasa/containerctl/internal/render"
	rt "github.com/jkandasa/containerctl/internal/runtime"
	"github.com/jkandasa/containerctl/internal/state"
)

var statusCmd = &cobra.Command{
	Use:   "status [name...]",
	Short: "Show state and sync status of all managed containers",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	stack, err := config.Load(flagFile)
	if err != nil {
		return err
	}
	if flagProject != "" {
		stack.Project = flagProject
	}

	runtime, err := runtimeFrom(stack)
	if err != nil {
		return err
	}
	defer runtime.Close()

	if err := pingRuntime(ctx, runtime); err != nil {
		return err
	}

	st, err := state.Load(stack.Project)
	if err != nil {
		return err
	}

	live, err := runtime.ListContainers(ctx, rt.Filters{
		Labels: map[string]string{
			rt.LabelManaged: "true",
			rt.LabelProject: stack.Project,
		},
	})
	if err != nil {
		return err
	}

	liveByName := make(map[string]rt.ContainerInfo, len(live))
	for _, c := range live {
		name := c.Labels[rt.LabelName]
		if name != "" {
			liveByName[name] = c
		}
	}

	filterSet := make(map[string]bool, len(args))
	for _, a := range args {
		filterSet[a] = true
	}

	var entries []render.StatusEntry
	for _, c := range stack.Containers {
		if len(filterSet) > 0 && !filterSet[c.Name] {
			continue
		}
		entry := render.StatusEntry{
			Name:  c.Name,
			Image: c.Image,
			Ports: parseDeclaredPorts(c.Ports),
			Sync:  "-",
		}

		if c.Disabled {
			entry.State = "declared-off"
			entry.Note = "disabled: true in YAML"
			entries = append(entries, entry)
			continue
		}

		lc, exists := liveByName[c.Name]
		if !exists {
			if st.IsDisabled(c.Name) {
				entry.State = "disabled"
				entry.Note = "disabled via state file (container not on host)"
			} else {
				entry.State = "missing"
				entry.Note = "apply will create"
			}
			entries = append(entries, entry)
			continue
		}

		if st.IsDisabled(c.Name) {
			entry.State = "disabled"
			entry.Image = lc.Image
			entry.ContainerID = shortID(lc.ID)
			entry.Ports = portBindingsToEntries(lc.Ports)
			if !lc.StartedAt.IsZero() {
				entry.StartedAt = &lc.StartedAt
			}
			entry.Note = "disabled via state file"
			entries = append(entries, entry)
			continue
		}

		entry.State = lc.State
		entry.Image = lc.Image
		entry.ContainerName = lc.Name
		entry.ContainerID = shortID(lc.ID)
		entry.Ports = portBindingsToEntries(lc.Ports)
		if !lc.StartedAt.IsZero() {
			entry.StartedAt = &lc.StartedAt
		}

		// Image digest and size.
		if meta, err := runtime.LocalImageMeta(ctx, lc.Image); err == nil && meta.Digest != "" {
			entry.ImageDigest = meta.Digest
			if meta.Size > 0 {
				entry.ImageSize = formatImageSize(meta.Size)
			}
		}

		// Inspect for restart count, last restart, exit code, and resource limits.
		if detail, err := runtime.InspectContainer(ctx, lc.ID); err == nil && detail != nil {
			entry.RestartCount = detail.RestartCount
			if !detail.LastRestart.IsZero() {
				entry.LastRestart = &detail.LastRestart
			}
			if detail.ExitCode != 0 || lc.State == "exited" {
				ec := detail.ExitCode
				entry.ExitCode = &ec
			}
			if r := detail.Resources; r.NanoCPUs > 0 || r.MemoryBytes > 0 || r.PidsLimit > 0 {
				rl := &render.ResourceLimits{Pids: r.PidsLimit}
				if r.NanoCPUs > 0 {
					rl.CPUs = formatCPUs(r.NanoCPUs)
				}
				if r.MemoryBytes > 0 {
					rl.Memory = formatImageSize(r.MemoryBytes)
				}
				entry.Resources = rl
			}
		}

		expectedHash := config.Hash(&c)
		if lc.Labels[rt.LabelConfigHash] == expectedHash {
			entry.Sync = "ok"
		} else {
			entry.Sync = "drift"
		}
		entries = append(entries, entry)
	}

	// also show managed containers not in YAML (orphans)
	declaredNames := make(map[string]bool, len(stack.Containers))
	for _, c := range stack.Containers {
		declaredNames[c.Name] = true
	}
	for name, lc := range liveByName {
		if declaredNames[name] {
			continue
		}
		if len(filterSet) > 0 && !filterSet[name] {
			continue
		}
		entry := render.StatusEntry{
			Name:          name,
			ContainerName: lc.Name,
			State:         lc.State,
			Image:         lc.Image,
			ContainerID:   shortID(lc.ID),
			Ports:         portBindingsToEntries(lc.Ports),
			Sync:          "-",
			Note:          "not in stack.yaml (orphan)",
		}
		if !lc.StartedAt.IsZero() {
			entry.StartedAt = &lc.StartedAt
		}
		if meta, err := runtime.LocalImageMeta(ctx, lc.Image); err == nil && meta.Digest != "" {
			entry.ImageDigest = meta.Digest
			if meta.Size > 0 {
				entry.ImageSize = formatImageSize(meta.Size)
			}
		}
		if detail, err := runtime.InspectContainer(ctx, lc.ID); err == nil && detail != nil {
			entry.RestartCount = detail.RestartCount
			if !detail.LastRestart.IsZero() {
				entry.LastRestart = &detail.LastRestart
			}
			if detail.ExitCode != 0 || lc.State == "exited" {
				ec := detail.ExitCode
				entry.ExitCode = &ec
			}
			if r := detail.Resources; r.NanoCPUs > 0 || r.MemoryBytes > 0 || r.PidsLimit > 0 {
				rl := &render.ResourceLimits{Pids: r.PidsLimit}
				if r.NanoCPUs > 0 {
					rl.CPUs = formatCPUs(r.NanoCPUs)
				}
				if r.MemoryBytes > 0 {
					rl.Memory = formatImageSize(r.MemoryBytes)
				}
				entry.Resources = rl
			}
		}
		entries = append(entries, entry)
	}

	render.Status(os.Stdout, entries, render.Format(flagOutput), colors())
	return nil
}

// portBindingsToEntries converts runtime port bindings to render PortEntry slice.
func portBindingsToEntries(ports []rt.PortBinding) []render.PortEntry {
	out := make([]render.PortEntry, 0, len(ports))
	for _, p := range ports {
		out = append(out, render.PortEntry{
			HostIP:        p.HostIP,
			HostPort:      p.HostPort,
			ContainerPort: p.ContainerPort,
			Protocol:      p.Protocol,
		})
	}
	return out
}

// parseDeclaredPorts parses stack.yaml port strings into PortEntry structs.
// Formats: "CONTAINER[/proto]", "HOST:CONTAINER[/proto]", "IP:HOST:CONTAINER[/proto]"
func parseDeclaredPorts(ports []string) []render.PortEntry {
	out := make([]render.PortEntry, 0, len(ports))
	for _, s := range ports {
		proto := "tcp"
		if idx := strings.LastIndex(s, "/"); idx >= 0 {
			proto = s[idx+1:]
			s = s[:idx]
		}
		parts := strings.SplitN(s, ":", 3)
		var e render.PortEntry
		switch len(parts) {
		case 1:
			e = render.PortEntry{ContainerPort: parts[0], Protocol: proto}
		case 2:
			e = render.PortEntry{HostPort: parts[0], ContainerPort: parts[1], Protocol: proto}
		case 3:
			e = render.PortEntry{HostIP: parts[0], HostPort: parts[1], ContainerPort: parts[2], Protocol: proto}
		default:
			e = render.PortEntry{ContainerPort: s, Protocol: proto}
		}
		out = append(out, e)
	}
	return out
}

// shortID returns the first 12 characters of a container ID.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// formatImageSize converts bytes to a human-readable string (KiB/MiB/GiB).
func formatImageSize(b int64) string {
	const (
		KiB = 1024
		MiB = 1024 * KiB
		GiB = 1024 * MiB
	)
	switch {
	case b >= GiB:
		return fmt.Sprintf("%.1f GiB", float64(b)/GiB)
	case b >= MiB:
		return fmt.Sprintf("%.1f MiB", float64(b)/MiB)
	case b >= KiB:
		return fmt.Sprintf("%.1f KiB", float64(b)/KiB)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// formatCPUs converts NanoCPUs to a decimal CPU string (e.g. "2.0", "0.5").
func formatCPUs(nanoCPUs int64) string {
	return fmt.Sprintf("%.2g", float64(nanoCPUs)/1e9)
}

