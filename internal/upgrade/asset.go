package upgrade

import (
	"fmt"
	"runtime"
)

// AssetName is the GitHub release asset for this GOOS/GOARCH, matching
// the names produced by `make cross-build`.
func AssetName(goos, goarch string) (string, error) {
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	var suffix string
	switch {
	case goos == "linux" && goarch == "amd64":
		suffix = "linux-amd64"
	case goos == "linux" && goarch == "arm64":
		suffix = "linux-arm64"
	case goos == "linux" && goarch == "arm":
		suffix = "linux-armv7"
	case goos == "darwin" && goarch == "amd64":
		suffix = "darwin-amd64"
	case goos == "darwin" && goarch == "arm64":
		suffix = "darwin-arm64"
	case goos == "windows" && goarch == "amd64":
		suffix = "windows-amd64.exe"
	case goos == "windows" && goarch == "arm64":
		suffix = "windows-arm64.exe"
	default:
		return "", fmt.Errorf("no published binary for %s/%s", goos, goarch)
	}
	return "containerctl-" + suffix, nil
}

// FindAsset returns the release asset whose name matches this platform.
func FindAsset(rel *Release, goos, goarch string) (*Asset, error) {
	name, err := AssetName(goos, goarch)
	if err != nil {
		return nil, err
	}
	if rel == nil {
		return nil, fmt.Errorf("no release")
	}
	for i := range rel.Assets {
		if rel.Assets[i].Name == name {
			return &rel.Assets[i], nil
		}
	}
	return nil, fmt.Errorf("release %s has no asset %s", rel.Tag, name)
}
