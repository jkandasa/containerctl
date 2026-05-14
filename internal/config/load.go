package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func Load(path string) (*Stack, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	expanded := expandEnv(string(raw))
	var s Stack
	if err := yaml.Unmarshal([]byte(expanded), &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := validate(&s); err != nil {
		return nil, err
	}
	applyDefaults(&s)
	resolveVolumePaths(&s)
	if err := resolveEnvFiles(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

func expandEnv(s string) string {
	return os.Expand(s, func(key string) string {
		if key == "$" {
			return "$"
		}
		// ${VAR:-default} — use default if VAR is unset or empty
		if idx := strings.Index(key, ":-"); idx >= 0 {
			if val := os.Getenv(key[:idx]); val != "" {
				return val
			}
			return key[idx+2:]
		}
		return os.Getenv(key)
	})
}

func applyDefaults(s *Stack) {
	if s.Runtime == "" {
		s.Runtime = "docker"
	}
	for i := range s.Networks {
		if s.Networks[i].Driver == "" {
			s.Networks[i].Driver = "bridge"
		}
	}
	for i := range s.Containers {
		if s.Containers[i].Restart == "" {
			s.Containers[i].Restart = "unless-stopped"
		}
	}
}

// resolveVolumePaths prepends data_path to any relative host path in volumes.
// A path is considered relative when it does not start with '/'.
// "SRC:DST" → "<data_path>/SRC:DST"
func resolveVolumePaths(s *Stack) {
	if s.DataPath == "" {
		return
	}
	base, err := filepath.Abs(s.DataPath)
	if err != nil {
		base = s.DataPath
	}
	s.DataPath = base
	for i := range s.Containers {
		for j, vol := range s.Containers[i].Volumes {
			parts := strings.SplitN(vol, ":", 3)
			if len(parts) < 2 {
				continue
			}
			src := parts[0]
			if !filepath.IsAbs(src) {
				parts[0] = filepath.Join(base, src)
				s.Containers[i].Volumes[j] = strings.Join(parts, ":")
			}
		}
	}
}

func resolveEnvFiles(s *Stack) error {
	for i := range s.Containers {
		c := &s.Containers[i]
		if len(c.EnvFile) == 0 {
			continue
		}
		merged := make(map[string]string)
		for _, path := range c.EnvFile {
			if s.DataPath != "" && !filepath.IsAbs(path) {
				path = filepath.Join(s.DataPath, path)
			}
			pairs, err := parseEnvFile(path)
			if err != nil {
				return fmt.Errorf("containers[%s].env_file: %w", c.Name, err)
			}
			for k, v := range pairs {
				merged[k] = v
			}
		}
		// inline env overrides files
		for k, v := range c.Env {
			merged[k] = v
		}
		c.Env = merged
		c.EnvFile = nil
	}
	return nil
}

func parseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	out := make(map[string]string)
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			return nil, fmt.Errorf("%s line %d: expected KEY=VALUE", path, lineNo)
		}
		k := strings.TrimSpace(line[:idx])
		v := strings.TrimSpace(line[idx+1:])
		out[k] = v
	}
	return out, scanner.Err()
}
