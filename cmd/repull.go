package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	"github.com/jkandasa/containerctl/internal/reconcile"
)

var flagRepullFollow bool

var repullCmd = &cobra.Command{
	Use:   "repull <name>",
	Short: "Force-pull the image and recreate a container, bypassing the config hash",
	Args:  cobra.ExactArgs(1),
	RunE:  runRepull,
}

func init() {
	rootCmd.AddCommand(repullCmd)
	repullCmd.Flags().BoolVar(&flagRepullFollow, "follow", false, "follow log output after recreating")
}

func runRepull(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	name := args[0]

	stack, err := config.Load(flagFile)
	if err != nil {
		return err
	}
	if flagProject != "" {
		stack.Project = flagProject
	}

	c := stack.ContainerByName(name)
	if c == nil {
		return fmt.Errorf("container %q not found in %s", name, flagFile)
	}

	rt, err := runtimeFrom(stack)
	if err != nil {
		return err
	}
	defer rt.Close()

	if err := pingRuntime(ctx, rt); err != nil {
		return err
	}
	applyAuthFile(rt, stack.AuthFile)

	fmt.Printf("Pulling %s...\n", c.Image)
	if err := rt.Pull(ctx, c.Image); err != nil {
		return fmt.Errorf("pull: %w", err)
	}

	fullName := config.ContainerFullName(stack.Project, name)
	existing, err := rt.InspectContainer(ctx, fullName)
	if err != nil {
		return err
	}
	if existing != nil {
		fmt.Printf("Stopping %s...\n", name)
		if err := rt.StopContainer(ctx, existing.ID, 10*time.Second); err != nil {
			return fmt.Errorf("stop: %w", err)
		}
		if err := rt.RemoveContainer(ctx, existing.ID, false); err != nil {
			return fmt.Errorf("remove: %w", err)
		}
	}

	fmt.Printf("Creating %s...\n", name)
	spec, err := reconcile.ContainerSpecFrom(stack.Project, c)
	if err != nil {
		return err
	}
	id, err := rt.CreateContainer(ctx, spec)
	if err != nil {
		return err
	}
	if err := rt.StartContainer(ctx, id); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	fmt.Fprintf(os.Stdout, "upgraded %s\n", name)
	if flagRepullFollow {
		return followLogsAfter(rt, stack, name, true, "")
	}
	return nil
}
