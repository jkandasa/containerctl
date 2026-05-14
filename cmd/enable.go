package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	"github.com/jkandasa/containerctl/internal/reconcile"
	"github.com/jkandasa/containerctl/internal/render"
	"github.com/jkandasa/containerctl/internal/state"
)

var enableCmd = &cobra.Command{
	Use:   "enable <name...>",
	Short: "Remove from disabled state and reconcile (start or recreate as needed)",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runEnable,
}

func init() {
	rootCmd.AddCommand(enableCmd)
}

func runEnable(cmd *cobra.Command, args []string) error {
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

		if plan.HasChanges() {
			for _, e := range reconcile.PullImages(ctx, plan, runtime, 2) {
				fmt.Fprintf(os.Stderr, "WARN: %v\n", e)
			}
			fmt.Println()
			if _, err := reconcile.Apply(ctx, plan, runtime, os.Stdout); err != nil {
				return err
			}
		}
		fmt.Printf("enabled %s\n", name)
	}
	return nil
}
