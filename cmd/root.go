package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

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
	flagVerbose bool
	flagProject string
)

var rootCmd = &cobra.Command{
	Use:   "containerctl",
	Short: "Declarative container management from a single YAML file",
	SilenceUsage: true,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&flagFile, "file", "f", "stack.yaml", "YAML stack file")
	rootCmd.PersistentFlags().StringVar(&flagRuntime, "runtime", "", "container runtime: docker|podman (overrides YAML)")
	rootCmd.PersistentFlags().StringVar(&flagSocket, "socket", "", "override runtime socket path")
	rootCmd.PersistentFlags().StringVarP(&flagOutput, "output", "o", "text", "output format: text|json")
	rootCmd.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "disable ANSI colors")
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().StringVar(&flagProject, "project", "", "override project name from YAML")
}

func newRuntime(runtimeName string) (rt.Runtime, error) {
	switch runtimeName {
	case "docker", "":
		return docker.New(flagSocket)
	case "podman":
		return podman.New(flagSocket)
	default:
		return nil, fmt.Errorf("unknown runtime %q; use docker or podman", runtimeName)
	}
}

func colors() render.Colors {
	if flagNoColor {
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

