package cmd

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	"github.com/jkandasa/containerctl/internal/render"
	rt "github.com/jkandasa/containerctl/internal/runtime"
	"github.com/jkandasa/containerctl/internal/state"
)

var statusCmd = &cobra.Command{
	Use:   "status [name...]",
	Short: "Show state and drift of all managed containers",
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
	runtimeName := stack.Runtime
	if flagRuntime != "" {
		runtimeName = flagRuntime
	}

	runtime, err := newRuntime(runtimeName)
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

	var rows []render.StatusRow
	for _, c := range stack.Containers {
		if len(filterSet) > 0 && !filterSet[c.Name] {
			continue
		}
		row := render.StatusRow{Name: c.Name, Image: c.Image, Uptime: "-", Drift: "-"}

		if c.Disabled {
			row.State = "declared-off"
			row.Note = "disabled: true in YAML"
			rows = append(rows, row)
			continue
		}

		live, exists := liveByName[c.Name]
		if !exists {
			if st.IsDisabled(c.Name) {
				row.State = "disabled"
				row.Note = "disabled via state file (container not on host)"
			} else {
				row.State = "missing"
				row.Note = "apply will create"
			}
			rows = append(rows, row)
			continue
		}

		if st.IsDisabled(c.Name) {
			row.State = "disabled"
			row.Image = live.Image
			row.Uptime = render.FormatUptime(live.StartedAt)
			row.Note = "disabled via state file"
			rows = append(rows, row)
			continue
		}

		row.State = live.State
		row.Image = live.Image
		row.Uptime = render.FormatUptime(live.StartedAt)

		expectedHash := config.Hash(&c)
		if live.Labels[rt.LabelConfigHash] == expectedHash {
			row.Drift = "no"
		} else {
			row.Drift = "yes"
		}
		rows = append(rows, row)
	}

	// also show managed containers not in YAML (orphans)
	declaredNames := make(map[string]bool, len(stack.Containers))
	for _, c := range stack.Containers {
		declaredNames[c.Name] = true
	}
	for name, c := range liveByName {
		if declaredNames[name] {
			continue
		}
		if len(filterSet) > 0 && !filterSet[name] {
			continue
		}
		rows = append(rows, render.StatusRow{
			Name:   name,
			State:  c.State,
			Image:  c.Image,
			Uptime: render.FormatUptime(c.StartedAt),
			Drift:  "-",
			Note:   "not in stack.yaml (orphan)",
		})
	}

	render.Status(os.Stdout, rows, render.Format(flagOutput), colors())
	return nil
}
