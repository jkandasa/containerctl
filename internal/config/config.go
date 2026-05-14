package config

type Stack struct {
	Project    string      `yaml:"project"`
	Runtime    string      `yaml:"runtime,omitempty"`
	DataPath   string      `yaml:"data_path,omitempty"`
	AuthFile   string      `yaml:"auth_file,omitempty"`
	Networks   []Network   `yaml:"networks,omitempty"`
	Containers []Container `yaml:"containers"`
}

type Network struct {
	Name   string            `yaml:"name"`
	Driver string            `yaml:"driver,omitempty"`
	Labels map[string]string `yaml:"labels,omitempty"`
}

type Container struct {
	Name        string            `yaml:"name"`
	Image       string            `yaml:"image"`
	Disabled      bool              `yaml:"disabled,omitempty"`
	UpdatePolicy  string            `yaml:"update_policy,omitempty"` // "" | "auto" | "manual"
	Command     []string          `yaml:"command,omitempty"`
	Entrypoint  []string          `yaml:"entrypoint,omitempty"`
	Restart     string            `yaml:"restart,omitempty"`
	Ports       []string          `yaml:"ports,omitempty"`
	Volumes     []string          `yaml:"volumes,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
	EnvFile     []string          `yaml:"env_file,omitempty"`
	Networks    []string          `yaml:"networks,omitempty"`
	Resources   Resources         `yaml:"resources,omitempty"`
	Healthcheck *Healthcheck      `yaml:"healthcheck,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	User        string            `yaml:"user,omitempty"`
	WorkingDir  string            `yaml:"working_dir,omitempty"`
	Hostname    string            `yaml:"hostname,omitempty"`
	DNS         []string          `yaml:"dns,omitempty"`
	CapAdd      []string          `yaml:"cap_add,omitempty"`
	CapDrop     []string          `yaml:"cap_drop,omitempty"`
	Privileged  bool              `yaml:"privileged,omitempty"`
	ReadOnly    bool              `yaml:"read_only,omitempty"`
	Tmpfs       []string          `yaml:"tmpfs,omitempty"`
	DependsOn   []string          `yaml:"depends_on,omitempty"`
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
