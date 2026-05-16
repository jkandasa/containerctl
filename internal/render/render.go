package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/jkandasa/containerctl/internal/reconcile"
)

type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
	FormatYAML Format = "yaml"
)

type Colors struct {
	Reset  string
	Green  string
	Yellow string
	Red    string
	Cyan   string
	Gray   string
}

func NoColors() Colors { return Colors{} }

func ANSIColors() Colors {
	return Colors{
		Reset:  "\033[0m",
		Green:  "\033[32m",
		Yellow: "\033[33m",
		Red:    "\033[31m",
		Cyan:   "\033[36m",
		Gray:   "\033[90m",
	}
}

func Plan(w io.Writer, plan *reconcile.Plan, colors Colors) {
	c := colors

	fmt.Fprintf(w, "Project: %s\n", plan.Project)

	if len(plan.Networks) > 0 {
		fmt.Fprintf(w, "\nNetworks:\n")
		for _, n := range plan.Networks {
			switch n.Action {
			case reconcile.ActionCreate:
				fmt.Fprintf(w, "  %s+ create%s   %s\n", c.Green, c.Reset, n.Name)
			case reconcile.ActionRemove:
				fmt.Fprintf(w, "  %s- remove%s   %s\n", c.Red, c.Reset, n.Name)
			case reconcile.ActionSkip:
				fmt.Fprintf(w, "  %s= skip%s     %s\n", c.Gray, c.Reset, n.Name)
			}
		}
	}

	if len(plan.Containers) > 0 {
		fmt.Fprintf(w, "\nContainers:\n")
		for _, a := range plan.Containers {
			switch a.Action {
			case reconcile.ActionCreate:
				fmt.Fprintf(w, "  %s+ create%s    %-20s (image: %s)\n", c.Green, c.Reset, a.Name, a.Spec.Image)
			case reconcile.ActionRecreate:
				fmt.Fprintf(w, "  %s~ recreate%s  %-20s (%s)\n", c.Yellow, c.Reset, a.Name, a.Reason)
			case reconcile.ActionSkip:
				fmt.Fprintf(w, "  %s= skip%s      %-20s (no changes)\n", c.Gray, c.Reset, a.Name)
			case reconcile.ActionRemove:
				fmt.Fprintf(w, "  %s- remove%s    %-20s (%s)\n", c.Red, c.Reset, a.Name, a.Reason)
			case reconcile.ActionDisabled:
				fmt.Fprintf(w, "  %s! disabled%s  %-20s (disabled via state file; skipped)\n", c.Cyan, c.Reset, a.Name)
			case reconcile.ActionDeclaredOff:
				fmt.Fprintf(w, "  %sx off%s       %-20s (disabled: true in YAML; not present)\n", c.Gray, c.Reset, a.Name)
			}
		}
	}

	for _, w2 := range plan.Warnings {
		fmt.Fprintf(w, "%sWARN%s %s\n", c.Yellow, c.Reset, w2)
	}
}

func Result(w io.Writer, res *reconcile.Result, colors Colors) {
	c := colors
	if len(res.Created) > 0 {
		fmt.Fprintf(w, "%screated%s:   %s\n", c.Green, c.Reset, strings.Join(res.Created, ", "))
	}
	if len(res.Recreated) > 0 {
		fmt.Fprintf(w, "%srecreated%s: %s\n", c.Yellow, c.Reset, strings.Join(res.Recreated, ", "))
	}
	if len(res.Removed) > 0 {
		fmt.Fprintf(w, "%sremoved%s:   %s\n", c.Red, c.Reset, strings.Join(res.Removed, ", "))
	}
	if len(res.Failed) > 0 {
		fmt.Fprintf(w, "%sfailed%s:    %s\n", c.Red, c.Reset, strings.Join(res.Failed, ", "))
	}
}

// PortEntry is a structured port binding used in StatusEntry.
type PortEntry struct {
	HostIP        string `json:"host_ip,omitempty"    yaml:"host_ip,omitempty"`
	HostPort      string `json:"host_port,omitempty"  yaml:"host_port,omitempty"`
	ContainerPort string `json:"container_port"       yaml:"container_port"`
	Protocol      string `json:"protocol"             yaml:"protocol"`
}

// ResourceLimits holds the formatted resource constraints for a container.
type ResourceLimits struct {
	CPUs   string `json:"cpus,omitempty"   yaml:"cpus,omitempty"`
	Memory string `json:"memory,omitempty" yaml:"memory,omitempty"`
	Pids   int64  `json:"pids,omitempty"   yaml:"pids,omitempty"`
}

