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

var diffCmd = &cobra.Command{
	Use:   "diff [name...]",
	Short: "Show what apply would change without making any changes",
	RunE:  runDiff,
}

func init() {
	rootCmd.AddCommand(diffCmd)
}

func runDiff(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	stack, err := config.Load(flagFile)
	if err != nil {
		return err
	}
	if flagProject != "" {
		stack.Project = flagProject
	}

	rt, err := runtimeFrom(stack)
	if err != nil {
		return err
	}
	defer rt.Close()

	if err := pingRuntime(ctx, rt); err != nil {
		return err
	}

	st, err := state.Load(stack.Project)
	if err != nil {
		return err
	}

	plan, err := reconcile.Build(ctx, stack, rt, args, st.DisabledSet())
	if err != nil {
		return err
	}

	for _, w := range plan.Warnings {
		fmt.Fprintf(os.Stderr, "WARN: %s\n", w)
	}

	render.Plan(os.Stdout, plan, colors())

	if plan.HasChanges() {
		os.Exit(3)
	}
	return nil
}
