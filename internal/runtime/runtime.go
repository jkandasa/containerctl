package runtime

import (
	"context"
	"io"
	"time"

	"github.com/jkandasa/containerctl/internal/registry"
)

const (
	LabelManaged     = "containerctl.managed"
	LabelProject     = "containerctl.project"
	LabelName        = "containerctl.name"
	LabelConfigHash  = "containerctl.config-hash"
	LabelSpecVersion = "containerctl.spec-version"

	SpecVersion = "1"
)

type Runtime interface {
	Pull(ctx context.Context, image string) error
	CreateContainer(ctx context.Context, spec ContainerSpec) (id string, err error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string, timeout time.Duration) error
	RemoveContainer(ctx context.Context, id string, force bool) error

	InspectContainer(ctx context.Context, nameOrID string) (*ContainerInfo, error)
	ListContainers(ctx context.Context, filters Filters) ([]ContainerInfo, error)
	Logs(ctx context.Context, id string, opts LogOptions) (io.ReadCloser, error)

	CreateNetwork(ctx context.Context, spec NetworkSpec) (id string, err error)
	RemoveNetwork(ctx context.Context, nameOrID string) error
	ListNetworks(ctx context.Context, filters Filters) ([]NetworkInfo, error)
	NetworkExists(ctx context.Context, name string) (bool, error)

	// LocalImageMeta returns digest and size of the image in the local cache.
	// Returns zero values if the image has not been pulled yet.
	LocalImageMeta(ctx context.Context, image string) (ImageMeta, error)
	// RemoteImageDigest queries the registry for the current digest of image.
	RemoteImageDigest(ctx context.Context, image string) (string, error)
	// CheckTagUpdates queries the registry for semver tags newer than the one in image.
	// Credentials are resolved from the runtime's configured auth sources.
	CheckTagUpdates(ctx context.Context, image string, max int) (*registry.TagUpdates, error)
	// ContainerStats returns a single live usage snapshot for the container.
	ContainerStats(ctx context.Context, id string) (ContainerUsage, error)

	Name() string
	Ping(ctx context.Context) error
	Close() error
}

type ContainerSpec struct {
	Name          string
	Image         string
	Command       []string
	Entrypoint    []string
	Env           map[string]string
	Labels        map[string]string
	Ports         []PortBinding
	Mounts        []Mount
	Networks      []string
	Resources     Resources
	Healthcheck   *Healthcheck
	RestartPolicy string
	User          string
	WorkingDir    string
	Hostname      string
	DNS           []string
	CapAdd        []string
	CapDrop       []string
	Privileged    bool
	SecurityOpt   []string
	ReadOnly      bool
	Tmpfs         []string
}

type PortBinding struct {
	HostIP        string
	HostPort      string
	ContainerPort string
	Protocol      string
}

type Mount struct {
	Type     string // bind | volume | tmpfs
	Source   string
	Target   string
	ReadOnly bool
}

type Resources struct {
	NanoCPUs    int64
	MemoryBytes int64
	PidsLimit   int64
}

type Filters struct {
	Labels map[string]string
	Names  []string
}

type ContainerInfo struct {
	ID           string
	Name         string
	Image        string
	State        string
	Labels       map[string]string
	StartedAt    time.Time
	ExitCode     int
	Ports        []PortBinding
	RestartCount int
	LastRestart  time.Time // time of last exit before a restart; zero if never restarted
	Resources    ContainerResources
}

type ContainerResources struct {
	NanoCPUs    int64
	MemoryBytes int64
	PidsLimit   int64
}

type ImageMeta struct {
	Digest string
	Size   int64 // bytes; 0 if unavailable
}

type ContainerUsage struct {
	CPUPercent  float64
	MemoryUsed  int64 // bytes; working-set (cache excluded)
}

type NetworkSpec struct {
	Name   string
	Driver string
	Labels map[string]string
}

type NetworkInfo struct {
	ID     string
	Name   string
	Driver string
	Labels map[string]string
}

type LogOptions struct {
	Follow     bool
	Tail       int
	Timestamps bool
	Since      time.Time
}

type Healthcheck struct {
	Test        []string
	Interval    time.Duration
	Timeout     time.Duration
	StartPeriod time.Duration
	Retries     int
}
