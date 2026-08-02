package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	rt "github.com/jkandasa/containerctl/internal/runtime"
	"github.com/jkandasa/containerctl/internal/state"
)

var (
	flagStartAll    bool
	flagStartFollow bool
)

var startCmd = &cobra.Command{
	Use:   "start <name...> | --all | -l selector",
	Short: "Start stopped containers without reconciling",
	Args:  cobra.ArbitraryArgs,
	RunE:  runStart,
}

func init() {
	rootCmd.AddCommand(startCmd)
	startCmd.Flags().BoolVar(&flagStartAll, "all", false, "start all managed containers in the project")
	startCmd.Flags().BoolVar(&flagStartFollow, "follow", false, "follow log output after starting (single container only)")
	addLabelFlag(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	stack, err := config.Load(flagFile)
	if err != nil {
		return err
	}
	if flagProject != "" {
		stack.Project = flagProject
	}

	names, _, err := selectContainerNames(stack, args, flagLabels, true, flagStartAll, false)
	if err != nil {
		return err
	}
	if flagStartFollow && len(names) != 1 {
		return fmt.Errorf("--follow requires exactly one container name")
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

	for _, name := range names {
		if st.IsDisabled(name) {
			fmt.Printf("  %s: skipping (persistently disabled; run 'containerctl enable %s' first)\n", name, name)
			continue
		}
		fullName := config.ContainerFullName(stack.Project, name)
		info, err := runtime.InspectContainer(ctx, fullName)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", name, err)
		}
		if info == nil {
			fmt.Printf("  %s: not found (run apply to create it)\n", name)
			continue
		}
		if info.Labels[rt.LabelManaged] != "true" {
			return fmt.Errorf("%s is not managed by containerctl", name)
		}
		fmt.Printf("  %-20s starting...\n", name)
		if err := runtime.StartContainer(ctx, info.ID); err != nil {
			fmt.Printf("  %-20s failed: %v\n", name, err)
			return fmt.Errorf("start %s: %w", name, err)
		}
		fmt.Printf("  %-20s started   → running\n", name)
	}
	if flagStartFollow {
		return followContainerLogs(ctx, runtime, stack, names[0])
	}
	return nil
}
