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
	// Resolve relative paths against the stack file's directory so
	// `containerctl -f /path/to/stack.yaml` works regardless of CWD.
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	stackDir := filepath.Dir(absPath)

	applyDefaults(&s)
	resolveVolumePaths(&s, stackDir)
	if err := resolveEnvFiles(&s, stackDir); err != nil {
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
// A path is considered relative when it is not absolute.
// "SRC:DST" → "<data_path>/SRC:DST"
//
// Relative data_path values are resolved against stackDir (the directory
// containing the stack YAML), not the process CWD.
func resolveVolumePaths(s *Stack, stackDir string) {
	if s.DataPath == "" {
		return
	}
	base := s.DataPath
	if !filepath.IsAbs(base) {
		base = filepath.Join(stackDir, base)
	}
	if abs, err := filepath.Abs(base); err == nil {
		base = abs
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

// resolveEnvFiles reads each container's env_file entries, merges them into
// Env (later files override earlier ones; inline env overrides files), then
// clears EnvFile so the resolved map is the single source of truth for hashing
// and container creation.
//
// Relative paths are resolved against data_path when set, otherwise against
// stackDir (the directory containing the stack YAML). Absolute paths are left
// unchanged.
func resolveEnvFiles(s *Stack, stackDir string) error {
	for i := range s.Containers {
		c := &s.Containers[i]
		if len(c.EnvFile) == 0 {
			continue
		}
		merged := make(map[string]string)
		for _, path := range c.EnvFile {
			if path == "" {
				return fmt.Errorf("containers[%s].env_file: empty path", c.Name)
			}
			if !filepath.IsAbs(path) {
				if s.DataPath != "" {
					path = filepath.Join(s.DataPath, path)
				} else {
					path = filepath.Join(stackDir, path)
				}
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

// parseEnvFile reads a Docker-style env file (KEY=VALUE lines).
//
// Rules (aligned with docker run --env-file / Compose env_file):
//   - Blank lines and lines starting with # are ignored
//   - Leading whitespace on the line is ignored
//   - KEY=VALUE sets KEY to VALUE (value may be empty)
//   - KEY without '=' is filled from the host environment when set; otherwise
//     the key is omitted (same as Docker)
//   - Surrounding single or double quotes on the value are stripped
//   - An optional "export " prefix on the line is accepted
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
		// Accept shell-style "export KEY=VALUE"
		if rest, ok := strings.CutPrefix(line, "export "); ok {
			line = strings.TrimSpace(rest)
			if line == "" {
				continue
			}
		}
		k, v, hasValue := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if k == "" {
			return nil, fmt.Errorf("%s line %d: empty variable name", path, lineNo)
		}
		if strings.ContainsAny(k, " \t") {
			return nil, fmt.Errorf("%s line %d: variable name %q contains whitespace", path, lineNo, k)
		}
		if !hasValue {
			// KEY alone → pass through from host env when present
			if host, ok := os.LookupEnv(k); ok {
				out[k] = host
			}
			continue
		}
		// Trim surrounding whitespace, then strip matching quotes.
		v = strings.TrimSpace(v)
		v = unquoteEnvValue(v)
		out[k] = v
	}
	return out, scanner.Err()
}

// unquoteEnvValue strips a single layer of matching single or double quotes.
// Unmatched quotes are left as-is (the value is used literally).
func unquoteEnvValue(v string) string {
	if len(v) < 2 {
		return v
	}
	if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
		return v[1 : len(v)-1]
	}
	return v
}
