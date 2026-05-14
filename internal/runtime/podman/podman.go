package podman

import (
	"fmt"
	"os"

	rt "github.com/jkandasa/containerctl/internal/runtime"
	"github.com/jkandasa/containerctl/internal/runtime/docker"
)

// Client wraps the Docker client pointed at Podman's Docker-compatible socket.
type Client struct {
	*docker.Client
}

func New(socketPath string) (*Client, error) {
	if socketPath == "" {
		socketPath = defaultSocket()
	}
	dc, err := docker.New(socketPath)
	if err != nil {
		return nil, fmt.Errorf("podman client: %w", err)
	}
	return &Client{Client: dc}, nil
}

func (c *Client) Name() string { return "podman" }

func defaultSocket() string {
	// rootless first
	uid := os.Getuid()
	if uid != 0 {
		xdg := os.Getenv("XDG_RUNTIME_DIR")
		if xdg == "" {
			xdg = fmt.Sprintf("/run/user/%d", uid)
		}
		return xdg + "/podman/podman.sock"
	}
	return "/run/podman/podman.sock"
}

// Ensure Client implements Runtime at compile time.
var _ rt.Runtime = (*Client)(nil)
