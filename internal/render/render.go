package render

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/jkandasa/containerctl/internal/reconcile"
)

type Format string

const (
	FormatConsole Format = "console"
	FormatJSON    Format = "json"
	FormatYAML    Format = "yaml"
)

type Colors struct {
	Reset       string
	Green       string
	Yellow      string
	Red         string
	Cyan        string
	Blue        string
	Magenta     string
	Gray        string
	Punctuation string // Used for brackets, colons, commas etc. in JSON/YAML
}

func NoColors() Colors { return Colors{} }

func ANSIColors() Colors {
	return Colors{
		Reset:       "\033[0m",
		Green:       "\033[32m",
		Yellow:      "\033[33m",
		Red:         "\033[31m",
		Cyan:        "\033[36m",
		Blue:        "\033[34m",
		Magenta:     "\033[35m",
		Gray:        "\033[90m",
		Punctuation: "\033[2;37m", // Dim white — less dull than pure gray for structure
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

// NetworkEntry is a network attachment for a container.
type NetworkEntry struct {
	Name      string `json:"name"                 yaml:"name"`
	IPAddress string `json:"ip_address,omitempty" yaml:"ip_address,omitempty"`
	Gateway   string `json:"gateway,omitempty"    yaml:"gateway,omitempty"`
}

// MountEntry is a mount point for a container.
type MountEntry struct {
	Type        string `json:"type"                yaml:"type"`
	Name        string `json:"name,omitempty"      yaml:"name,omitempty"`
	Source      string `json:"source"              yaml:"source"`
	Destination string `json:"destination"         yaml:"destination"`
	ReadOnly    bool   `json:"read_only,omitempty" yaml:"read_only,omitempty"`
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
	Name            string          `json:"name"                       yaml:"name"`
	ContainerName   string          `json:"container_name,omitempty"   yaml:"container_name,omitempty"`
	Image           string          `json:"image"                      yaml:"image"`
	ImageDigest     string          `json:"image_digest,omitempty"     yaml:"image_digest,omitempty"`
	ImageSize       string          `json:"image_size,omitempty"       yaml:"image_size,omitempty"`
	State           string          `json:"state"                      yaml:"state"`
	ContainerID     string          `json:"container_id,omitempty"     yaml:"container_id,omitempty"`
	Ports           []PortEntry     `json:"ports"                      yaml:"ports"`
	Networks        []NetworkEntry  `json:"networks,omitempty"         yaml:"networks,omitempty"`
	Mounts          []MountEntry    `json:"mounts,omitempty"           yaml:"mounts,omitempty"`
	StartedAt       *time.Time      `json:"started_at,omitempty"       yaml:"started_at,omitempty"`
	RestartCount    int             `json:"restart_count"              yaml:"restart_count"`
	LastRestart     *time.Time      `json:"last_restart,omitempty"     yaml:"last_restart,omitempty"`
	Sync            string          `json:"sync"                       yaml:"sync"`
	ExitCode        *int            `json:"exit_code,omitempty"        yaml:"exit_code,omitempty"`
	Resources       *ResourceLimits `json:"resources,omitempty"        yaml:"resources,omitempty"`
	CPUPercent      *float64        `json:"cpu_percent,omitempty"      yaml:"cpu_percent,omitempty"`
	MemoryUsedBytes int64           `json:"memory_used_bytes,omitempty" yaml:"memory_used_bytes,omitempty"`
	MemoryUsed      string          `json:"memory_used,omitempty"      yaml:"memory_used,omitempty"`
	Note            string          `json:"note,omitempty"             yaml:"note,omitempty"`
}

func Status(w io.Writer, entries []StatusEntry, format Format, colors Colors) {
	switch format {
	case FormatJSON:
		_ = JSON(w, entries, colors)
	case FormatYAML:
		_ = YAML(w, entries, colors)
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

	// only show CPU/MEM columns when at least one entry has stats data
	hasStats := false
	for _, e := range entries {
		if e.CPUPercent != nil {
			hasStats = true
			break
		}
	}

	const stateW, uptimeW, syncW, cpuW, memW = 14, 10, 5, 7, 10
	c := colors

	var headerLine string
	if hasStats {
		headerLine = fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %s",
			nameW, "NAME", imageW, "IMAGE", stateW, "STATE", portsW, "PORTS",
			uptimeW, "UPTIME", restartsW, "RESTARTS", cpuW, "CPU", memW, "MEM", syncW, "SYNC", "NOTE")
	} else {
		headerLine = fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %s",
			nameW, "NAME", imageW, "IMAGE", stateW, "STATE", portsW, "PORTS",
			uptimeW, "UPTIME", restartsW, "RESTARTS", syncW, "SYNC", "NOTE")
	}
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

		if hasStats {
			cpu := "-"
			if e.CPUPercent != nil {
				cpu = fmt.Sprintf("%.2f%%", *e.CPUPercent)
			}
			mem := "-"
			if e.MemoryUsed != "" {
				mem = e.MemoryUsed
			}
			fmt.Fprintf(w, "%-*s  %-*s  %s%-*s%s  %-*s  %-*s  %-*s  %-*s  %-*s  %s%-*s%s  %s\n",
				nameW, e.Name,
				imageW, e.Image,
				stateColor, stateW, e.State, c.Reset,
				portsW, textPorts(e.Ports),
				uptimeW, uptime,
				restartsW, textRestarts(e.RestartCount, e.LastRestart),
				cpuW, cpu,
				memW, mem,
				syncColor, syncW, e.Sync, c.Reset,
				e.Note)
		} else {
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

// JSON writes the value as pretty-printed JSON.
// When colors are enabled, it applies syntax highlighting for keys, strings,
// numbers, booleans, and null values.
func JSON(w io.Writer, v any, colors Colors) error {
	// Always marshal to JSON first, then unmarshal to a generic structure.
	// This ensures colorJSON always receives map[string]any / []any / primitives,
	// so coloring works reliably for both custom structs and maps.
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}

	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return err
	}

	if colors.Reset == "" {
		// No colors — just pretty print the generic form
		b, err := json.MarshalIndent(generic, "", "  ")
		if err != nil {
			return err
		}
		_, err = w.Write(append(b, '\n'))
		return err
	}

	colored := colorJSON(generic, colors)
	_, err = w.Write(colored)
	return err
}

// colorJSON builds colored pretty JSON by walking the value directly.
// This is the reliable way (avoids fragile regex post-processing on ANSI strings).
func colorJSON(v any, c Colors) []byte {
	if c.Reset == "" {
		b, _ := json.MarshalIndent(v, "", "  ")
		return append(b, '\n')
	}

	var buf strings.Builder
	writeColoredJSON(&buf, v, c, 0)
	buf.WriteByte('\n')
	return []byte(buf.String())
}

func writeColoredJSON(buf *strings.Builder, v any, c Colors, indent int) {
	indentStr := strings.Repeat("  ", indent)

	switch val := v.(type) {
	case map[string]any:
		if len(val) == 0 {
			buf.WriteString(c.Punctuation + "{}" + c.Reset)
			return
		}
		buf.WriteString(c.Punctuation + "{" + c.Reset + "\n")
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			buf.WriteString(indentStr + "  ")
			buf.WriteString(c.Cyan + `"` + k + `"` + c.Reset) // key
			buf.WriteString(c.Punctuation + ":" + c.Reset + " ")
			writeColoredJSON(buf, val[k], c, indent+1)
			if i < len(keys)-1 {
				buf.WriteString(c.Punctuation + "," + c.Reset)
			}
			buf.WriteString("\n")
		}
		buf.WriteString(c.Punctuation + "}" + c.Reset)

	case []any:
		if len(val) == 0 {
			buf.WriteString(c.Punctuation + "[]" + c.Reset)
			return
		}
		buf.WriteString(c.Punctuation + "[" + c.Reset + "\n")
		for i, item := range val {
			buf.WriteString(indentStr + "  ")
			writeColoredJSON(buf, item, c, indent+1)
			if i < len(val)-1 {
				buf.WriteString(c.Punctuation + "," + c.Reset)
			}
			buf.WriteString("\n")
		}
		buf.WriteString(c.Punctuation + "]" + c.Reset)

	case string:
		buf.WriteString(c.Green + `"` + val + `"` + c.Reset)

	case float64:
		buf.WriteString(c.Yellow + fmt.Sprintf("%v", val) + c.Reset)

	case bool:
		buf.WriteString(c.Magenta + fmt.Sprintf("%v", val) + c.Reset)

	case nil:
		buf.WriteString(c.Magenta + "null" + c.Reset)

	default:
		// fallback
		b, _ := json.Marshal(val)
		buf.Write(b)
	}
}

