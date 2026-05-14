package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	"github.com/jkandasa/containerctl/internal/reconcile"
	rt "github.com/jkandasa/containerctl/internal/runtime"
)

var flagRestartAll bool

var restartCmd = &cobra.Command{
	Use:   "restart [name...]",
	Short: "Stop, remove, recreate, and start containers from current config",
	Args:  cobra.ArbitraryArgs,
	RunE:  runRestart,
}

func init() {
	rootCmd.AddCommand(restartCmd)
	restartCmd.Flags().BoolVar(&flagRestartAll, "all", false, "restart all managed containers in the project")
}

func runRestart(cmd *cobra.Command, args []string) error {
	if !flagRestartAll && len(args) == 0 {
		return fmt.Errorf("specify at least one container name, or use --all")
	}

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

	names := args
	if flagRestartAll {
		ctrs, err := runtime.ListContainers(ctx, rt.Filters{
			Labels: map[string]string{
				rt.LabelManaged: "true",
				rt.LabelProject: stack.Project,
			},
		})
		if err != nil {
			return err
		}
		for _, c := range ctrs {
			names = append(names, c.Labels[rt.LabelName])
		}
	}

	for _, name := range names {
		c := stack.ContainerByName(name)
		if c == nil {
			return fmt.Errorf("container %q not found in %s", name, flagFile)
		}

		fullName := config.ContainerFullName(stack.Project, name)
		info, err := runtime.InspectContainer(ctx, fullName)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", name, err)
		}
		if info == nil {
			return fmt.Errorf("%s: not found (run apply to create it first)", name)
		}
		if info.Labels[rt.LabelManaged] != "true" {
			return fmt.Errorf("%s is not managed by containerctl", name)
		}

		fmt.Printf("  %-20s stopping...\n", name)
		if err := runtime.StopContainer(ctx, info.ID, 10*time.Second); err != nil {
			return fmt.Errorf("stop %s: %w", name, err)
		}
		if err := runtime.RemoveContainer(ctx, info.ID, false); err != nil {
			return fmt.Errorf("remove %s: %w", name, err)
		}

		spec, err := reconcile.ContainerSpecFrom(stack.Project, c)
		if err != nil {
			return err
		}

		fmt.Printf("  %-20s creating...\n", name)
		id, err := runtime.CreateContainer(ctx, spec)
		if err != nil {
			return fmt.Errorf("create %s: %w", name, err)
		}
		if err := runtime.StartContainer(ctx, id); err != nil {
			return fmt.Errorf("start %s: %w", name, err)
		}
		fmt.Printf("  %-20s restarted → running\n", name)
	}
	return nil
}
