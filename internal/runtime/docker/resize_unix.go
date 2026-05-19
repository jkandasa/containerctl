//go:build !windows

package docker

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/moby/term"
)

func watchResize(ctx context.Context, fd uintptr, fn func(rows, cols uint16)) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		defer signal.Stop(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-ch:
				if !ok {
					return
				}
				if ws, err := term.GetWinsize(fd); err == nil {
					fn(ws.Height, ws.Width)
				}
			}
		}
	}()
}