// YAML writes the value as pretty-printed YAML.
// When colors are enabled, it applies syntax highlighting by walking the YAML AST.
// This is consistent with the JSON implementation and much more reliable than regex.
func YAML(w io.Writer, v any, colors Colors) error {
	if colors.Reset == "" {
		// Fast path - no colors
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		return enc.Encode(v)
	}

	// Marshal to YAML nodes so we can walk the AST semantically
	node := &yaml.Node{}
	if err := node.Encode(v); err != nil {
		return err
	}

	var buf strings.Builder
	writeColoredYAML(&buf, node, colors, 0)
	_, err := io.WriteString(w, buf.String())
	return err
}

// writeColoredYAML walks the YAML AST and writes colored output.
// This is the proper way to do YAML syntax highlighting (much better than regex on text).
func writeColoredYAML(buf *strings.Builder, node *yaml.Node, c Colors, indent int) {
	if node == nil {
		return
	}

	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			writeColoredYAML(buf, child, c, indent)
		}

	case yaml.MappingNode:
		// Mapping nodes come in pairs: key, value, key, value...
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			value := node.Content[i+1]

			// Head comment before the key (e.g. above a field)
			if key.HeadComment != "" {
				buf.WriteString(strings.Repeat("  ", indent))
				buf.WriteString(c.Gray + key.HeadComment + c.Reset + "\n")
			}

			// Write indentation + key (colored as key)
			buf.WriteString(strings.Repeat("  ", indent))
			writeScalar(buf, key, c, true)

			// Write ": "
			buf.WriteString(": ")

			// Write value
			if value.Kind == yaml.ScalarNode || value.Kind == yaml.AliasNode {
				writeScalar(buf, value, c, false)
				buf.WriteString("\n")
			} else {
				buf.WriteString("\n")
				writeColoredYAML(buf, value, c, indent+1)
			}
		}

		if node.FootComment != "" {
			buf.WriteString(strings.Repeat("  ", indent))
			buf.WriteString(c.Gray + node.FootComment + c.Reset + "\n")
		}

	case yaml.SequenceNode:
		for _, item := range node.Content {
			buf.WriteString(strings.Repeat("  ", indent) + "- ")

			if item.Kind == yaml.ScalarNode || item.Kind == yaml.AliasNode {
				writeScalar(buf, item, c, false)
				buf.WriteString("\n")
			} else {
				buf.WriteString("\n")
				writeColoredYAML(buf, item, c, indent+1)
			}
		}

		if node.FootComment != "" {
			buf.WriteString(strings.Repeat("  ", indent))
			buf.WriteString(c.Gray + node.FootComment + c.Reset + "\n")
		}

	case yaml.ScalarNode:
		writeScalar(buf, node, c, false)
		buf.WriteString("\n")

	default:
		// Fallback for aliases etc.
		buf.WriteString(strings.Repeat("  ", indent))
		writeScalar(buf, node, c, false)
		buf.WriteString("\n")
	}
}

