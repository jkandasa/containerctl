package copy

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	rt "github.com/jkandasa/containerctl/internal/runtime"
)

// Options configure a copy.
type Options struct {
	FollowLink bool
	Archive    bool // preserve uid/gid when copying into a container
	Overwrite  bool // allow directory/file type mismatch on the container
}

// FS is the subset of a runtime needed to copy files.
type FS interface {
	StatPath(ctx context.Context, id, path string) (*rt.PathStat, error)
	CopyFromContainer(ctx context.Context, id, srcPath string) (io.ReadCloser, rt.PathStat, error)
	CopyToContainer(ctx context.Context, id, destPath string, content io.Reader, opts rt.CopyToOptions) error
}

// resolveContainerSource follows a symlink at srcPath when followLink is set.
// rebaseName is the original basename when the target has a different name.
func resolveContainerSource(ctx context.Context, fs FS, id, srcPath string, followLink bool) (resolved, rebaseName string) {
	resolved = srcPath
	if !followLink {
		return resolved, ""
	}
	st, err := fs.StatPath(ctx, id, srcPath)
	if err != nil || st.Mode&os.ModeSymlink == 0 || st.LinkTarget == "" {
		return resolved, ""
	}
	target := st.LinkTarget
	if !path.IsAbs(target) {
		target = path.Join(path.Dir(srcPath), target)
	}
	if path.Base(target) != path.Base(srcPath) {
		rebaseName = path.Base(srcPath)
	}
	return target, rebaseName
}

