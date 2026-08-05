package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

type Stack struct {
	Project    string      `yaml:"project"`
	Runtime    string      `yaml:"runtime,omitempty"`
	Socket     string      `yaml:"socket,omitempty"`
	DataPath   string      `yaml:"data_path,omitempty"`
	AuthFile   string      `yaml:"auth_file,omitempty"`
	Networks   []Network   `yaml:"networks,omitempty"`
	Containers []Container `yaml:"containers"`
	Serve      ServeConfig `yaml:"serve,omitempty"`
}

// ServeConfig controls behaviour of the "containerctl serve" web terminal.
// It is read once at server startup from the active stack file.
type ServeConfig struct {
	Exec ExecServeConfig   `yaml:"exec,omitempty"`
	Edit SimpleServeConfig `yaml:"edit,omitempty"`
	Use  SimpleServeConfig `yaml:"use,omitempty"`
}

// ExecServeConfig controls whether the web terminal may open interactive
// shell sessions inside containers and which containers are permitted.
//
//	serve:
//	  exec:
//	    enabled: true
//	    allowed:       # omit or leave empty to permit all containers
//	      - myapp
//	      - debug
type ExecServeConfig struct {
	// Enabled must be true to allow any exec command. Disabled by default
	// because exec gives full shell access to the container.
	Enabled bool `yaml:"enabled"`

	// Allowed is an optional allowlist of container names. When empty every
	// container may be exec'd into. When non-empty only the listed names are
	// permitted.
	Allowed []string `yaml:"allowed,omitempty"`
}

// SimpleServeConfig is a minimal on/off gate for browser features such as
// the stack file editor and the "use" stack-switch command.
//
//	serve:
//	  edit:
//	    enabled: true
//	  use:
//	    enabled: false
type SimpleServeConfig struct {
	Enabled bool `yaml:"enabled"`
}

type Network struct {
	Name   string            `yaml:"name"`
	Driver string            `yaml:"driver,omitempty"`
	Labels map[string]string `yaml:"labels,omitempty"`
}

// StringList is a YAML field that accepts either a single string or a list of
// strings (Compose-compatible). Example:
//
//	env_file: secrets.env
//	env_file:
//	  - secrets.env
//	  - override.env
type StringList []string

func (s *StringList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		var str string
		if err := value.Decode(&str); err != nil {
			return err
		}
		*s = StringList{str}
		return nil
	case yaml.SequenceNode:
		var list []string
		if err := value.Decode(&list); err != nil {
			return err
		}
		*s = StringList(list)
		return nil
	case yaml.AliasNode:
		if value.Alias == nil {
			return fmt.Errorf("invalid YAML alias")
		}
		return s.UnmarshalYAML(value.Alias)
	case 0:
		// empty / omitted node
		*s = nil
		return nil
	default:
		return fmt.Errorf("expected string or list of strings, got %v", value.Kind)
	}
}

// CommandList is a YAML field for command/entrypoint that accepts either a
// Compose-style string (shell-split into argv) or a list of strings (exec form).
//
//	command: serve --port 8080 --log-level info
//	command: ["serve", "--port", "8080"]
//	entrypoint: /app/start.sh
//	entrypoint: ["/app/start.sh"]
//
// String form splits on whitespace and honours single/double quotes; it does
// not invoke a shell (no pipes, redirects, or variable expansion beyond what
// containerctl already applies to the whole YAML at load time).
type CommandList []string

func (c *CommandList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		var str string
		if err := value.Decode(&str); err != nil {
			return err
		}
		if str == "" {
			*c = nil
			return nil
		}
		parts, err := splitCommand(str)
		if err != nil {
			return err
		}
		*c = CommandList(parts)
		return nil
	case yaml.SequenceNode:
		var list []string
		if err := value.Decode(&list); err != nil {
			return err
		}
		*c = CommandList(list)
		return nil
	case yaml.AliasNode:
		if value.Alias == nil {
			return fmt.Errorf("invalid YAML alias")
		}
		return c.UnmarshalYAML(value.Alias)
	case 0:
		*c = nil
		return nil
	default:
		return fmt.Errorf("expected string or list of strings, got %v", value.Kind)
	}
}

