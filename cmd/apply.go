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

var applyCmd = &cobra.Command{
	Use:   "apply [name...]",
	Short: "Reconcile host to desired state defined in the YAML file",
	RunE:  runApply,
}

func init() {
	rootCmd.AddCommand(applyCmd)
}

func runApply(cmd *cobra.Command, args []string) error {
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
	applyAuthFile(rt, stack.AuthFile)

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

	if !plan.HasChanges() {
		fmt.Println("Nothing to do.")
		return nil
	}

	render.Plan(os.Stdout, plan, colors())
	fmt.Println()

	// pull images for Create/Recreate
	pullErrs := reconcile.PullImages(ctx, plan, rt, 4)
	if len(pullErrs) > 0 {
		for _, e := range pullErrs {
			fmt.Fprintf(os.Stderr, "WARN: %v\n", e)
		}
	}

	fmt.Println()
	res, err := reconcile.Apply(ctx, plan, rt, os.Stdout)
	if err != nil {
		return err
	}

	if res.HasFailures() {
		os.Exit(2)
	}
	return nil
}
