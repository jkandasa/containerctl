package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jkandasa/containerctl/internal/reconcile"
)

type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
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

type StatusRow struct {
	Name     string `json:"name"`
	State    string `json:"state"`
	Image    string `json:"image"`
	Ports    string `json:"ports,omitempty"`
	Uptime   string `json:"uptime"`
	Restarts string `json:"restarts"`
	Sync     string `json:"sync"`
	Note     string `json:"note,omitempty"`
}

func Status(w io.Writer, rows []StatusRow, format Format, colors Colors) {
	if format == FormatJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rows)
		return
	}

	// compute column widths from data
	nameW, imageW, portsW, restartsW := len("NAME"), len("IMAGE"), len("PORTS"), len("RESTARTS")
	for _, r := range rows {
		if len(r.Name) > nameW {
			nameW = len(r.Name)
		}
		if len(r.Image) > imageW {
			imageW = len(r.Image)
		}
		if len(r.Ports) > portsW {
			portsW = len(r.Ports)
		}
		if len(r.Restarts) > restartsW {
			restartsW = len(r.Restarts)
		}
	}

	const stateW, uptimeW, syncW = 14, 10, 5
	c := colors

	// column order: NAME  IMAGE  STATE  PORTS  UPTIME  RESTARTS  DRIFT  NOTE
	headerLine := fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %s",
		nameW, "NAME", imageW, "IMAGE", stateW, "STATE", portsW, "PORTS", uptimeW, "UPTIME", restartsW, "RESTARTS", syncW, "SYNC", "NOTE")
	fmt.Fprintln(w, headerLine)
	fmt.Fprintln(w, strings.Repeat("-", len(headerLine)))

	for _, r := range rows {
		stateColor := ""
		switch r.State {
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
		if r.Sync == "drift" {
			syncColor = c.Yellow
		}
		fmt.Fprintf(w, "%-*s  %-*s  %s%-*s%s  %-*s  %-*s  %-*s  %s%-*s%s  %s\n",
			nameW, r.Name,
			imageW, r.Image,
			stateColor, stateW, r.State, c.Reset,
			portsW, r.Ports,
			uptimeW, r.Uptime,
			restartsW, r.Restarts,
			syncColor, syncW, r.Sync, c.Reset,
			r.Note)
	}
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

