package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// ManagedFilesDir is the subdirectory under data_path where file content is written.
	ManagedFilesDir = "files"
	defaultFileMode = 0o644
)

// resolveFiles absolutizes data_path (if needed) and fills each container FileMount
// with Content, HostPath, and effective RO. Requires data_path when files are used
// (enforced by validate).
func resolveFiles(s *Stack, stackDir string) error {
	if !s.UsesFiles() {
		return nil
	}
	if strings.TrimSpace(s.DataPath) == "" {
		return fmt.Errorf("data_path is required when files are declared (managed files are stored under data_path/files/)")
	}

	base := s.DataPath
	if !filepath.IsAbs(base) {
		base = filepath.Join(stackDir, base)
	}
	if abs, err := filepath.Abs(base); err == nil {
		base = abs
	}
	s.DataPath = base

	byName := make(map[string]*FileDef, len(s.Files))
	for i := range s.Files {
		f := &s.Files[i]
		byName[f.Name] = f
	}

	filesDir := filepath.Join(base, ManagedFilesDir)
	for i := range s.Containers {
		c := &s.Containers[i]
		for j := range c.Files {
			fm := &c.Files[j]
			def := byName[fm.Source]
			if def == nil {
				// validate should have caught this
				return fmt.Errorf("containers[%s].files[%d]: source %q not found", c.Name, j, fm.Source)
			}
			fm.Content = def.Content
			fm.HostPath = filepath.Join(filesDir, fm.Source)
			if fm.ReadOnly == nil {
				fm.RO = true
			} else {
				fm.RO = *fm.ReadOnly
			}
			if fm.Mode == "" {
				fm.Mode = fmt.Sprintf("%04o", defaultFileMode)
			}
		}
	}
	return nil
}

// MaterializeFiles writes each managed file for the container under data_path/files/.
// YAML content is the source of truth: existing host files are overwritten.
func MaterializeFiles(c *Container) error {
	for _, fm := range c.Files {
		if fm.HostPath == "" {
			return fmt.Errorf("containers[%s].files: host path not resolved for source %q (load stack via config.Load)", c.Name, fm.Source)
		}
		if err := os.MkdirAll(filepath.Dir(fm.HostPath), 0o755); err != nil {
			return fmt.Errorf("containers[%s].files: create dir for %s: %w", c.Name, fm.Source, err)
		}
		mode, err := parseFileMode(fm.Mode)
		if err != nil {
			return fmt.Errorf("containers[%s].files source %q: %w", c.Name, fm.Source, err)
		}
		if err := os.WriteFile(fm.HostPath, []byte(fm.Content), mode); err != nil {
			return fmt.Errorf("containers[%s].files: write %s: %w", c.Name, fm.HostPath, err)
		}
		// WriteFile only applies mode on create; always chmod for updates.
		if err := os.Chmod(fm.HostPath, mode); err != nil {
			return fmt.Errorf("containers[%s].files: chmod %s: %w", c.Name, fm.HostPath, err)
		}
	}
	return nil
}

// parseFileMode accepts octal modes like "644", "0644", or "0o644".
func parseFileMode(s string) (os.FileMode, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return defaultFileMode, nil
	}
	s = strings.TrimPrefix(s, "0o")
	s = strings.TrimPrefix(s, "0O")
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid mode %q (want octal like 0644)", s)
	}
	return os.FileMode(v), nil
}
