package cmd

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	"github.com/jkandasa/containerctl/internal/reconcile"
	"github.com/jkandasa/containerctl/internal/render"
	rt "github.com/jkandasa/containerctl/internal/runtime"
	"github.com/jkandasa/containerctl/internal/state"
)

var flagEnableFollow bool

var enableCmd = &cobra.Command{
	Use:   "enable <name...>",
	Short: "Remove from disabled state and reconcile (start or recreate as needed)",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runEnable,
}

func init() {
	rootCmd.AddCommand(enableCmd)
	enableCmd.Flags().BoolVar(&flagEnableFollow, "follow", false, "follow log output after enabling (single container only)")
}

func runEnable(cmd *cobra.Command, args []string) error {
	if flagEnableFollow && len(args) != 1 {
		return fmt.Errorf("--follow requires exactly one container name")
	}

	ctx := context.Background()

	stack, err := config.Load(flagFile)
	if err != nil {
		return err
	}
	if flagProject != "" {
		stack.Project = flagProject
	}

	st, err := state.Load(stack.Project)
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

	var started bool
	for _, name := range args {
		c := stack.ContainerByName(name)
		if c == nil {
			return fmt.Errorf("container %q not found in %s", name, flagFile)
		}
		if c.Disabled {
			return fmt.Errorf("%s has disabled: true in YAML; remove that field first", name)
		}

		if !st.IsDisabled(name) {
			fmt.Printf("%s is already enabled\n", name)
			continue
		}

		st.Enable(name)
		if err := st.Save(); err != nil {
			return fmt.Errorf("save state: %w", err)
		}

		// reconcile just this container with an empty disabled set
		plan, err := reconcile.Build(ctx, stack, runtime, []string{name}, map[string]bool{})
		if err != nil {
			return err
		}
		for _, w := range plan.Warnings {
			fmt.Printf("WARN: %s\n", w)
		}
		render.Plan(os.Stdout, plan, colors())
		fmt.Println()

		startedThis := false
		if plan.HasChanges() {
			for _, e := range reconcile.PullImages(ctx, plan, runtime, 2) {
				fmt.Fprintf(os.Stderr, "WARN: %v\n", e)
			}
			fmt.Println()
			res, err := reconcile.Apply(ctx, plan, runtime, os.Stdout)
			if err != nil {
				return err
			}
			if slices.Contains(res.Created, name) || slices.Contains(res.Recreated, name) {
				startedThis = true
			}
		}
		// Hash match: planner skips. disable left the container stopped — start it.
		if !startedThis {
			running, err := startStoppedContainer(ctx, runtime, stack, name)
			if err != nil {
				return err
			}
			startedThis = running
		}
		started = startedThis
		fmt.Printf("enabled %s\n", name)
	}
	if flagEnableFollow {
		return followLogsAfter(runtime, stack, args[0], started,
			fmt.Sprintf("%s was not created or started.", args[0]))
	}
	return nil
}

// startStoppedContainer starts name if it exists and is not running.
// Returns true when the container is running afterwards.
func startStoppedContainer(ctx context.Context, runtime rt.Runtime, stack *config.Stack, name string) (bool, error) {
	fullName := config.ContainerFullName(stack.Project, name)
	info, err := runtime.InspectContainer(ctx, fullName)
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", name, err)
	}
	if info == nil {
		return false, nil
	}
	if info.State == "running" {
		return true, nil
	}
	fmt.Printf("  %-20s starting...\n", name)
	if err := runtime.StartContainer(ctx, info.ID); err != nil {
		return false, fmt.Errorf("start %s: %w", name, err)
	}
	fmt.Printf("  %-20s started   → running\n", name)
	return true, nil
}
