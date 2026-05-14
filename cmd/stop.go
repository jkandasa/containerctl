package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	rt "github.com/jkandasa/containerctl/internal/runtime"
)

var flagStopAll bool

var stopCmd = &cobra.Command{
	Use:   "stop [name...]",
	Short: "Transient stop — container kept on disk; next apply restarts it",
	Args:  cobra.ArbitraryArgs,
	RunE:  runStop,
}

func init() {
	rootCmd.AddCommand(stopCmd)
	stopCmd.Flags().BoolVar(&flagStopAll, "all", false, "stop all managed containers in the project")
}

func runStop(cmd *cobra.Command, args []string) error {
	if !flagStopAll && len(args) == 0 {
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
	if flagStopAll {
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
		fullName := config.ContainerFullName(stack.Project, name)
		info, err := runtime.InspectContainer(ctx, fullName)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", name, err)
		}
		if info == nil {
			fmt.Printf("  %s: not found\n", name)
			continue
		}
		if info.Labels[rt.LabelManaged] != "true" {
			return fmt.Errorf("%s is not managed by containerctl", name)
		}
		fmt.Printf("  %-20s stopping...\n", name)
		if err := runtime.StopContainer(ctx, info.ID, 10*time.Second); err != nil {
			fmt.Printf("  %-20s failed: %v\n", name, err)
			return fmt.Errorf("stop %s: %w", name, err)
		}
		fmt.Printf("  %-20s stopped\n", name)
	}
	return nil
}
