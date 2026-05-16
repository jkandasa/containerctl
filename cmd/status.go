package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	"github.com/jkandasa/containerctl/internal/render"
	rt "github.com/jkandasa/containerctl/internal/runtime"
	"github.com/jkandasa/containerctl/internal/state"
)

var (
	statusCmd = &cobra.Command{
		Use:   "status [name...]",
		Short: "Show state and sync status of all managed containers",
		RunE:  runStatus,
	}
	flagStats         bool
	flagWatch         bool
	flagWatchInterval time.Duration
)

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.Flags().BoolVar(&flagStats, "stats", false, "show live CPU and memory usage (adds ~1-2s)")
	statusCmd.Flags().BoolVarP(&flagWatch, "watch", "w", false, "refresh status repeatedly")
	statusCmd.Flags().DurationVar(&flagWatchInterval, "interval", 2*time.Second, "refresh interval (used with --watch), e.g. 500ms, 5s, 1m")
}

// liveData holds the pre-fetched results for a single live container.
type liveData struct {
	meta     rt.ImageMeta
	usage    rt.ContainerUsage
	hasUsage bool
	detail   *rt.ContainerInfo
}

func runStatus(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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

	if !flagWatch {
		return renderStatus(ctx, runtime, stack, args, os.Stdout)
	}

	// Watch mode: render into a buffer, then write to stdout in one shot to
	// avoid flicker. Move cursor to home without clearing so content is
	// overwritten in-place; \033[J erases any leftover lines below.
	// Sleep starts after render completes so output is visible for the full interval.
	for {
		var buf bytes.Buffer
		_ = renderStatus(ctx, runtime, stack, args, &buf)
		// Move to home, then replace each \n with \033[K\n so leftover
		// characters from a wider previous line are erased. Finish with
		// \033[J to clear any lines from a previously taller render.
		frame := append([]byte("\033[H"),
			bytes.ReplaceAll(buf.Bytes(), []byte("\n"), []byte("\033[K\n"))...)
		frame = append(frame, []byte("\033[J")...)
		os.Stdout.Write(frame)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(flagWatchInterval):
		}
	}
}

// renderStatus fetches and prints a single status snapshot to w.
func renderStatus(ctx context.Context, runtime rt.Runtime, stack *config.Stack, args []string, w io.Writer) error {
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

	// Pre-fetch per-container data for all live containers in parallel.
	dataByID := make(map[string]*liveData, len(live))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, lc := range live {
		wg.Add(1)
		go func(lc rt.ContainerInfo) {
			defer wg.Done()
			d := &liveData{}
			if meta, err := runtime.LocalImageMeta(ctx, lc.Image); err == nil {
				d.meta = meta
			}
			if flagStats && lc.State == "running" {
				sCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				if usage, err := runtime.ContainerStats(sCtx, lc.ID); err == nil {
					d.usage = usage
					d.hasUsage = true
				}
				cancel()
			}
			if detail, err := runtime.InspectContainer(ctx, lc.ID); err == nil {
				d.detail = detail
			}
			mu.Lock()
			dataByID[lc.ID] = d
			mu.Unlock()
		}(lc)
	}
	wg.Wait()

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

		if d := dataByID[lc.ID]; d != nil {
			if d.meta.Digest != "" {
				entry.ImageDigest = d.meta.Digest
				if d.meta.Size > 0 {
					entry.ImageSize = formatImageSize(d.meta.Size)
				}
			}
			if d.hasUsage {
				pct := d.usage.CPUPercent
				entry.CPUPercent = &pct
				if d.usage.MemoryUsed > 0 {
					entry.MemoryUsedBytes = d.usage.MemoryUsed
					entry.MemoryUsed = formatImageSize(d.usage.MemoryUsed)
				}
			}
			if detail := d.detail; detail != nil {
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
		if d := dataByID[lc.ID]; d != nil {
			if d.meta.Digest != "" {
				entry.ImageDigest = d.meta.Digest
				if d.meta.Size > 0 {
					entry.ImageSize = formatImageSize(d.meta.Size)
				}
			}
			if d.hasUsage {
				pct := d.usage.CPUPercent
				entry.CPUPercent = &pct
				if d.usage.MemoryUsed > 0 {
					entry.MemoryUsedBytes = d.usage.MemoryUsed
					entry.MemoryUsed = formatImageSize(d.usage.MemoryUsed)
				}
			}
			if detail := d.detail; detail != nil {
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
		}
		entries = append(entries, entry)
	}

	if flagWatch {
		fmt.Fprintf(w, "Every %s: containerctl status          %s\n\n",
			flagWatchInterval, time.Now().Format("2006-01-02 15:04:05"))
	}
	render.Status(w, entries, render.Format(flagOutput), colors())
	return nil
}

// portBindingsToEntries converts runtime port bindings to render PortEntry slice.
// Entries are sorted by container port then protocol for stable watch output.
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
	sort.Slice(out, func(i, j int) bool {
		if out[i].ContainerPort != out[j].ContainerPort {
			return out[i].ContainerPort < out[j].ContainerPort
		}
		if out[i].Protocol != out[j].Protocol {
			return out[i].Protocol < out[j].Protocol
		}
		if out[i].HostPort != out[j].HostPort {
			return out[i].HostPort < out[j].HostPort
		}
		return out[i].HostIP < out[j].HostIP
	})
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
