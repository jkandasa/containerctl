package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	"github.com/jkandasa/containerctl/internal/copy"
	rt "github.com/jkandasa/containerctl/internal/runtime"
)

var (
	flagCpArchive    bool
	flagCpFollowLink bool
	flagCpOverwrite  bool
	flagCpQuiet      bool
)

var cpCmd = &cobra.Command{
	Use: `cp [OPTIONS] NAME:SRC_PATH DEST_PATH|-
  containerctl cp [OPTIONS] SRC_PATH|- NAME:DEST_PATH
  containerctl cp [OPTIONS] SRC_NAME:SRC_PATH DEST_NAME:DEST_PATH`,
	Short: "Copy files between a container and the host, or between containers",
	Long: `Copy files and directories between a managed container and the local
filesystem, or from one managed container to another.

NAME:PATH uses the logical container name from the stack (not the runtime
name <project>_<name>). Use "-" to stream a tar archive to stdout or from
stdin. Copies between two different containers stream a tar through the
engine. A copy within the same container is fully read first (a temp
file), then written. The engine deadlocks if both happen at once.

  containerctl cp postgres:/var/lib/postgresql/data/dump.sql ./dump.sql
  containerctl cp ./config.yaml app:/etc/app/config.yaml
  containerctl cp postgres:/dump.sql app:/backup/dump.sql
  containerctl cp -L app:/current ./current
  containerctl cp -a ./data app:/var/lib/data
  containerctl cp app:/etc/nginx/nginx.conf -
  containerctl cp - app:/restore < backup.tar

Works on running or stopped containers.

--archive preserves uid/gid when copying into a container.
--follow-link follows a symlink at SRC_PATH (not every symlink in a tree).
--overwrite allows replacing a directory with a file (and vice versa) in
the container.
--quiet suppresses the success summary.`,
	Args:                  cobra.ExactArgs(2),
	DisableFlagsInUseLine: true,
	RunE:                  runCp,
}

func init() {
	rootCmd.AddCommand(cpCmd)
	cpCmd.Flags().BoolVarP(&flagCpArchive, "archive", "a", false, "preserve uid/gid when copying into a container")
	cpCmd.Flags().BoolVarP(&flagCpFollowLink, "follow-link", "L", false, "follow symlinks in SRC_PATH")
	cpCmd.Flags().BoolVar(&flagCpOverwrite, "overwrite", false, "allow overwriting a directory with a file (and vice versa)")
	cpCmd.Flags().BoolVarP(&flagCpQuiet, "quiet", "q", false, "do not print the success summary")
}

func runCp(cmd *cobra.Command, args []string) error {
	srcSpec, destSpec, dir, err := copy.Resolve(args[0], args[1])
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	opts := copy.Options{
		FollowLink: flagCpFollowLink,
		Archive:    flagCpArchive,
		Overwrite:  flagCpOverwrite,
	}

	var (
		n          int64
		logical    string
		printedDst = args[1]
	)

	switch dir {
	case copy.DirFromContainer:
		logical = srcSpec.Container
		id, err := resolveCopyContainer(ctx, runtime, stack, logical)
		if err != nil {
			return err
		}
		n, err = copy.FromContainer(ctx, runtime, id, srcSpec.Path, destSpec.Path, opts, os.Stdout)
		if err != nil {
			return err
		}
	case copy.DirToContainer:
		logical = destSpec.Container
		id, err := resolveCopyContainer(ctx, runtime, stack, logical)
		if err != nil {
			return err
		}
		n, err = copy.ToContainer(ctx, runtime, id, srcSpec.Path, destSpec.Path, opts, os.Stdin)
		if err != nil {
			return err
		}
	case copy.DirAcrossContainers:
		srcID, err := resolveCopyContainer(ctx, runtime, stack, srcSpec.Container)
		if err != nil {
			return err
		}
		destID, err := resolveCopyContainer(ctx, runtime, stack, destSpec.Container)
		if err != nil {
			return err
		}
		n, err = copy.BetweenContainers(ctx, runtime, srcID, srcSpec.Path, destID, destSpec.Path, opts)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("internal error: unknown copy direction")
	}

	if flagCpQuiet || srcSpec.Stdio() || destSpec.Stdio() {
		return nil
	}
	fmt.Printf("copied %s to %s\n", formatImageSize(n), printedDst)
	return nil
}

func resolveCopyContainer(ctx context.Context, runtime rt.Runtime, stack *config.Stack, name string) (string, error) {
	fullName := config.ContainerFullName(stack.Project, name)
	info, err := runtime.InspectContainer(ctx, fullName)
	if err != nil {
		return "", err
	}
	if info == nil {
		return "", fmt.Errorf("container %q not found", name)
	}
	return info.ID, nil
}
