package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultStackFile is used when neither -f/--file nor a saved `stack` path is set.
const DefaultStackFile = "stack.yaml"

// UserConfig holds CLI preferences that apply across invocations (not per-project).
// Stored at $XDG_CONFIG_HOME/containerctl/config.json (or ~/.config/...).
type UserConfig struct {
	// File is the absolute path of the default stack YAML selected by `containerctl stack`.
	File string `json:"file,omitempty"`
}

// LoadUserConfig reads the user config file. Missing file is not an error.
func LoadUserConfig() (*UserConfig, error) {
	p, err := userConfigPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return &UserConfig{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read user config %s: %w", p, err)
	}
	var c UserConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse user config %s: %w", p, err)
	}
	return &c, nil
}

// SaveUserConfig writes the user config file (mode 0600).
func SaveUserConfig(c *UserConfig) error {
	p, err := userConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o600)
}

// CurrentStackPath returns the absolute path saved by `containerctl stack`, or
// empty string if none is set.
func CurrentStackPath() (string, error) {
	c, err := LoadUserConfig()
	if err != nil {
		return "", err
	}
	return c.File, nil
}

// SetCurrentStackPath stores path as the default stack file. path is made
// absolute. The file must already exist.
func SetCurrentStackPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path %s: %w", path, err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file not found: %s", path)
		}
		return "", fmt.Errorf("stat %s: %w", abs, err)
	}
	if st.IsDir() {
		return "", fmt.Errorf("%s is a directory, not a stack file", abs)
	}

	c, err := LoadUserConfig()
	if err != nil {
		return "", err
	}
	c.File = abs
	if err := SaveUserConfig(c); err != nil {
		return "", err
	}
	return abs, nil
}

// ClearCurrentStackPath removes the saved default so the built-in default
// (stack.yaml) is used again.
func ClearCurrentStackPath() error {
	c, err := LoadUserConfig()
	if err != nil {
		return err
	}
	if c.File == "" {
		return nil
	}
	c.File = ""
	return SaveUserConfig(c)
}

// ResolveStackFile picks the stack path for a command.
// Priority: explicit -f/--file > saved `stack` path > DefaultStackFile.
func ResolveStackFile(explicit string, fileFlagChanged bool) (string, error) {
	if fileFlagChanged {
		if explicit == "" {
			return DefaultStackFile, nil
		}
		return explicit, nil
	}
	if p, err := CurrentStackPath(); err != nil {
		return "", err
	} else if p != "" {
		return p, nil
	}
	if explicit != "" {
		return explicit, nil
	}
	return DefaultStackFile, nil
}

// UserConfigPath returns the path of the user config file (exported for tests/docs).
func UserConfigPath() (string, error) {
	return userConfigPath()
}

func userConfigPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "containerctl", "config.json"), nil
}
