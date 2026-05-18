package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"

	"gopkg.in/yaml.v3"

	"github.com/spf13/cobra"

	rt "github.com/jkandasa/containerctl/internal/runtime"
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

type versionEngineInfo struct {
	Name          string `json:"name"                      yaml:"name"`
	Reachable     bool   `json:"reachable"                 yaml:"reachable"`
	Version       string `json:"version,omitempty"         yaml:"version,omitempty"`
	APIVersion    string `json:"api_version,omitempty"     yaml:"api_version,omitempty"`
	MinAPIVersion string `json:"min_api_version,omitempty" yaml:"min_api_version,omitempty"`
	Platform      string `json:"platform,omitempty"        yaml:"platform,omitempty"`
	OS            string `json:"os,omitempty"              yaml:"os,omitempty"`
	Arch          string `json:"arch,omitempty"            yaml:"arch,omitempty"`
	KernelVersion string `json:"kernel_version,omitempty"  yaml:"kernel_version,omitempty"`
}

type versionInfo struct {
	Version   string             `json:"version"    yaml:"version"`
	BuildDate string             `json:"build_date" yaml:"build_date"`
	GoVersion string             `json:"go_version" yaml:"go_version"`
	OS        string             `json:"os"         yaml:"os"`
	Arch      string             `json:"arch"       yaml:"arch"`
	Engine    *versionEngineInfo `json:"engine,omitempty" yaml:"engine,omitempty"`
}

func runVersion(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	info := versionInfo{
		Version:   Version,
		BuildDate: BuildDate,
		GoVersion: goVersion(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}

	rtName := flagRuntime
	if rtName == "" {
		rtName = "docker"
	}
	if r, err := newRuntime(rtName, flagSocket); err == nil {
		defer r.Close()
		engine := &versionEngineInfo{Name: r.Name()}
		if pingErr := r.Ping(ctx); pingErr != nil {
			engine.Reachable = false
		} else {
			engine.Reachable = true
			if ev, err := r.EngineVersion(ctx); err == nil {
				applyEngineInfo(engine, ev)
			}
		}
		info.Engine = engine
	}

	switch flagOutput {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	case "yaml":
		enc := yaml.NewEncoder(os.Stdout)
		enc.SetIndent(2)
		return enc.Encode(info)
	default:
		fmt.Printf("version:    %s\n", info.Version)
		fmt.Printf("build date: %s\n", info.BuildDate)
		fmt.Printf("go version: %s\n", info.GoVersion)
		fmt.Printf("os/arch:    %s/%s\n", info.OS, info.Arch)
		if e := info.Engine; e != nil {
			if !e.Reachable {
				fmt.Printf("runtime:    %s (unreachable)\n", e.Name)
			} else {
				fmt.Printf("runtime:    %s (reachable)\n", e.Name)
				fmt.Printf("  engine:   %s\n", e.Version)
				fmt.Printf("  api:      %s (min: %s)\n", e.APIVersion, e.MinAPIVersion)
				if e.Platform != "" {
					fmt.Printf("  platform: %s\n", e.Platform)
				}
				fmt.Printf("  os/arch:  %s/%s\n", e.OS, e.Arch)
				if e.KernelVersion != "" {
					fmt.Printf("  kernel:   %s\n", e.KernelVersion)
				}
			}
		}
	}
	return nil
}

func applyEngineInfo(dst *versionEngineInfo, src rt.EngineInfo) {
	dst.Version = src.Version
	dst.APIVersion = src.APIVersion
	dst.MinAPIVersion = src.MinAPIVersion
	dst.Platform = src.Platform
	dst.OS = src.OS
	dst.Arch = src.Arch
	dst.KernelVersion = src.KernelVersion
}

func goVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.GoVersion
	}
	return runtime.Version()
}
