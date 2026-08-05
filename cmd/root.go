package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jkandasa/containerctl/internal/config"
	"github.com/jkandasa/containerctl/internal/render"
	rt "github.com/jkandasa/containerctl/internal/runtime"
	"github.com/jkandasa/containerctl/internal/runtime/docker"
	"github.com/jkandasa/containerctl/internal/runtime/podman"
)

var (
	flagFile    string
	flagRuntime string
	flagSocket  string
	flagOutput  string
	flagNoColor bool
	flagProject string
)

var rootCmd = &cobra.Command{
	Use:          "containerctl",
	Short:        "Declarative container management from a single YAML file",
	SilenceUsage: true,
	// Resolve the stack file once for every command:
	// explicit -f/--file > path from `containerctl stack` > stack.yaml.
	PersistentPreRunE: resolveStackFile,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&flagFile, "file", "f", "", "YAML stack file (default: from \"stack\", else stack.yaml)")
	rootCmd.PersistentFlags().StringVar(&flagRuntime, "runtime", "", "container runtime: docker|podman (overrides YAML)")
	rootCmd.PersistentFlags().StringVar(&flagSocket, "socket", "", "override runtime socket path")
	rootCmd.PersistentFlags().StringVarP(&flagOutput, "output", "o", "console", "output format: console|json|yaml")
	rootCmd.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "disable ANSI color output (also respects NO_COLOR env var)")
	rootCmd.PersistentFlags().StringVar(&flagProject, "project", "", "override project name from YAML")
}

// resolveStackFile sets flagFile from -f, the saved `stack` path, or stack.yaml.
func resolveStackFile(cmd *cobra.Command, _ []string) error {
	// Persistent -f/--file is defined on the root; Changed is true when the user passed it.
	changed := cmd.Root().PersistentFlags().Changed("file")
	resolved, err := config.ResolveStackFile(flagFile, changed)
	if err != nil {
		return err
	}
	flagFile = resolved
	return nil
}

func newRuntime(runtimeName, socket string) (rt.Runtime, error) {
	switch runtimeName {
	case "docker", "":
		return docker.New(socket)
	case "podman":
		return podman.New(socket)
	default:
		return nil, fmt.Errorf("unknown runtime %q; use docker or podman", runtimeName)
	}
}

// runtimeFrom builds a runtime from a loaded stack, applying flag overrides.
// Priority: --runtime flag > stack.runtime, --socket flag > stack.socket.
// If socket is set (from either source), the runtime type is optional —
// the Docker-compatible API works for Docker, Podman, OrbStack, Colima, etc.
func runtimeFrom(stack *config.Stack) (rt.Runtime, error) {
	name := stack.Runtime
	if flagRuntime != "" {
		name = flagRuntime
	}
	socket := stack.Socket
	if flagSocket != "" {
		socket = flagSocket
	}
	return newRuntime(name, socket)
}

// runtimeFromOptional builds a runtime using the stack when available, falling
// back to --runtime and --socket flags only. Use this for host-wide commands
// (images, volumes, networks, prune) that do not require a stack file.
func runtimeFromOptional(stack *config.Stack) (rt.Runtime, error) {
	if stack != nil {
		return runtimeFrom(stack)
	}
	return newRuntime(flagRuntime, flagSocket)
}

func colors() render.Colors {
	if flagNoColor || os.Getenv("NO_COLOR") != "" {
		return render.NoColors()
	}
	return render.ANSIColors()
}

func pingRuntime(ctx context.Context, runtime rt.Runtime) error {
	if err := runtime.Ping(ctx); err != nil {
		return fmt.Errorf("%s daemon unreachable: %w", runtime.Name(), err)
	}
	return nil
}

type authFileSetter interface{ SetAuthFile(path string) }

// applyAuthFile wires the stack's auth_file into the runtime when supported.
func applyAuthFile(runtime rt.Runtime, path string) {
	if path == "" {
		return
	}
	if s, ok := runtime.(authFileSetter); ok {
		s.SetAuthFile(path)
	}
}
