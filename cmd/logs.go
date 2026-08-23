package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	rt "github.com/jkandasa/containerctl/internal/runtime"
)

var (
	flagLogsFollow bool
	flagLogsTail   int
	flagLogsClear  bool
	flagLogsInfo   bool
)

var logsCmd = &cobra.Command{
	Use:   "logs <name>",
	Short: "Stream container logs",
	Long: `Stream container logs, or inspect / empty the current log file.

--info prints the engine log path, size, and driver.
--clear truncates the engine log file in place. The container is not stopped
or recreated. Requires a file-backed log driver (json-file, k8s-file). journald
cannot be cleared this way. The process must be able to write the log file
(often root for Docker).`,
	Args: cobra.ExactArgs(1),
	RunE: runLogs,
}

func init() {
	rootCmd.AddCommand(logsCmd)
	logsCmd.Flags().BoolVar(&flagLogsFollow, "follow", false, "follow log output")
	logsCmd.Flags().IntVar(&flagLogsTail, "tail", -1, "lines from the end to show (-1 = all, 0 = none)")
	logsCmd.Flags().BoolVar(&flagLogsClear, "clear", false, "empty the current log file without stopping the container")
	logsCmd.Flags().BoolVar(&flagLogsInfo, "info", false, "print log file path, size, and driver")
}

func followContainerLogs(ctx context.Context, runtime rt.Runtime, stack *config.Stack, name string) error {
	fullName := config.ContainerFullName(stack.Project, name)
	info, err := runtime.InspectContainer(ctx, fullName)
	if err != nil {
		return err
	}
	if info == nil {
		return fmt.Errorf("container %q not found", name)
	}
	rc, err := runtime.Logs(ctx, info.ID, rt.LogOptions{Follow: true, Tail: -1})
	if err != nil {
		return err
	}
	defer rc.Close()
	_, err = io.Copy(os.Stdout, rc)
	if ctx.Err() != nil {
		// The caller's signal handler cancelled us. Ctrl-C is how the operator
		// is told to stop following, so it is a clean exit, not a failure.
		return nil
	}
	return err
}

// followLogsAfter streams logs for name after a mutating command.
// If started is false, it prints notStartedReason and returns nil so --follow
// on a no-op is a clean exit rather than "container not found".
func followLogsAfter(runtime rt.Runtime, stack *config.Stack, name string, started bool, notStartedReason string) error {
	if !started {
		fmt.Printf("\nNot following logs: %s\n", notStartedReason)
		return nil
	}
	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	fmt.Printf("\nFollowing logs for %s (Ctrl-C to stop)...\n", name)
	return followContainerLogs(sigCtx, runtime, stack, name)
}

func runLogs(cmd *cobra.Command, args []string) error {
	if (flagLogsClear || flagLogsInfo) && (flagLogsFollow || cmd.Flags().Changed("tail")) {
		return fmt.Errorf("--clear and --info cannot be used with --follow or --tail")
	}
	if flagLogsClear && flagLogsInfo {
		return fmt.Errorf("--clear and --info cannot be used together")
	}

	ctx := context.Background()
	name := args[0]

	stack, err := config.Load(flagFile)
	if err != nil {
		return err
	}
	if flagProject != "" {
		stack.Project = flagProject
	}

	runtime, err := runtimeFrom(stack)
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

	if flagLogsInfo {
		return printLogInfo(ctx, runtime, name, info.ID)
	}

	if flagLogsClear {
		if err := runtime.TruncateLogs(ctx, info.ID); err != nil {
			return err
		}
		fmt.Printf("cleared logs for %s\n", name)
		return nil
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

func printLogInfo(ctx context.Context, runtime rt.Runtime, name, id string) error {
	meta, err := runtime.LogFileInfo(ctx, id)
	if err != nil {
		return err
	}
	fmt.Printf("NAME      %s\n", name)
	fmt.Printf("DRIVER    %s\n", meta.Driver)
	if meta.Path == "" {
		fmt.Printf("PATH      -\n")
		fmt.Printf("SIZE      -\n")
		return nil
	}
	fmt.Printf("PATH      %s\n", meta.Path)
	if meta.StatErr != nil {
		fmt.Printf("SIZE      (%v)\n", meta.StatErr)
		return nil
	}
	fmt.Printf("SIZE      %s (%d bytes)\n", formatImageSize(meta.Size), meta.Size)
	if !meta.ModTime.IsZero() {
		fmt.Printf("MODIFIED  %s\n", formatAge(meta.ModTime))
	}
	return nil
}
