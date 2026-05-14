package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	rt "github.com/jkandasa/containerctl/internal/runtime"
	"github.com/jkandasa/containerctl/internal/state"
)

var disableCmd = &cobra.Command{
	Use:   "disable <name...>",
	Short: "Persistently stop a container; apply will skip it until 'enable' is run",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runDisable,
}

func init() {
	rootCmd.AddCommand(disableCmd)
}

func runDisable(cmd *cobra.Command, args []string) error {
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

	st, err := state.Load(stack.Project)
	if err != nil {
		return err
	}

	runtime, err := newRuntime(runtimeName)
	if err != nil {
		return err
	}
	defer runtime.Close()

	if err := pingRuntime(ctx, runtime); err != nil {
		return err
	}

	for _, name := range args {
		if stack.ContainerByName(name) == nil {
			return fmt.Errorf("container %q not found in %s", name, flagFile)
		}

		if st.IsDisabled(name) {
			fmt.Printf("%s is already disabled\n", name)
			continue
		}

		st.Disable(name)
		if err := st.Save(); err != nil {
			return fmt.Errorf("save state: %w", err)
		}

		// stop the running container if it exists
		fullName := config.ContainerFullName(stack.Project, name)
		info, err := runtime.InspectContainer(ctx, fullName)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", name, err)
		}
		if info != nil && info.Labels[rt.LabelManaged] != "true" {
			return fmt.Errorf("%s is not managed by containerctl", name)
		}
		if info != nil && info.State == "running" {
			fmt.Printf("Stopping %s...\n", name)
			if err := runtime.StopContainer(ctx, info.ID, 10*time.Second); err != nil {
				return fmt.Errorf("stop %s: %w", name, err)
			}
		}

		p, _ := state.Path(stack.Project)
		fmt.Printf("disabled %s (state saved to %s)\n", name, p)
	}
	return nil
}
