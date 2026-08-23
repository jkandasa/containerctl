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
	"github.com/jkandasa/containerctl/internal/state"
)

var (
	flagDryRun      bool
	flagApplyFollow bool
)

var applyCmd = &cobra.Command{
	Use:   "apply [name...]",
	Short: "Reconcile host to desired state defined in the YAML file",
	RunE:  runApply,
}

func init() {
	applyCmd.Flags().BoolVar(&flagDryRun, "dry-run", false, "Show what would change without making any changes (exits 3 if changes pending)")
	applyCmd.Flags().BoolVar(&flagApplyFollow, "follow", false, "follow log output after apply (single container only)")
	addLabelFlag(applyCmd)
	rootCmd.AddCommand(applyCmd)
}

func runApply(cmd *cobra.Command, args []string) error {
	if flagApplyFollow && flagDryRun {
		return fmt.Errorf("--follow cannot be used with --dry-run")
	}

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
	if flagApplyFollow && (!filtered || len(selected) != 1) {
		return fmt.Errorf("--follow requires exactly one selected container (name and/or -l)")
	}

	rt, err := runtimeFrom(stack)
	if err != nil {
		return err
	}
	defer rt.Close()

	if err := pingRuntime(ctx, rt); err != nil {
		return err
	}

	if !flagDryRun {
		applyAuthFile(rt, stack.AuthFile)
	}

	st, err := state.Load(stack.Project)
	if err != nil {
		return err
	}

	// selected is nil for full apply; otherwise partial (names and/or -l).
	plan, err := reconcile.Build(ctx, stack, rt, selected, st.DisabledSet())
	if err != nil {
		return err
	}

	for _, w := range plan.Warnings {
		fmt.Fprintf(os.Stderr, "WARN: %s\n", w)
	}

	render.Plan(os.Stdout, plan, colors())

	if flagDryRun {
		if plan.HasChanges() {
			os.Exit(3)
		}
		return nil
	}

	if !plan.HasChanges() {
		fmt.Println("Nothing to do.")
		if flagApplyFollow {
			return followLogsAfter(rt, stack, selected[0], false,
				fmt.Sprintf("%s was not created or recreated.", selected[0]))
		}
		return nil
	}

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

	if flagApplyFollow {
		target := selected[0]
		started := slices.Contains(res.Created, target) || slices.Contains(res.Recreated, target)
		if err := followLogsAfter(rt, stack, target, started,
			fmt.Sprintf("%s was not created or recreated.", target)); err != nil {
			return err
		}
	}

	if res.HasFailures() {
		os.Exit(2)
	}
	return nil
}
