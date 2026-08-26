package docker

import (
	"context"
	"fmt"
	"time"

	"github.com/moby/term"

	rt "github.com/jkandasa/containerctl/internal/runtime"
)

// ttySize returns the current window size of fd.
//
// fd 0 is stdin and is a valid descriptor — callers must not treat 0 as "unset".
// A zero height or width is ignored because engines reject that resize and
// shells fall back to an 80-column default, which makes long input overwrite
// the current line instead of wrapping.
func ttySize(fd uintptr) (height, width uint16, ok bool) {
	ws, err := term.GetWinsize(fd)
	if err != nil || ws.Height == 0 || ws.Width == 0 {
		return 0, 0, false
	}
	return ws.Height, ws.Width, true
}

// consoleSize is the Docker API ConsoleSize value [height, width].
func consoleSize(opts rt.ExecOptions) *[2]uint {
	if !opts.Tty {
		return nil
	}
	h, w, ok := ttySize(opts.StdinFd)
	if !ok {
		return nil
	}
	sz := [2]uint{uint(h), uint(w)}
	return &sz
}

// ttyEnv adds COLUMNS and LINES so shells that only read the environment at
// startup wrap at the host width before the first SIGWINCH.
func ttyEnv(env []string, size *[2]uint) []string {
	if size == nil {
		return env
	}
	return append(append([]string(nil), env...),
		fmt.Sprintf("COLUMNS=%d", (*size)[1]),
		fmt.Sprintf("LINES=%d", (*size)[0]),
	)
}

// initExecResize applies the host window size to a newly attached exec TTY.
// ContainerExecResize fails until the exec process has actually started, so
// a failed first attempt is retried in the background (same approach as
// docker CLI's initTtySize).
func initExecResize(ctx context.Context, fd uintptr, fn func(rows, cols uint16) error) {
	try := func() error {
		h, w, ok := ttySize(fd)
		if !ok {
			return fmt.Errorf("no tty size")
		}
		return fn(h, w)
	}
	if err := try(); err == nil {
		return
	}
	go func() {
		for retry := 0; retry < 10; retry++ {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(time.Duration(retry+1) * 10 * time.Millisecond)
			if err := try(); err == nil {
				return
			}
		}
	}()
}
