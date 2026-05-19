//go:build windows

package docker

import "context"

func watchResize(ctx context.Context, fd uintptr, fn func(rows, cols uint16)) {}
