package cmd

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	BuildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version and runtime info",
	RunE:  runVersion,
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

func runVersion(cmd *cobra.Command, args []string) error {
	fmt.Printf("version:    %s\n", Version)
	fmt.Printf("build date: %s\n", BuildDate)
	fmt.Printf("go version: %s\n", goVersion())
	fmt.Printf("os/arch:    %s/%s\n", runtime.GOOS, runtime.GOARCH)

	ctx := context.Background()
	rtName := flagRuntime
	if rtName == "" {
		rtName = "docker"
	}
	rt, err := newRuntime(rtName)
	if err == nil {
		defer rt.Close()
		if err := rt.Ping(ctx); err == nil {
			fmt.Printf("runtime:    %s (reachable)\n", rt.Name())
		} else {
			fmt.Printf("runtime:    %s (unreachable: %v)\n", rt.Name(), err)
		}
	}
	return nil
}

func goVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.GoVersion
	}
	return runtime.Version()
}
