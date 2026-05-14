package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	rt "github.com/jkandasa/containerctl/internal/runtime"
)

var (
	flagLogsFollow bool
	flagLogsTail   int
)

var logsCmd = &cobra.Command{
	Use:   "logs <name>",
	Short: "Stream container logs",
	Args:  cobra.ExactArgs(1),
	RunE:  runLogs,
}

func init() {
	rootCmd.AddCommand(logsCmd)
	logsCmd.Flags().BoolVar(&flagLogsFollow, "follow", false, "follow log output")
	logsCmd.Flags().IntVar(&flagLogsTail, "tail", 0, "number of lines from end to show (0 = all)")
}

func runLogs(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	name := args[0]

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

	fullName := config.ContainerFullName(stack.Project, name)
	info, err := runtime.InspectContainer(ctx, fullName)
	if err != nil {
		return err
	}
	if info == nil {
		return fmt.Errorf("container %q not found", name)
	}

	rc, err := runtime.Logs(ctx, info.ID, rt.LogOptions{
		Follow: flagLogsFollow,
		Tail:   flagLogsTail,
	})
	if err != nil {
		return err
	}
	defer rc.Close()

	_, err = io.Copy(os.Stdout, rc)
	return err
}
