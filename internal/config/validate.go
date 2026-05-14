package config

import (
	"fmt"
	"strings"
)

func validate(s *Stack) error {
	if strings.TrimSpace(s.Project) == "" {
		return fmt.Errorf("project name is required")
	}
	if len(s.Containers) == 0 {
		return fmt.Errorf("at least one container is required")
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