// writeScalar writes a scalar node with appropriate coloring.
// For strings, we only add quotes when YAML would require them (to keep output natural).
func writeScalar(buf *strings.Builder, node *yaml.Node, c Colors, isKey bool) {
	value := node.Value

	if isKey {
		buf.WriteString(c.Cyan + value + c.Reset)
		return
	}

	switch node.Tag {
	case "!!str":
		if yamlNeedsQuoting(value) {
			// Escape double quotes inside the string
			escaped := strings.ReplaceAll(value, `"`, `\"`)
			buf.WriteString(c.Green + `"` + escaped + `"` + c.Reset)
		} else {
			buf.WriteString(c.Green + value + c.Reset)
		}

	case "!!int", "!!float":
		buf.WriteString(c.Yellow + value + c.Reset)

	case "!!bool":
		buf.WriteString(c.Magenta + value + c.Reset)

	case "!!null":
		buf.WriteString(c.Magenta + "null" + c.Reset)

	case "!!timestamp":
		// Color timestamps like numbers for good UX
		buf.WriteString(c.Yellow + value + c.Reset)

	default:
		buf.WriteString(value)
	}

	// Append inline comment if present
	if node.LineComment != "" {
		buf.WriteString(" " + c.Gray + node.LineComment + c.Reset)
	}
}

// yamlNeedsQuoting returns whether a string value needs to be quoted in YAML output.
// This is a lightweight heuristic that covers the most common cases.
func yamlNeedsQuoting(s string) bool {
	if s == "" {
		return true
	}

	// Must quote if it contains characters that have special meaning or would be ambiguous
	if strings.ContainsAny(s, ":{}[]&*!|>'\"%@`") ||
		strings.HasPrefix(s, "- ") ||
		strings.HasPrefix(s, "? ") ||
		strings.Contains(s, "\n") ||
		strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") {
		return true
	}

	// Quote if it would be parsed as a number, boolean, or null
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return true
	}

	lower := strings.ToLower(s)
	switch lower {
	case "true", "false", "null", "~", "yes", "no", "on", "off":
		return true
	}

	return false
}
