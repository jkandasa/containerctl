package cmd

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Version and BuildDate are set at link time via -ldflags (see Makefile).
var (
	Version   = "dev"
	BuildDate = "unknown"
)

func versionTemplate() string {
	return fmt.Sprintf(
		"version:    {{.Version}}\nbuild date: %s\ngo version: %s\nos/arch:    %s/%s\n",
		BuildDate, goVersion(), runtime.GOOS, runtime.GOARCH,
	)
}

func goVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return info.GoVersion
	}
	return runtime.Version()
}