// splitCommand splits a Compose-style command string into argv, handling
// single- and double-quoted segments. Unbalanced quotes return an error.
func splitCommand(s string) ([]string, error) {
	var (
		parts   []string
		current []rune
		quote   rune // 0, '\'' or '"'
		escape  bool
	)
	for _, r := range s {
		if escape {
			current = append(current, r)
			escape = false
			continue
		}
		if r == '\\' && quote != '\'' {
			// Outside single quotes, backslash escapes the next character.
			escape = true
			continue
		}
		switch quote {
		case '\'':
			if r == '\'' {
				quote = 0
			} else {
				current = append(current, r)
			}
		case '"':
			if r == '"' {
				quote = 0
			} else {
				current = append(current, r)
			}
		default:
			switch {
			case r == '\'' || r == '"':
				quote = r
			case r == ' ' || r == '\t' || r == '\n' || r == '\r':
				if len(current) > 0 {
					parts = append(parts, string(current))
					current = current[:0]
				}
			default:
				current = append(current, r)
			}
		}
	}
	if escape {
		return nil, fmt.Errorf("invalid command string: trailing backslash")
	}
	if quote != 0 {
		return nil, fmt.Errorf("invalid command string: unmatched %q", string(quote))
	}
	if len(current) > 0 {
		parts = append(parts, string(current))
	}
	if len(parts) == 0 {
		return nil, nil
	}
	return parts, nil
}

type Container struct {
	Name           string            `yaml:"name"`
	Image          string            `yaml:"image"`
	Disabled       bool              `yaml:"disabled,omitempty"`
	UpdatePolicy   string            `yaml:"update_policy,omitempty"` // "" | "auto" | "manual"
	Command        CommandList       `yaml:"command,omitempty"`
	Entrypoint     CommandList       `yaml:"entrypoint,omitempty"`
	Restart        string            `yaml:"restart,omitempty"`
	Ports          []string          `yaml:"ports,omitempty"`
	Volumes        []string          `yaml:"volumes,omitempty"`
	Env            map[string]string `yaml:"env,omitempty"`
	EnvFile        StringList        `yaml:"env_file,omitempty"`
	Networks       []string          `yaml:"networks,omitempty"`
	NetworkAliases []string          `yaml:"network_aliases,omitempty"`
	Resources      Resources         `yaml:"resources,omitempty"`
	Healthcheck    *Healthcheck      `yaml:"healthcheck,omitempty"`
	Labels         map[string]string `yaml:"labels,omitempty"`
	User           string            `yaml:"user,omitempty"`
	WorkingDir     string            `yaml:"working_dir,omitempty"`
	Hostname       string            `yaml:"hostname,omitempty"`
	DNS            []string          `yaml:"dns,omitempty"`
	GroupAdd       []string          `yaml:"group_add,omitempty"`
	CapAdd         []string          `yaml:"cap_add,omitempty"`
	CapDrop        []string          `yaml:"cap_drop,omitempty"`
	Privileged     bool              `yaml:"privileged,omitempty"`
	SecurityOpt    []string          `yaml:"security_opt,omitempty"`
	ReadOnly       bool              `yaml:"read_only,omitempty"`
	Tmpfs          []string          `yaml:"tmpfs,omitempty"`
	DependsOn      []string          `yaml:"depends_on,omitempty"`
}

type Resources struct {
	CPUs      string `yaml:"cpus,omitempty"`
	Memory    string `yaml:"memory,omitempty"`
	PidsLimit int64  `yaml:"pids_limit,omitempty"`
}

type Healthcheck struct {
	Test     []string `yaml:"test,omitempty"`
	Interval string   `yaml:"interval,omitempty"`
	Timeout  string   `yaml:"timeout,omitempty"`
	Retries  int      `yaml:"retries,omitempty"`
	Start    string   `yaml:"start_period,omitempty"`
}

func (s *Stack) ContainerByName(name string) *Container {
	for i := range s.Containers {
		if s.Containers[i].Name == name {
			return &s.Containers[i]
		}
	}
	return nil
}

func (s *Stack) NetworkByName(name string) *Network {
	for i := range s.Networks {
		if s.Networks[i].Name == name {
			return &s.Networks[i]
		}
	}
	return nil
}

func ContainerFullName(project, name string) string {
	return project + "_" + name
}

func NetworkFullName(project, name string) string {
	return project + "_" + name
}
