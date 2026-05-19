package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/moby/term"
	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	rt "github.com/jkandasa/containerctl/internal/runtime"
)

var execCmd = &cobra.Command{
	Use:   "exec <name> [command...]",
	Short: "Run a command in a running container (default: /bin/sh)",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runExec,
}

func init() {
	rootCmd.AddCommand(execCmd)
	execCmd.Flags().SetInterspersed(false)
}

func runExec(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	name := args[0]
	command := args[1:]
	if len(command) == 0 {
		command = []string{"/bin/sh"}
	}

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
	if info.State != "running" {
		return fmt.Errorf("container %q is not running (state: %s)", name, info.State)
	}

	inFd := os.Stdin.Fd()
	isTTY := term.IsTerminal(inFd)

	var savedState *term.State
	if isTTY {
		savedState, err = term.SetRawTerminal(inFd)
		if err != nil {
			return fmt.Errorf("raw terminal: %w", err)
		}
		defer term.RestoreTerminal(inFd, savedState)
	}

	exitCode, err := runtime.Exec(ctx, info.ID, rt.ExecOptions{
		Command:     command,
		Tty:         isTTY,
		Interactive: true,
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		StdinFd:     inFd,
	})
	if err != nil {
		return err
	}
	if exitCode != 0 {
		if savedState != nil {
			term.RestoreTerminal(inFd, savedState)
		}
		os.Exit(exitCode)
	}
	return nil
}