// StatusEntry is the unified data model for the status command.
// JSON and YAML output marshal this directly; text output derives display
// strings from the typed fields.
type StatusEntry struct {
	Name          string          `json:"name"                    yaml:"name"`
	ContainerName string          `json:"container_name,omitempty" yaml:"container_name,omitempty"`
	Image         string          `json:"image"                   yaml:"image"`
	ImageDigest   string          `json:"image_digest,omitempty"  yaml:"image_digest,omitempty"`
	ImageSize     string          `json:"image_size,omitempty"    yaml:"image_size,omitempty"`
	State         string          `json:"state"                   yaml:"state"`
	ContainerID   string          `json:"container_id,omitempty"  yaml:"container_id,omitempty"`
	Ports         []PortEntry     `json:"ports"                   yaml:"ports"`
	StartedAt     *time.Time      `json:"started_at,omitempty"    yaml:"started_at,omitempty"`
	RestartCount  int             `json:"restart_count"           yaml:"restart_count"`
	LastRestart   *time.Time      `json:"last_restart,omitempty"  yaml:"last_restart,omitempty"`
	Sync          string          `json:"sync"                    yaml:"sync"`
	ExitCode      *int            `json:"exit_code,omitempty"     yaml:"exit_code,omitempty"`
	Resources     *ResourceLimits `json:"resources,omitempty"     yaml:"resources,omitempty"`
	Note          string          `json:"note,omitempty"          yaml:"note,omitempty"`
}

func Status(w io.Writer, entries []StatusEntry, format Format, colors Colors) {
	switch format {
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(entries)
	case FormatYAML:
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		_ = enc.Encode(entries)
	default:
		renderStatusText(w, entries, colors)
	}
}

func renderStatusText(w io.Writer, entries []StatusEntry, colors Colors) {
	// compute dynamic column widths from data
	nameW, imageW, portsW, restartsW := len("NAME"), len("IMAGE"), len("PORTS"), len("RESTARTS")
	for _, e := range entries {
		if n := len(e.Name); n > nameW {
			nameW = n
		}
		if n := len(e.Image); n > imageW {
			imageW = n
		}
		if n := len(textPorts(e.Ports)); n > portsW {
			portsW = n
		}
		if n := len(textRestarts(e.RestartCount, e.LastRestart)); n > restartsW {
			restartsW = n
		}
	}

	const stateW, uptimeW, syncW = 14, 10, 5
	c := colors

	// column order: NAME  IMAGE  STATE  PORTS  UPTIME  RESTARTS  SYNC  NOTE
	headerLine := fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %s",
		nameW, "NAME", imageW, "IMAGE", stateW, "STATE", portsW, "PORTS",
		uptimeW, "UPTIME", restartsW, "RESTARTS", syncW, "SYNC", "NOTE")
	fmt.Fprintln(w, headerLine)
	fmt.Fprintln(w, strings.Repeat("-", len(headerLine)))

	for _, e := range entries {
		stateColor := ""
		switch e.State {
		case "running":
			stateColor = c.Green
		case "disabled", "declared-off":
			stateColor = c.Cyan
		case "missing":
			stateColor = c.Yellow
		case "stopped", "exited":
			stateColor = c.Yellow
		}
		syncColor := ""
		if e.Sync == "drift" {
			syncColor = c.Yellow
		}

		uptime := "-"
		if e.StartedAt != nil {
			uptime = FormatUptime(*e.StartedAt)
		}

		fmt.Fprintf(w, "%-*s  %-*s  %s%-*s%s  %-*s  %-*s  %-*s  %s%-*s%s  %s\n",
			nameW, e.Name,
			imageW, e.Image,
			stateColor, stateW, e.State, c.Reset,
			portsW, textPorts(e.Ports),
			uptimeW, uptime,
			restartsW, textRestarts(e.RestartCount, e.LastRestart),
			syncColor, syncW, e.Sync, c.Reset,
			e.Note)
	}
}

// textPorts formats a []PortEntry into the compact string used by the text table.
func textPorts(ports []PortEntry) string {
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		var s string
		if p.HostPort == "" {
			s = p.ContainerPort + "/" + p.Protocol
		} else if p.HostIP != "" {
			s = p.HostIP + ":" + p.HostPort + ":" + p.ContainerPort
			if p.Protocol != "tcp" {
				s += "/" + p.Protocol
			}
		} else {
			s = p.HostPort + ":" + p.ContainerPort
			if p.Protocol != "tcp" {
				s += "/" + p.Protocol
			}
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

// textRestarts formats restart count + last restart time for the text table.
func textRestarts(count int, lastRestart *time.Time) string {
	if count == 0 {
		return "0"
	}
	if lastRestart == nil || lastRestart.IsZero() {
		return fmt.Sprintf("%d", count)
	}
	return fmt.Sprintf("%d (%s)", count, FormatUptime(*lastRestart))
}

func FormatUptime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	if d < 0 {
		return "-"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}
