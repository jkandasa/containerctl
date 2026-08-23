package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

func validate(s *Stack) error {
	if strings.TrimSpace(s.Project) == "" {
		return fmt.Errorf("project name is required")
	}
	if len(s.Containers) == 0 {
		return fmt.Errorf("at least one container is required")
	}

	// Managed files require data_path — they are always stored under it.
	if s.UsesFiles() && strings.TrimSpace(s.DataPath) == "" {
		return fmt.Errorf("data_path is required when files are declared (managed files are stored under data_path/files/)")
	}

	fileSeen := map[string]bool{}
	for i, f := range s.Files {
		name := strings.TrimSpace(f.Name)
		if name == "" {
			return fmt.Errorf("files[%d]: name is required", i)
		}
		if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
			return fmt.Errorf("files[%d]: name %q must be a plain file name (no path separators)", i, name)
		}
		if fileSeen[name] {
			return fmt.Errorf("files[%d]: duplicate name %q", i, name)
		}
		fileSeen[name] = true
	}

	seen := map[string]bool{}
	for i, c := range s.Containers {
		if strings.TrimSpace(c.Name) == "" {
			return fmt.Errorf("containers[%d]: name is required", i)
		}
		if seen[c.Name] {
			return fmt.Errorf("containers[%d]: duplicate name %q", i, c.Name)
		}
		seen[c.Name] = true
		if !c.Disabled && strings.TrimSpace(c.Image) == "" {
			return fmt.Errorf("containers[%s]: image is required", c.Name)
		}
		if c.Restart != "" {
			switch c.Restart {
			case "no", "on-failure", "always", "unless-stopped":
			default:
				return fmt.Errorf("containers[%s]: invalid restart value %q", c.Name, c.Restart)
			}
		}
		if c.UpdatePolicy != "" {
			switch c.UpdatePolicy {
			case "auto", "manual":
			default:
				return fmt.Errorf("containers[%s]: invalid update_policy %q (must be auto or manual)", c.Name, c.UpdatePolicy)
			}
		}
		for j, fm := range c.Files {
			src := strings.TrimSpace(fm.Source)
			tgt := strings.TrimSpace(fm.Target)
			if src == "" {
				return fmt.Errorf("containers[%s].files[%d]: source is required", c.Name, j)
			}
			if !fileSeen[src] {
				return fmt.Errorf("containers[%s].files[%d]: source %q is not declared in top-level files", c.Name, j, src)
			}
			if tgt == "" {
				return fmt.Errorf("containers[%s].files[%d]: target is required", c.Name, j)
			}
			if !filepath.IsAbs(tgt) {
				return fmt.Errorf("containers[%s].files[%d]: target %q must be an absolute path inside the container", c.Name, j, tgt)
			}
			if fm.Mode != "" {
				if _, err := parseFileMode(fm.Mode); err != nil {
					return fmt.Errorf("containers[%s].files[%d]: %w", c.Name, j, err)
				}
			}
		}
	}

	// validate depends_on references
	for _, c := range s.Containers {
		for _, dep := range c.DependsOn {
			if !seen[dep] {
				return fmt.Errorf("containers[%s]: depends_on references unknown container %q", c.Name, dep)
			}
		}
	}

	netSeen := map[string]bool{}
	for i, n := range s.Networks {
		if strings.TrimSpace(n.Name) == "" {
			return fmt.Errorf("networks[%d]: name is required", i)
		}
		if netSeen[n.Name] {
			return fmt.Errorf("networks[%d]: duplicate name %q", i, n.Name)
		}
		netSeen[n.Name] = true
	}
	return nil
}
