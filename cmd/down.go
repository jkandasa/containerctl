package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	rt "github.com/jkandasa/containerctl/internal/runtime"
)

var downCmd = &cobra.Command{
	Use:   "down [name...]",
	Short: "Stop and remove managed containers (all if no names given)",
	RunE:  runDown,
}

func init() {
	addLabelFlag(downCmd)
	rootCmd.AddCommand(downCmd)
}

func runDown(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	stack, err := config.Load(flagFile)
	if err != nil {
		return err
	}
	if flagProject != "" {
		stack.Project = flagProject
	}

	selected, filtered, err := selectContainerNames(stack, args, flagLabels, false, false, false)
	if err != nil {
		return err
	}
	nameSet := make(map[string]bool, len(selected))
	for _, n := range selected {
		nameSet[n] = true
	}

	runtime, err := runtimeFrom(stack)
	if err != nil {
		return err
	}
	defer runtime.Close()

	if err := pingRuntime(ctx, runtime); err != nil {
		return err
	}

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
		lname := c.Labels[rt.LabelName]
		if filtered && !nameSet[lname] {
			continue
		}
		fmt.Printf("Stopping %s...\n", lname)
		if err := runtime.StopContainer(ctx, c.ID, 10*time.Second); err != nil {
			fmt.Printf("  warn: stop %s: %v\n", lname, err)
		}
		if err := runtime.RemoveContainer(ctx, c.ID, false); err != nil {
			fmt.Printf("  warn: remove %s: %v\n", lname, err)
		}
	}
	return nil
}
