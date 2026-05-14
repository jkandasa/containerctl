package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type State struct {
	Disabled []string `json:"disabled,omitempty"`
	path     string
}

func Load(project string) (*State, error) {
	p, err := filePath(project)
	if err != nil {
		return nil, err
	}
	s := &State{path: p}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state %s: %w", p, err)
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parse state %s: %w", p, err)
	}
	return s, nil
}

func (s *State) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	sort.Strings(s.Disabled)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, append(data, '\n'), 0o644)
}

func (s *State) IsDisabled(name string) bool {
	for _, n := range s.Disabled {
		if n == name {
			return true
		}
	}
	return false
}

func (s *State) Disable(name string) {
	if !s.IsDisabled(name) {
		s.Disabled = append(s.Disabled, name)
	}
}

func (s *State) Enable(name string) {
	out := s.Disabled[:0]
	for _, n := range s.Disabled {
		if n != name {
			out = append(out, n)
		}
	}
	s.Disabled = out
}

// DisabledSet returns a set of disabled container names for O(1) lookup.
func (s *State) DisabledSet() map[string]bool {
	m := make(map[string]bool, len(s.Disabled))
	for _, n := range s.Disabled {
		m[n] = true
	}
	return m
}

// Path returns the path to the state file for this project.
func Path(project string) (string, error) {
	return filePath(project)
}

func filePath(project string) (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "containerctl", project, "state.json"), nil
}