func containerDestInfo(ctx context.Context, fs FS, id, destPath string) (Info, error) {
	dstInfo := Info{Path: destPath}
	if hasTrailingSep(destPath) || isCopyContents(destPath) {
		dstInfo.IsDir = true
		dstInfo.Path = strings.TrimRight(destPath, `/\`)
		if isCopyContents(destPath) {
			dstInfo.Path = strings.TrimSuffix(dstInfo.Path, ".")
			dstInfo.Path = strings.TrimRight(dstInfo.Path, `/\`)
		}
		if dstInfo.Path == "" {
			dstInfo.Path = "/"
		}
	}

	if st, err := fs.StatPath(ctx, id, destPath); err == nil {
		if st.Mode&os.ModeSymlink != 0 && st.LinkTarget != "" {
			target := st.LinkTarget
			if !path.IsAbs(target) {
				target = path.Join(path.Dir(destPath), target)
			}
			dstInfo.Path = target
			st, err = fs.StatPath(ctx, id, target)
		}
		if err == nil {
			if !st.Mode.IsDir() && !st.Mode.IsRegular() {
				return Info{}, fmt.Errorf("destination %q must be a directory or a regular file", destPath)
			}
			dstInfo.Exists = true
			dstInfo.IsDir = st.Mode.IsDir()
		}
	}
	return dstInfo, nil
}

func copyToOpts(opts Options) rt.CopyToOptions {
	return rt.CopyToOptions{
		AllowOverwriteDirWithFile: opts.Overwrite,
		CopyUIDGID:                opts.Archive,
	}
}

func reportedSize(st rt.PathStat, extracted int64) int64 {
	if !st.Mode.IsDir() && st.Size > 0 {
		return st.Size
	}
	return extracted
}

// FromContainer copies srcPath in the container to destPath on the host.
// destPath "-" writes a tar archive to stdout.
func FromContainer(ctx context.Context, fs FS, id, srcPath, destPath string, opts Options, stdout io.Writer) (int64, error) {
	srcPath, rebaseName := resolveContainerSource(ctx, fs, id, srcPath, opts.FollowLink)

	rc, st, err := fs.CopyFromContainer(ctx, id, srcPath)
	if err != nil {
		return 0, err
	}
	defer rc.Close()

	if destPath == "-" {
		if stdout == nil {
			stdout = io.Discard
		}
		return io.Copy(stdout, rc)
	}

	dstAbs, err := resolveLocalPath(destPath)
	if err != nil {
		return 0, err
	}
	if err := validateOutputPath(dstAbs); err != nil {
		return 0, err
	}

	srcInfo := Info{
		Path:       srcPath,
		Exists:     true,
		IsDir:      st.Mode.IsDir(),
		RebaseName: rebaseName,
	}

	content := io.Reader(rc)
	if rebaseName != "" {
		content = rebaseReader(rc, path.Base(srcPath), rebaseName)
		// The archive now uses the original source name; don't rebase again.
		srcInfo.Path = path.Join(path.Dir(srcPath), rebaseName)
		srcInfo.RebaseName = ""
	}

	n, err := extractToHost(content, srcInfo, dstAbs)
	if err != nil {
		return n, err
	}
	return reportedSize(st, n), nil
}

// ToContainer copies srcPath on the host into destPath in the container.
// srcPath "-" reads a tar archive from stdin; destPath must be a directory.
func ToContainer(ctx context.Context, fs FS, id, srcPath, destPath string, opts Options, stdin io.Reader) (int64, error) {
	dstInfo, err := containerDestInfo(ctx, fs, id, destPath)
	if err != nil {
		return 0, err
	}

	toOpts := copyToOpts(opts)

	if srcPath == "-" {
		if !dstInfo.IsDir {
			return 0, fmt.Errorf("destination %q must be a directory", destPath)
		}
		if stdin == nil {
			return 0, fmt.Errorf("stdin is empty")
		}
		if err := fs.CopyToContainer(ctx, id, dstInfo.Path, stdin, toOpts); err != nil {
			return 0, err
		}
		return 0, nil
	}

	abs, err := resolveLocalPath(srcPath)
	if err != nil {
		return 0, err
	}
	srcInfo, err := sourceInfo(abs, opts.FollowLink)
	if err != nil {
		return 0, err
	}

	packed, err := packLocal(srcInfo)
	if err != nil {
		return 0, err
	}
	defer packed.Close()

	// prepareCopy may MkdirAll for a host dest; for a container dest we only
	// need the dest directory path and a possibly rebased archive.
	dstDir, prepared, err := prepareArchiveForContainer(packed, srcInfo, dstInfo)
	if err != nil {
		return 0, err
	}
	defer prepared.Close()

	if err := fs.CopyToContainer(ctx, id, dstDir, prepared, toOpts); err != nil {
		return 0, err
	}
	n, _ := localContentSize(srcInfo.Path)
	return n, nil
}

// BetweenContainers copies srcPath in srcID to destPath in destID.
// Distinct containers are streamed. The same container is fully drained first:
// the engine holds a filesystem lock for archive GET and PUT, so overlapping
// them on one container deadlocks.
func BetweenContainers(ctx context.Context, fs FS, srcID, srcPath, destID, destPath string, opts Options) (int64, error) {
	srcPath, rebaseName := resolveContainerSource(ctx, fs, srcID, srcPath, opts.FollowLink)
	if srcID == destID && path.Clean(srcPath) == path.Clean(destPath) {
		return 0, fmt.Errorf("source and destination are the same path")
	}

	// Stat dest before opening the source archive. On the same container a
	// StatPath during an in-flight GET /archive can also block.
	dstInfo, err := containerDestInfo(ctx, fs, destID, destPath)
	if err != nil {
		return 0, err
	}

	rc, st, err := fs.CopyFromContainer(ctx, srcID, srcPath)
	if err != nil {
		return 0, err
	}

	srcInfo := Info{
		Path:       srcPath,
		Exists:     true,
		IsDir:      st.Mode.IsDir(),
		RebaseName: rebaseName,
	}

	var content io.Reader = rc
	if srcID == destID {
		tmp, err := drainToTemp(rc)
		_ = rc.Close()
		if err != nil {
			return 0, err
		}
		defer func() {
			name := tmp.Name()
			_ = tmp.Close()
			_ = os.Remove(name)
		}()
		content = tmp
	} else {
		defer rc.Close()
	}

	if rebaseName != "" {
		content = rebaseReader(content, path.Base(srcPath), rebaseName)
		srcInfo.Path = path.Join(path.Dir(srcPath), rebaseName)
		srcInfo.RebaseName = ""
	}

	dstDir, prepared, err := prepareArchiveForContainer(content, srcInfo, dstInfo)
	if err != nil {
		return 0, err
	}
	defer prepared.Close()

	if err := fs.CopyToContainer(ctx, destID, dstDir, prepared, copyToOpts(opts)); err != nil {
		return 0, err
	}
	return reportedSize(st, 0), nil
}

// drainToTemp copies r into a seekable temp file. The caller must close and
// remove the file.
func drainToTemp(r io.Reader) (*os.File, error) {
	f, err := os.CreateTemp("", "containerctl-cp-")
	if err != nil {
		return nil, fmt.Errorf("buffer copy: %w", err)
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, fmt.Errorf("buffer copy: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, fmt.Errorf("buffer copy: %w", err)
	}
	return f, nil
}

// prepareArchiveForContainer is prepareCopy without creating host directories.
func prepareArchiveForContainer(src io.Reader, srcInfo, dstInfo Info) (dstDir string, content io.ReadCloser, err error) {
	if srcInfo.IsDir && dstInfo.Exists && !dstInfo.IsDir {
		return "", nil, fmt.Errorf("cannot copy directory onto a file")
	}
	if srcInfo.CopyContents && dstInfo.Exists && !dstInfo.IsDir {
		return "", nil, fmt.Errorf("cannot copy directory contents onto a file")
	}

	if dstInfo.IsDir {
		return dstInfo.Path, asReadCloser(src), nil
	}

	dstDir = path.Dir(dstInfo.Path)
	if dstDir == "." || dstDir == "" {
		dstDir = "/"
	}
	srcBase := path.Base(srcInfo.Path)
	if srcInfo.RebaseName != "" {
		srcBase = srcInfo.RebaseName
	}
	dstBase := path.Base(dstInfo.Path)
	if srcBase == dstBase {
		return dstDir, asReadCloser(src), nil
	}
	return dstDir, rebaseReader(src, srcBase, dstBase), nil
}
