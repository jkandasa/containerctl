package copy

import (
	"archive/tar"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Info describes one side of an archive copy.
type Info struct {
	Path         string
	Exists       bool
	IsDir        bool
	RebaseName   string
	CopyContents bool // SRC ended with "/.": copy directory contents, not the dir
}

func hasTrailingSep(p string) bool {
	if p == "" {
		return false
	}
	last := p[len(p)-1]
	return last == '/' || last == filepath.Separator
}

func isCopyContents(p string) bool {
	return strings.HasSuffix(p, "/.") || strings.HasSuffix(p, string(filepath.Separator)+".")
}

func preserveTrailing(cleaned, original string) string {
	if isCopyContents(original) {
		return cleaned + string(filepath.Separator) + "."
	}
	if hasTrailingSep(original) && !hasTrailingSep(cleaned) {
		return cleaned + string(filepath.Separator)
	}
	return cleaned
}

func resolveLocalPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return preserveTrailing(abs, p), nil
}

func sourceInfo(srcPath string, followLink bool) (Info, error) {
	copyContents := isCopyContents(srcPath)
	cleaned := filepath.Clean(srcPath)

	info := Info{
		Path:         cleaned,
		Exists:       true,
		CopyContents: copyContents,
	}

	if followLink {
		li, err := os.Lstat(cleaned)
		if err != nil {
			return Info{}, err
		}
		if li.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(cleaned)
			if err != nil {
				return Info{}, err
			}
			if filepath.Base(resolved) != origBase(srcPath) {
				info.RebaseName = origBase(srcPath)
			}
			info.Path = resolved
			cleaned = resolved
		}
	}

	fi, err := os.Lstat(cleaned)
	if err != nil {
		return Info{}, err
	}
	info.IsDir = fi.IsDir()
	return info, nil
}

