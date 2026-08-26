package docker

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/creack/pty"

	rt "github.com/jkandasa/containerctl/internal/runtime"
)

func openSizedTTY(t *testing.T, rows, cols uint16) (master, slave uintptr, cleanup func()) {
	t.Helper()
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skipf("open pty: %v", err)
	}
	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: rows, Cols: cols}); err != nil {
		_ = ptmx.Close()
		_ = tty.Close()
		t.Fatalf("set pty size: %v", err)
	}
	return ptmx.Fd(), tty.Fd(), func() {
		_ = ptmx.Close()
		_ = tty.Close()
	}
}

func TestConsoleSizeUsesHostWinsize(t *testing.T) {
	_, slave, cleanup := openSizedTTY(t, 37, 142)
	defer cleanup()

	got := consoleSize(rt.ExecOptions{Tty: true, StdinFd: slave})
	if got == nil {
		t.Fatal("expected console size from tty fd")
	}
	if (*got)[0] != 37 || (*got)[1] != 142 {
		t.Fatalf("consoleSize = [%d, %d], want [37, 142] (height, width)", (*got)[0], (*got)[1])
	}
}

func TestTtyEnvSeedsColumnsAndLines(t *testing.T) {
	sz := [2]uint{37, 142}
	got := ttyEnv([]string{"FOO=bar"}, &sz)
	want := []string{"FOO=bar", "COLUMNS=142", "LINES=37"}
	if len(got) != len(want) {
		t.Fatalf("ttyEnv = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ttyEnv = %v, want %v", got, want)
		}
	}
	if got := ttyEnv([]string{"FOO=bar"}, nil); len(got) != 1 || got[0] != "FOO=bar" {
		t.Fatalf("nil size should leave env unchanged, got %v", got)
	}
}

func TestConsoleSizeNilWhenNotTty(t *testing.T) {
	_, slave, cleanup := openSizedTTY(t, 24, 80)
	defer cleanup()

	if got := consoleSize(rt.ExecOptions{Tty: false, StdinFd: slave}); got != nil {
		t.Fatalf("expected nil when Tty=false, got %v", *got)
	}
}

func TestTtySizeRejectsZeroDimensions(t *testing.T) {
	_, slave, cleanup := openSizedTTY(t, 0, 0)
	defer cleanup()

	if _, _, ok := ttySize(slave); ok {
		t.Fatal("zero winsize must not be treated as valid")
	}
}

func TestInitExecResizeRetriesUntilSuccess(t *testing.T) {
	_, slave, cleanup := openSizedTTY(t, 24, 100)
	defer cleanup()

	var n atomic.Int32
	done := make(chan struct{})
	initExecResize(context.Background(), slave, func(rows, cols uint16) error {
		if n.Add(1) < 3 {
			return fmt.Errorf("not ready")
		}
		if rows != 24 || cols != 100 {
			t.Errorf("resize %dx%d, want 24x100", rows, cols)
		}
		close(done)
		return nil
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("resize never succeeded after %d attempts", n.Load())
	}
}

func TestInitExecResizeSucceedsImmediately(t *testing.T) {
	_, slave, cleanup := openSizedTTY(t, 40, 160)
	defer cleanup()

	var called atomic.Int32
	initExecResize(context.Background(), slave, func(rows, cols uint16) error {
		called.Add(1)
		if rows != 40 || cols != 160 {
			t.Errorf("resize %dx%d, want 40x160", rows, cols)
		}
		return nil
	})
	if called.Load() != 1 {
		t.Fatalf("immediate success should call resize once, got %d", called.Load())
	}
}
