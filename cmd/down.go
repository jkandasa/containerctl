package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	rt "github.com/jkandasa/containerctl/internal/runtime"
	"github.com/jkandasa/containerctl/internal/config"
)

var downCmd = &cobra.Command{
	Use:   "down [name...]",
	Short: "Stop and remove managed containers (all if no names given)",
	RunE:  runDown,
}

func init() {
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

	nameSet := make(map[string]bool, len(args))
	for _, a := range args {
		nameSet[a] = true
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
		if len(nameSet) > 0 && !nameSet[lname] {
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