func origBase(p string) string {
	p = strings.TrimRight(p, `/\`)
	if isCopyContents(p) {
		p = strings.TrimSuffix(p, ".")
		p = strings.TrimRight(p, `/\`)
	}
	return filepath.Base(p)
}

func destInfo(dstPath string) (Info, error) {
	trailingDir := hasTrailingSep(dstPath) || isCopyContents(dstPath)
	cleaned := filepath.Clean(dstPath)

	fi, err := os.Lstat(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			return Info{Path: cleaned, Exists: false, IsDir: trailingDir}, nil
		}
		return Info{}, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		fi, err = os.Stat(cleaned)
		if err != nil {
			return Info{}, err
		}
	}
	if trailingDir && !fi.IsDir() {
		return Info{}, fmt.Errorf("%s: not a directory", dstPath)
	}
	return Info{Path: cleaned, Exists: true, IsDir: fi.IsDir()}, nil
}

func validateOutputPath(p string) error {
	if p == "-" {
		return nil
	}
	dir := filepath.Dir(filepath.Clean(p))
	st, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("destination directory %q: %w", dir, err)
	}
	if !st.IsDir() {
		return fmt.Errorf("destination directory %q is not a directory", dir)
	}
	return nil
}

func asReadCloser(r io.Reader) io.ReadCloser {
	if rc, ok := r.(io.ReadCloser); ok {
		return rc
	}
	return io.NopCloser(r)
}

// prepareCopy rewrites src so extracting into the returned directory produces
// docker-cp behaviour (file→file rename, dir→new-name, file into existing dir).
func prepareCopy(src io.Reader, srcInfo, dstInfo Info) (dstDir string, content io.ReadCloser, err error) {
	if srcInfo.IsDir && dstInfo.Exists && !dstInfo.IsDir {
		return "", nil, fmt.Errorf("cannot copy directory onto a file")
	}
	if srcInfo.CopyContents && dstInfo.Exists && !dstInfo.IsDir {
		return "", nil, fmt.Errorf("cannot copy directory contents onto a file")
	}

	if dstInfo.IsDir {
		if !dstInfo.Exists {
			if err := os.MkdirAll(dstInfo.Path, 0o755); err != nil {
				return "", nil, err
			}
		}
		return dstInfo.Path, asReadCloser(src), nil
	}

	dstDir = filepath.Dir(dstInfo.Path)
	srcBase := filepath.Base(srcInfo.Path)
	if srcInfo.RebaseName != "" {
		srcBase = srcInfo.RebaseName
	}
	dstBase := filepath.Base(dstInfo.Path)
	if srcBase == dstBase {
		return dstDir, asReadCloser(src), nil
	}
	return dstDir, rebaseReader(src, srcBase, dstBase), nil
}

func rebaseReader(src io.Reader, oldBase, newBase string) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		err := rewriteTarBase(src, pw, oldBase, newBase)
		if rc, ok := src.(io.Closer); ok {
			_ = rc.Close()
		}
		_ = pw.CloseWithError(err)
	}()
	return pr
}

func rewriteTarBase(src io.Reader, dst io.Writer, oldBase, newBase string) error {
	tr := tar.NewReader(src)
	tw := tar.NewWriter(dst)
	defer tw.Close()
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return tw.Close()
		}
		if err != nil {
			return err
		}
		hdr.Name = rewriteBase(hdr.Name, oldBase, newBase)
		if hdr.Typeflag == tar.TypeLink {
			hdr.Linkname = rewriteBase(hdr.Linkname, oldBase, newBase)
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := io.Copy(tw, tr); err != nil {
			return err
		}
	}
}

func rewriteBase(name, oldBase, newBase string) string {
	slash := filepath.ToSlash(name)
	switch {
	case slash == oldBase:
		return newBase
	case strings.HasPrefix(slash, oldBase+"/"):
		return newBase + slash[len(oldBase):]
	default:
		return name
	}
}

func packLocal(info Info) (io.ReadCloser, error) {
	pr, pw := io.Pipe()
	go func() {
		tw := tar.NewWriter(pw)
		err := writeTar(tw, info)
		_ = tw.Close()
		_ = pw.CloseWithError(err)
	}()
	return pr, nil
}

func writeTar(tw *tar.Writer, info Info) error {
	rootName := filepath.Base(info.Path)
	if info.RebaseName != "" {
		rootName = info.RebaseName
	}
	if info.CopyContents {
		entries, err := os.ReadDir(info.Path)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := addPath(tw, filepath.Join(info.Path, e.Name()), e.Name()); err != nil {
				return err
			}
		}
		return nil
	}
	return addPath(tw, info.Path, rootName)
}

func addPath(tw *tar.Writer, srcPath, nameInTar string) error {
	fi, err := os.Lstat(srcPath)
	if err != nil {
		return err
	}
	nameInTar = filepath.ToSlash(nameInTar)

	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(srcPath)
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(fi, target)
		if err != nil {
			return err
		}
		hdr.Name = nameInTar
		hdr.Linkname = target
		return tw.WriteHeader(hdr)

	case fi.IsDir():
		hdr, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return err
		}
		hdr.Name = nameInTar + "/"
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		entries, err := os.ReadDir(srcPath)
		if err != nil {
			return err
		}
		for _, e := range entries {
			child := filepath.Join(srcPath, e.Name())
			childName := path.Join(nameInTar, e.Name())
			if err := addPath(tw, child, childName); err != nil {
				return err
			}
		}
		return nil

	case fi.Mode().IsRegular():
		hdr, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return err
		}
		hdr.Name = nameInTar
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(srcPath)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr

	default:
		return nil
	}
}

func extractToHost(r io.Reader, srcInfo Info, dstPath string) (int64, error) {
	dst, err := destInfo(dstPath)
	if err != nil {
		return 0, err
	}
	dstDir, content, err := prepareCopy(r, srcInfo, dst)
	if err != nil {
		return 0, err
	}
	defer content.Close()
	return untar(content, dstDir)
}

func untar(r io.Reader, destDir string) (int64, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return 0, err
	}
	tr := tar.NewReader(r)
	var written int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return written, nil
		}
		if err != nil {
			return written, err
		}
		n, err := extractOne(destDir, hdr, tr)
		written += n
		if err != nil {
			return written, err
		}
	}
}

func extractOne(destDir string, hdr *tar.Header, r io.Reader) (int64, error) {
	name, err := sanitizeTarName(hdr.Name)
	if err != nil {
		return 0, err
	}
	if name == "" {
		return 0, nil
	}
	target := filepath.Join(destDir, filepath.FromSlash(name))
	if !within(destDir, target) {
		return 0, fmt.Errorf("refusing to extract %q: path traversal", hdr.Name)
	}

	switch hdr.Typeflag {
	case tar.TypeDir:
		return 0, os.MkdirAll(target, hdr.FileInfo().Mode().Perm())

	case tar.TypeReg, 0:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return 0, err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, hdr.FileInfo().Mode().Perm())
		if err != nil {
			return 0, err
		}
		n, copyErr := io.Copy(f, r)
		closeErr := f.Close()
		if copyErr != nil {
			return n, copyErr
		}
		if closeErr != nil {
			return n, closeErr
		}
		if !hdr.ModTime.IsZero() {
			_ = os.Chtimes(target, hdr.ModTime, hdr.ModTime)
		}
		return n, nil

	case tar.TypeSymlink:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return 0, err
		}
		_ = os.Remove(target)
		return 0, os.Symlink(hdr.Linkname, target)

	case tar.TypeLink:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return 0, err
		}
		linkTarget := filepath.Join(destDir, filepath.FromSlash(hdr.Linkname))
		if !within(destDir, linkTarget) {
			return 0, fmt.Errorf("refusing to extract hard link %q: path traversal", hdr.Linkname)
		}
		_ = os.Remove(target)
		return 0, os.Link(linkTarget, target)

	default:
		return 0, nil
	}
}

func sanitizeTarName(name string) (string, error) {
	name = filepath.ToSlash(name)
	name = strings.TrimPrefix(name, "./")
	name = strings.TrimPrefix(name, "/")
	if name == "" || name == "." {
		return "", nil
	}
	cleaned := path.Clean(name)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("refusing to extract %q: path traversal", name)
	}
	for _, p := range strings.Split(cleaned, "/") {
		if p == ".." {
			return "", fmt.Errorf("refusing to extract %q: path traversal", name)
		}
	}
	return cleaned, nil
}

func within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func localContentSize(srcPath string) (int64, error) {
	fi, err := os.Lstat(srcPath)
	if err != nil {
		return 0, err
	}
	if !fi.IsDir() {
		if fi.Mode().IsRegular() {
			return fi.Size(), nil
		}
		return 0, nil
	}
	var total int64
	err = filepath.WalkDir(srcPath, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}
