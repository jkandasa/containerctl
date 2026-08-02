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
	Use:   "stop <name...> | --all | -l selector",
	Short: "Stop containers; they stay on disk and restart on next apply",
	Args:  cobra.ArbitraryArgs,
	RunE:  runStop,
}

func init() {
	rootCmd.AddCommand(stopCmd)
	stopCmd.Flags().BoolVar(&flagStopAll, "all", false, "stop all managed containers in the project")
	addLabelFlag(stopCmd)
}

func runStop(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	stack, err := config.Load(flagFile)
	if err != nil {
		return err
	}
	if flagProject != "" {
		stack.Project = flagProject
	}

	names, _, err := selectContainerNames(stack, args, flagLabels, true, flagStopAll, false)
	if err != nil {
		return err
	}

	runtime, err := runtimeFrom(stack)
	if err != nil {
		return err
	}
	defer runtime.Close()

	if err := pingRuntime(ctx, runtime); err != nil {
		return err
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
