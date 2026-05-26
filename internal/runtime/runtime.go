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

	ListImages(ctx context.Context) ([]ImageInfo, error)
	RemoveImage(ctx context.Context, id string, force bool) error
	ListVolumes(ctx context.Context, f Filters) ([]VolumeInfo, error)
	RemoveVolume(ctx context.Context, name string, force bool) error
	// VolumeSizes returns a map of volume name → size in bytes by querying the
	// daemon's disk-usage endpoint. Returns -1 for volumes whose size is unavailable.
	VolumeSizes(ctx context.Context) (map[string]int64, error)

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

	// EngineVersion returns version details of the container engine daemon.
	EngineVersion(ctx context.Context) (EngineInfo, error)

	// Exec runs a command in a running container and returns its exit code.
	Exec(ctx context.Context, id string, opts ExecOptions) (int, error)

	Name() string
	Ping(ctx context.Context) error
	Close() error
}

// EngineInfo holds version details returned by the container engine daemon.
type EngineInfo struct {
	Version       string // engine version, e.g. "28.5.2"
	APIVersion    string // API version, e.g. "1.47"
	MinAPIVersion string // minimum supported API version
	Platform      string // platform name, e.g. "Docker Engine - Community"
	OS            string // e.g. "linux"
	Arch          string // e.g. "amd64"
	KernelVersion string // host kernel version
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
	Networks       []string
	NetworkAliases []string
	Resources     Resources
	Healthcheck   *Healthcheck
	RestartPolicy string
	User          string
	WorkingDir    string
	Hostname      string
	DNS           []string
	GroupAdd      []string
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
	Labels   map[string]string
	Names    []string
	Dangling *bool // nil = no filter; true = unused/dangling only
}

type ContainerInfo struct {
	ID           string
	Name         string
	Image        string
	ImageID      string // full sha256 image ID (sha256:...)
	Mounts       []ContainerMount
	NetworkInfos []ContainerNetworkInfo
	State        string
	Labels       map[string]string
	StartedAt    time.Time
	ExitCode     int
	Ports        []PortBinding
	RestartCount int
	LastRestart  time.Time // time of last exit before a restart; zero if never restarted
	Resources    ContainerResources
}

type ContainerMount struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"read_only,omitempty"`
}

type ContainerNetworkInfo struct {
	Name      string `json:"name"`
	IPAddress string `json:"ip_address,omitempty"`
	Gateway   string `json:"gateway,omitempty"`
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
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Driver string            `json:"driver"`
	Labels map[string]string `json:"labels,omitempty"`
}

type ImageInfo struct {
	ID      string    `json:"id"`
	Tags    []string  `json:"tags"`
	Digest  string    `json:"digest,omitempty"`
	Size    int64     `json:"size"`
	Created time.Time `json:"created"`
}

type VolumeInfo struct {
	Name       string            `json:"name"               yaml:"name"`
	Driver     string            `json:"driver"             yaml:"driver"`
	Mountpoint string            `json:"mountpoint,omitempty" yaml:"mountpoint,omitempty"`
	// Size is nil when not fetched; -1 when the driver does not report usage.
	Size       *int64            `json:"size,omitempty"     yaml:"size,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"   yaml:"labels,omitempty"`
}

type LogOptions struct {
	Follow     bool
	Tail       int
	Timestamps bool
	Since      time.Time
}

// ExecOptions configures a container exec session.
type ExecOptions struct {
	Command     []string  // command to run; defaults to ["/bin/sh"] in the implementation
	Tty         bool      // allocate a pseudo-TTY
	Interactive bool      // keep stdin open
	Env         []string  // additional env vars as KEY=VALUE
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	StdinFd     uintptr // file descriptor of Stdin; used for window-size when Tty=true
}

type Healthcheck struct {
	Test        []string
	Interval    time.Duration
	Timeout     time.Duration
	StartPeriod time.Duration
	Retries     int
}
