package docker

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"

	rt "github.com/jkandasa/containerctl/internal/runtime"
)

func toPathStat(st container.PathStat) rt.PathStat {
	return rt.PathStat{
		Name:       st.Name,
		Size:       st.Size,
		Mode:       st.Mode,
		Mtime:      st.Mtime,
		LinkTarget: st.LinkTarget,
	}
}

func (c *Client) StatPath(ctx context.Context, id, path string) (*rt.PathStat, error) {
	st, err := c.cli.ContainerStatPath(ctx, id, path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	out := toPathStat(st)
	return &out, nil
}

func (c *Client) CopyFromContainer(ctx context.Context, id, srcPath string) (io.ReadCloser, rt.PathStat, error) {
	rc, st, err := c.cli.CopyFromContainer(ctx, id, srcPath)
	if err != nil {
		return nil, rt.PathStat{}, fmt.Errorf("copy from container %s: %w", srcPath, err)
	}
	return rc, toPathStat(st), nil
}

func (c *Client) CopyToContainer(ctx context.Context, id, destPath string, content io.Reader, opts rt.CopyToOptions) error {
	err := c.cli.CopyToContainer(ctx, id, destPath, content, container.CopyToContainerOptions{
		AllowOverwriteDirWithFile: opts.AllowOverwriteDirWithFile,
		CopyUIDGID:                opts.CopyUIDGID,
	})
	if err != nil {
		return fmt.Errorf("copy to container %s: %w", destPath, err)
	}
	return nil
}
