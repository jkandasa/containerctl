package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
)

var pullCmd = &cobra.Command{
	Use:   "pull [name...]",
	Short: "Pull container images without reconciling",
	RunE:  runPull,
}

func init() {
	rootCmd.AddCommand(pullCmd)
}

func runPull(cmd *cobra.Command, args []string) error {
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

	rt, err := newRuntime(runtimeName)
	if err != nil {
		return err
	}
	defer rt.Close()

	if err := pingRuntime(ctx, rt); err != nil {
		return err
	}
	applyAuthFile(rt, stack.AuthFile)

	nameSet := make(map[string]bool, len(args))
	for _, a := range args {
		nameSet[a] = true
	}

	for _, c := range stack.Containers {
		if c.Disabled {
			continue
		}
		if len(nameSet) > 0 && !nameSet[c.Name] {
			continue
		}
		fmt.Printf("Pulling %s (%s)...\n", c.Name, c.Image)
		if err := rt.Pull(ctx, c.Image); err != nil {
			fmt.Printf("  error: %v\n", err)
		}
	}
	return nil
}
