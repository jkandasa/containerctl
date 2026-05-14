# containerctl — Specification

A single static Go binary plus a single YAML file that declaratively manages all containers on one host. Replaces a pile of per-container shell scripts with one source of truth and a reconciliation loop.

---

## 1. Goals and non-goals

### Goals

- **Single binary, single YAML.** No daemon, no separate state store.
- **Declarative.** YAML is the desired state; the host is reconciled toward it.
- **Per-container scope.** Every operation can target one container by name or the whole stack.
- **Two runtimes from day one.** Docker (official SDK) and Podman (via its Docker-compatible API socket), behind a `Runtime` interface.
- **Drift visibility.** `diff` and `status` show exactly what will change before `apply` runs.
- **Safe by default.** Only ever touches containers it owns (identified by a label).

### Non-goals (v1)

- Multi-host orchestration.
- Image building.
- Secret management beyond env / env_file.
- Blue/green or rolling deploys — recreate is the only upgrade strategy.
- Healthcheck-gated rollout between containers.

### Why not just docker-compose

Compose handles "bring stack up" well. It is weak at per-container upgrade, drift detection, and stateless reconciliation. containerctl fills exactly that gap; it is not a Compose rewrite.

---

## 2. Design principles

1. **Stateless tool.** State lives on the containers themselves (as labels) and in a small JSON file for disabled state. Wiping `~/.local/share/containerctl/` must not break reconciliation.
2. **Recreate, don't patch.** When config changes, stop → remove → run. Predictable, matches today's shell-script behavior, no in-place mutation surprises.
3. **Hash-driven reconciliation.** Each container's normalized config hashes to a SHA-256. Stored as a label. `apply` compares declared hash to running hash; equal → skip, different → recreate, missing → create.
4. **Never touch unmanaged containers.** Anything without the `containerctl.managed=true` label is invisible to the tool.
5. **Explicit project namespace.** Every managed container is named `<project>_<logical-name>`. Two projects on the same host cannot collide.

---

## 3. Project layout

```
containerctl/
├── docs/SPEC.md            # this file
├── go.mod
├── main.go                 # thin entrypoint; calls cmd.Execute()
├── cmd/                    # cobra commands, one file per subcommand
│   ├── root.go
│   ├── apply.go
│   ├── diff.go
│   ├── status.go
│   ├── upgrade.go
│   ├── restart.go          # stop → remove → create → start
│   ├── check_update.go     # registry update check; --apply for patch/digest updates
│   ├── down.go
│   ├── logs.go
│   ├── pull.go
│   ├── stop.go             # transient stop (next apply restarts)
│   ├── start.go            # transient start
│   ├── disable.go          # persistent off via state file
│   ├── enable.go           # removes from state file, re-reconciles
│   └── version.go
├── internal/
│   ├── config/             # YAML load + validate + normalize + hash
│   │   ├── config.go       # Stack, Container, Network, Resources, Healthcheck types
│   │   ├── load.go         # Load(path) (*Stack, error); ${VAR} expansion, data_path resolution
│   │   ├── validate.go     # required fields, duplicates, port/volume syntax
│   │   ├── hash.go         # canonical JSON of normalized container → SHA-256
│   │   └── update.go       # UpdateContainerImage — in-place tag rewrite of stack.yaml
│   ├── registry/           # OCI registry queries (no daemon dependency)
│   │   ├── digest.go       # RemoteDigest — Bearer-auth HEAD request for content digest
│   │   └── tags.go         # CheckTagUpdates — semver-aware newer-tag discovery
│   ├── runtime/            # runtime abstraction
│   │   ├── runtime.go      # Runtime interface + shared types
│   │   ├── docker/         # docker SDK implementation
│   │   └── podman/         # podman implementation (Docker-compat socket first)
│   ├── reconcile/          # diff plan + apply
│   │   ├── plan.go         # Plan, Action (Create|Recreate|Skip|Remove|Disabled|DeclaredOff)
│   │   └── reconcile.go    # Apply(ctx, plan, runtime, w) — streams per-container status
│   ├── state/              # file-based persistent disabled state
│   │   └── state.go        # Load/Save/IsDisabled/Disable/Enable/DisabledSet
│   └── render/             # human + json output for status/diff/plan
│       └── render.go
├── examples/
│   └── stack.yaml
└── Makefile                # build, test, lint, install
```

### Dependencies

- `github.com/spf13/cobra` — CLI.
- `gopkg.in/yaml.v3` — YAML.
- `github.com/docker/docker/client` — Docker SDK. Also used against Podman's Docker-compatible socket.
- `github.com/stretchr/testify` — test assertions.

`internal/registry` has no external dependencies — it uses the Go standard library only (net/http, encoding/json) to talk directly to OCI-compatible registries.

Avoid `github.com/containers/podman/v5/pkg/bindings` in v1. Heavy transitive deps; Podman's Docker-compat API is sufficient for the surface area we use.

---

## 4. YAML schema

### Top level

```yaml
project: string        # required. Namespace for all managed objects.
runtime: docker|podman # optional. Default: docker. Overridable via --runtime flag.
data_path: string      # optional. Base directory for relative volume sources and env_file paths.
                       # Relative data_path values are resolved to absolute using the CWD at load time.
networks:              # optional. Networks managed by containerctl.
  - name: string       # required.
    driver: string     # optional. Default: bridge.
    labels: map        # optional.
containers:            # required, non-empty.
  - ...                # see below.
```

### Container

```yaml
- name: string         # required. Logical name. Final container name = "<project>_<name>".
  image: string        # required. e.g. "postgres:16" or "registry.example.com/app:v1.2.3".
  disabled: bool       # optional. Default: false. When true, apply removes the container (if
                       # present) and skips creation. See §6 "Disabling containers".
  update_policy: string # optional. auto|manual. Default: auto. When "manual", check-update skips
                        # this container entirely — no registry query, no --apply action. Use for
                        # images you intentionally hold at a specific version.
  command: [string]    # optional. Overrides image CMD.
  entrypoint: [string] # optional. Overrides image ENTRYPOINT.
  restart: string      # optional. no|on-failure|always|unless-stopped. Default: unless-stopped.
  ports:               # optional. "HOST:CONTAINER" or "HOST:CONTAINER/proto" or "IP:HOST:CONTAINER".
    - "5432:5432"
    - "127.0.0.1:9090:9090"
    - "53:53/udp"
  volumes:             # optional. "SRC:DST[:MODE]". Relative SRC values are prefixed with data_path.
    - "/srv/pg:/var/lib/postgresql/data"
    - "pgdata:/var/lib/postgresql/data:rw"
    - "myservice/config.yaml:/app/config.yaml"  # → <data_path>/myservice/config.yaml
  env:                 # optional. Inline env vars. Overrides env_file values.
    KEY: value
  env_file: [string]   # optional. Paths to env files. Relative paths are resolved against data_path.
                       # Later entries override earlier ones; inline env overrides files.
    - "myservice/secrets.env"  # → <data_path>/myservice/secrets.env
  networks: [string]   # optional. Names from top-level networks: section, or pre-existing networks.
  resources:           # optional. Resource limits.
    cpus: "2.0"        # CPU shares as a decimal string.
    memory: "2g"       # 512m, 2g, etc.
    pids_limit: 200    # optional int.
  healthcheck:         # optional. Declared but not gated on in v1.
    test: ["CMD", "pg_isready"]
    interval: 10s
    timeout: 3s
    retries: 5
    start_period: 30s
  labels: map          # optional. User labels merged with containerctl-managed labels.
  user: string         # optional. UID[:GID]. Supports ${UID}:${GID} env expansion.
  working_dir: string  # optional.
  hostname: string     # optional.
  dns: [string]        # optional.
  cap_add: [string]    # optional.
  cap_drop: [string]   # optional.
  privileged: bool     # optional. Default: false.
  read_only: bool      # optional. Read-only root filesystem.
  tmpfs: [string]      # optional. Mount tmpfs at given paths.
  depends_on: [string] # optional. Logical names. Affects start order only; not a healthcheck gate in v1.
```

### data_path resolution

When `data_path` is set, any relative `SRC` in `volumes` and any relative path in `env_file` are automatically prefixed with the resolved absolute value of `data_path`. Absolute paths are left unchanged.

```yaml
data_path: ./data

containers:
  - name: myservice
    volumes:
      - "myservice/db:/var/lib/db"      # → /abs/path/to/data/myservice/db:/var/lib/db
      - "/external/mount:/ext"           # unchanged — already absolute
    env_file:
      - "myservice/secrets.env"          # → /abs/path/to/data/myservice/secrets.env
```

### Variable expansion

`$VAR` and `${VAR}` in any string value are expanded from the host environment at load time. Undefined variables expand to empty string.

`$$` is an escape sequence that produces a literal `$` without triggering expansion. This lets you pass shell-style parameter defaults through to the container unchanged:

```yaml
command:
  - "--log-level=$${LOG_LEVEL:-info}"   # container receives: --log-level=${LOG_LEVEL:-info}
```

Implemented via `os.Expand` with a custom mapping (`$` → `$`) rather than `os.ExpandEnv`.

---

## 5. CLI surface

All commands accept `-f, --file PATH` (default: `./stack.yaml`) and `--runtime docker|podman` (overrides YAML).

| Command                                          | Purpose                                                                                                                                   | Exit codes                               |
| ------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------- |
| `containerctl apply [name...]`                   | Reconcile host to YAML. With names, only those containers are affected. Orphaned containers/networks and unrelated network creation are skipped; run without names for a full cleanup. Streams per-container status as each action completes. | 0 ok, 1 error, 2 partial failure         |
| `containerctl diff [name...]`                    | Show planned actions without making changes.                                                                                              | 0 no changes, 3 changes pending, 1 error |
| `containerctl status [name...]`                  | Show all managed containers, their state, drift, and uptime.                                                                              | 0 ok, 1 error                            |
| `containerctl check-update [name...] [--apply]`  | Query the registry for updates. Semver tags: shows patch/minor and major updates separately. Floating tags: compares local vs remote digest. `--apply` pulls and recreates containers with patch/minor updates or digest changes; major-version bumps require manual tag edit in stack.yaml. Skips containers with `disabled: true` in YAML and those with `update_policy: manual`. | 0 ok, 1 error |
| `containerctl upgrade <name>`                    | Force-pull and recreate one container even if config hash is unchanged. Use for floating tags (e.g. `:latest`).                           | 0 ok, 1 error                            |
| `containerctl restart [name...] [--all]`         | Stop, remove, recreate, and start containers from current config without pulling a new image.                                             | 0 ok, 1 error                            |
| `containerctl pull [name...]`                    | Pull images without reconciling. Skips containers with `disabled: true` in YAML.                                                          | 0 ok, 1 error                            |
| `containerctl down [name...]`                    | Stop and remove managed containers. With no args, the whole project.                                                                      | 0 ok, 1 error                            |
| `containerctl stop [name...] [--all]`            | **Transient** stop. Container kept on disk. Next `apply` restarts it. Requires at least one name or `--all`.                              | 0 ok, 1 error                            |
| `containerctl start [name...] [--all]`           | Start a previously-stopped managed container without reconciling. Refuses if persistently disabled — run `enable` first. Requires at least one name or `--all`. | 0 ok, 1 error                            |
| `containerctl disable <name...>`                 | **Persistent** off. Stops the container and records it in the project state file. Survives reboots and `apply`. Container is not removed. | 0 ok, 1 error                            |
| `containerctl enable <name...>`                  | Removes from state file and reconciles the container (recreates if hash drifted, else starts).                                            | 0 ok, 1 error                            |
| `containerctl logs <name> [--follow] [--tail N]` | Stream container logs. Note: `--follow` has no `-f` shorthand (conflicts with global `-f/--file`).                                       | 0 ok, 1 error                            |
| `containerctl version`                           | Print binary version, build date, Go version, OS/arch, and runtime reachability.                                                         | 0                                        |

### `restart` vs `upgrade` vs `apply` vs `check-update --apply`

| Command                  | Pulls image        | Recreates always | Hash-driven | Updates stack.yaml |
| ------------------------ | ------------------ | ---------------- | ----------- | ------------------ |
| `apply`                  | yes (new)          | no               | yes         | no                 |
| `upgrade`                | yes (force)        | yes              | no          | no                 |
| `restart`                | no                 | yes              | no          | no                 |
| `check-update --apply`   | yes (new tag/same) | yes              | no          | yes (patch only)   |

Use `restart` when you want a clean container from the cached image without any network pull (e.g. after editing a mounted config file). Use `upgrade` when the image tag floats and you need the latest pull. Use `check-update --apply` for automated patch/minor version upgrades — it rewrites the image tag in stack.yaml before recreating so the change is persistent.

### `check-update` update detection

For **semver-tagged images** (e.g. `nginx:1.27.0`, `redis:7.2-alpine`), the check is pure registry-based and produces the same output regardless of which runtime is in use:

| Status        | Meaning                                                          | `--apply` |
| ------------- | ---------------------------------------------------------------- | --------- |
| `up-to-date`  | No newer tags in registry                                        | skip      |
| `patch update`| Newer tags in same major (e.g. `1.27.1`, `1.28.0`)              | apply — rewrites stack.yaml tag, pulls, recreates |
| `major update`| Only higher major versions found (e.g. `2.0.0`)                 | skip — manual tag change required (breaking changes likely) |
| `patch+major` | Both same-major patches and higher-major versions exist          | apply patch; major shown in NOTE for awareness |
| `manual`      | Container has `update_policy: manual` in YAML                    | skip — no registry call made |

For **floating tags** (e.g. `:latest`, `:master`, `:edge`), the check compares the locally cached digest against the registry:

| Status          | Meaning                                     | `--apply` |
| --------------- | ------------------------------------------- | --------- |
| `digest changed`| Remote digest differs from local cache      | pull same tag, recreate |
| `up-to-date`    | Digest matches                              | skip      |
| `not pulled`    | Image not in local cache                    | skip      |
| `manual`        | Container has `update_policy: manual`       | skip — no registry call made |

Tag family matching uses the non-numeric suffix: `2.1.2-alpine` and `2.2.0-alpine` are in the same family (`-alpine`); `2.2.0` and `sha256-abc` are not. Non-semver candidates (SHA digests, `testing-*`, bare words) are always excluded.

### Three states of "off"

| Need                               | Mechanism                 | Persistence                                   | What happens on disk                        |
| ---------------------------------- | ------------------------- | --------------------------------------------- | ------------------------------------------- |
| "Kill it briefly"                  | `stop <name>`             | Transient — next `apply` restarts             | Container kept, stopped                     |
| "Off until I say otherwise"        | `disable <name>`          | Persistent via state file (host-local)        | Container kept, stopped, skipped by planner |
| "Not running this service for now" | `disabled: true` in YAML  | Persistent in config (tracked in git)         | Container fully removed; planner ignores    |

Pick the most declarative one that fits: prefer `disabled: true` in YAML for anything you want auditable; use `disable` for ad-hoc host-level decisions you don't want in the committed file; use `stop` only as a transient.

### Global flags

- `-f, --file PATH` — YAML path. Default `./stack.yaml`.
- `--runtime docker|podman` — override YAML's `runtime:`.
- `--socket PATH` — override default runtime socket (e.g. `/run/user/1000/podman/podman.sock`).
- `-o, --output text|json` — output format. Default text.
- `--no-color` — disable ANSI.
- `-v, --verbose` — debug logs to stderr.
- `--project NAME` — override YAML's `project:` (use with care; affects which containers are considered managed).

### Output: `diff` / `apply` plan format

```
Project: home-services

Networks:
  + create   backend

Containers:
  + create    postgres             (image: postgres:16)
  ~ recreate  app                  (image changed)
  = skip      redis                (no changes)
  ! disabled  backups              (disabled via state file; skipped)
  x off       old-app              (disabled: true in YAML; not present)
```

### Output: `apply` streaming execution

After printing the plan, `apply` streams one line per container immediately as each action completes:

```
  postgres              created   → running
  app                   recreated → running
  redis                 skip
  backups               disabled
  old-app               off
```

Networks are printed similarly (`network <name>  created` / `network <name>  removed`).

### Output: `status` text format

```
NAME       STATE         IMAGE            UPTIME    DRIFT
postgres   running       postgres:16      4d 2h     no
nginx      running       nginx:1.27       4d 2h     yes (image, env)
redis      stopped       redis:7.2        -         -
backups    disabled      restic:0.16      -         -
old-app    declared-off  -                -         -
```

State values: `running`, `stopped` (exited — apply will restart), `disabled` (in state file — apply skips), `declared-off` (YAML `disabled: true`, no container present), `missing` (in YAML, not on host — apply will create).

---

## 6. Reconciliation

### Labels written on every managed container

| Label                       | Value            | Purpose                                                                                             |
| --------------------------- | ---------------- | --------------------------------------------------------------------------------------------------- |
| `containerctl.managed`      | `true`           | Marks a container as owned by containerctl. Anything without this label is never touched.           |
| `containerctl.project`      | `<project>`      | Namespace key. `status`/`down`/`apply` filter by this.                                              |
| `containerctl.name`         | `<logical-name>` | Stable logical id (the YAML name, not the full container name).                                     |
| `containerctl.config-hash`  | `sha256:<hex>`   | Hash of the normalized container spec. Drives recreate decisions.                                   |
| `containerctl.spec-version` | `1`              | Schema version of the labels themselves. Lets us migrate later.                                     |

Networks managed by containerctl are similarly labelled: `containerctl.managed=true`, `containerctl.project=<project>`, `containerctl.name=<network-name>`.

### Persistent disabled state (state file)

The "persistently disabled" set is stored in a JSON file, not as a container label. This avoids the need to recreate a container just to toggle its disabled state.

- Path: `$XDG_DATA_HOME/containerctl/<project>/state.json` (fallback: `~/.local/share/containerctl/<project>/state.json`)
- Format: `{"disabled": ["name1", "name2"]}`
- Written by `containerctl disable`, cleared by `containerctl enable`.
- Survives `apply`, reboots, and container removal (it is separate from the container).

### Config hash inputs

Normalize the container spec before hashing so semantically-equivalent YAML produces the same hash:

- Sort map keys (env, labels).
- Sort slices that have no ordering semantics (cap_add, cap_drop, dns, networks).
- Do **not** sort `command`, `entrypoint`, `ports`, `volumes`, `depends_on` — order matters.
- Resolve `env_file` contents into the env map (so editing an env file invalidates the hash).
- Image: keep as-written, including tag. Do not resolve to digest. (Image-resolve-to-digest is a future enhancement; see §10.)
- Exclude user-supplied `labels` keyed under `containerctl.*` (paranoia: never let user override our keys).
- Exclude the `disabled` field from the hash. Toggling `disabled` should not cause a recreate when the user later re-enables — the planner branches on `disabled` _before_ consulting the hash.

Serialize the normalized struct as canonical JSON (sorted keys, no whitespace) → SHA-256 → hex.

### Plan algorithm

For each container `c` in YAML:

1. Look up the container by full name `<project>_<c.name>` AND label `containerctl.managed=true`.
2. **If YAML has `disabled: true`:**
   - Container found → `Remove`.
   - Container not found → `Skip` (state: `declared-off`).
3. **Else if container name is in the project state file (persistently disabled):** → `Skip` (state: `disabled`). The hash is not consulted. The container is left stopped on disk.
4. **Else if container is not found:** → `Create`.
5. **Else if `containerctl.config-hash` label equals computed hash:** → `Skip`.
6. **Else (hash differs):** → `Recreate` (stop, remove, create, start).

After processing the YAML list, **and only when no name filter is active** (full apply): list all containers with `containerctl.project=<project>` and `containerctl.managed=true`. Any whose logical name is not in the YAML → `Remove`. Partial applies (names provided) never remove orphaned containers — they only affect the explicitly named containers.

Networks follow the same algorithm with simpler inputs (name, driver, labels). Networks have no `disabled` concept. Orphaned networks are only removed during a full apply (no name filter); partial applies never remove networks.

### Dependency validation

After building the plan, walk `depends_on` edges:

- If an enabled container depends on a container that is either `disabled: true` in YAML or in the state file → **warning** on stderr (not error). Sometimes intentional (e.g. disabling a sidecar the main service can tolerate missing).
- If `depends_on` references a logical name not present in YAML at all → **error**.

### Disabling containers — operator workflows

**Persistent off via state file (host-local, not in git):**

```
$ containerctl disable backups
  backups    stopped
disabled backups

$ containerctl apply          # skips backups regardless of YAML changes

$ containerctl enable backups
  backups    created   → running
enabled backups
```

**Persistent off via YAML (auditable, in git):**

```yaml
- name: old-app
  image: app:v1.2.0
  disabled: true  # next apply will remove the running container
```

**Transient stop (quick troubleshoot):**

```
$ containerctl stop nginx
  nginx                 stopping...
  nginx                 stopped

$ # poke at things…
$ containerctl start nginx     # OR: containerctl apply (will restart it)
  nginx                 starting...
  nginx                 started   → running
```

**Interaction matrix:**

| YAML `disabled`  | State file disabled | Container exists  | Plan                                                                                              |
| ---------------- | ------------------- | ----------------- | ------------------------------------------------------------------------------------------------- |
| `false` / absent | no                  | no                | Create                                                                                            |
| `false` / absent | no                  | yes, hash matches | Skip                                                                                              |
| `false` / absent | no                  | yes, hash differs | Recreate                                                                                          |
| `false` / absent | yes                 | yes               | Skip (disabled via state file)                                                                    |
| `false` / absent | yes                 | no                | Skip (state file entry remains; if container was removed externally, apply will not recreate it)  |
| `true`           | no                  | yes               | Remove                                                                                            |
| `true`           | no                  | no                | Skip (declared-off)                                                                               |
| `true`           | yes                 | yes               | Remove (YAML wins; also clears state file entry)                                                  |

### Execution order

1. Pull all images that will be used by `Create` or `Recreate` actions (parallel, bounded by worker count).
2. Create/reconcile networks before containers that reference them.
3. Topo-sort containers by `depends_on`. Cycle → error.
4. Execute actions in topo order, streaming one status line per container as each completes.
5. After containers, remove orphaned managed networks (full apply only — skipped when a name filter is active).

### Failure handling

- Pull failure on a container → mark that container failed, skip it, continue with others, exit code 2.
- Stop/remove failure → abort that container's action, continue, exit code 2.
- A `Skip` never fails.
- containerctl does not roll back on partial failure. `status` will show the partial state; user re-runs `apply` after fixing the cause.

---

## 7. Runtime interface

```go
package runtime

import (
    "context"
    "io"
    "time"
)

type Runtime interface {
    // Lifecycle
    Pull(ctx context.Context, image string) error
    CreateContainer(ctx context.Context, spec ContainerSpec) (id string, err error)
    StartContainer(ctx context.Context, id string) error
    StopContainer(ctx context.Context, id string, timeout time.Duration) error
    RemoveContainer(ctx context.Context, id string, force bool) error

    // Introspection
    InspectContainer(ctx context.Context, nameOrID string) (*ContainerInfo, error)
    ListContainers(ctx context.Context, filters Filters) ([]ContainerInfo, error)
    Logs(ctx context.Context, id string, opts LogOptions) (io.ReadCloser, error)

    // Networks
    CreateNetwork(ctx context.Context, spec NetworkSpec) (id string, err error)
    RemoveNetwork(ctx context.Context, nameOrID string) error
    ListNetworks(ctx context.Context, filters Filters) ([]NetworkInfo, error)
    NetworkExists(ctx context.Context, name string) (bool, error)

    // Image update detection (used by check-update)
    // LocalImageDigest returns the RepoDigest of the locally cached image, or ""
    // if the image has not been pulled. Used only for floating-tag digest comparison.
    LocalImageDigest(ctx context.Context, image string) (string, error)
    // RemoteImageDigest fetches the current digest from the registry via a direct
    // OCI HTTP HEAD request — no daemon-specific API, works identically on Docker
    // and Podman. Delegates to internal/registry.RemoteDigest.
    RemoteImageDigest(ctx context.Context, image string) (string, error)

    // Meta
    Name() string                  // "docker" or "podman"
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
    CapAdd, CapDrop []string
    Privileged    bool
    ReadOnly      bool
    Tmpfs         []string
}

type PortBinding struct {
    HostIP        string
    HostPort      string
    ContainerPort string
    Protocol      string // "tcp" | "udp"
}

type Mount struct {
    Type     string // "bind" | "volume"
    Source   string
    Target   string
    ReadOnly bool
}

type Resources struct {
    NanoCPUs    int64  // CPUs * 1e9
    MemoryBytes int64
    PidsLimit   int64
}

type Filters struct {
    Labels map[string]string
    Names  []string
}

type ContainerInfo struct {
    ID, Name, Image, State string
    Labels    map[string]string
    StartedAt time.Time
    ExitCode  int
}

type NetworkSpec struct {
    Name   string
    Driver string
    Labels map[string]string
}

type NetworkInfo struct {
    ID, Name, Driver string
    Labels map[string]string
}

type LogOptions struct {
    Follow     bool
    Tail       int    // 0 = all
    Timestamps bool
    Since      time.Time
}

type Healthcheck struct {
    Test                    []string
    Interval, Timeout, StartPeriod time.Duration
    Retries                 int
}
```

### Label constants

```go
const (
    LabelManaged     = "containerctl.managed"
    LabelProject     = "containerctl.project"
    LabelName        = "containerctl.name"
    LabelConfigHash  = "containerctl.config-hash"
    LabelSpecVersion = "containerctl.spec-version"
    SpecVersion      = "1"
)
```

### Implementation notes

- **Docker** (`internal/runtime/docker`): wrap `github.com/docker/docker/client`. Default socket `/var/run/docker.sock`. Map our types to Docker's `container.Config` / `container.HostConfig` / `network.NetworkingConfig`. Only one network can be passed to `ContainerCreate`; additional networks are connected via `NetworkConnect` after creation.
- **Podman** (`internal/runtime/podman`): same Docker SDK, but socket defaults to `/run/podman/podman.sock` (rootful) or `/run/user/$UID/podman/podman.sock` (rootless). Set via `client.WithHost("unix://" + path)`. Reuses the Docker implementation; only `Name()` and the default socket differ. Native bindings can be swapped in later without changing the interface.
- **Hostname auto-default**: when no `hostname:` is declared in YAML, the container's hostname is set to its logical name (e.g. `mosquitto` for a container named `home-services_mosquitto`). Docker's embedded DNS resolves containers by hostname within a user-defined network, so this lets other containers reach `home-services_mosquitto` as simply `mosquitto`. This mirrors Docker Compose behaviour.
- **Registry client** (`internal/registry`): direct OCI HTTP implementation — no daemon API calls. Uses `HEAD /v2/<repo>/manifests/<tag>` with multi-arch Accept headers and generic Bearer token auth (parses `WWW-Authenticate`, fetches from realm endpoint). Works identically for Docker Hub, quay.io, ghcr.io, and any standard OCI registry. Both `LocalImageDigest` and `RemoteImageDigest` route through this, ensuring Podman and Docker produce consistent remote-side results.

---

## 8. Example `stack.yaml`

```yaml
project: home-services
runtime: podman
data_path: ./data

networks:
  - name: backend
    driver: bridge

containers:
  - name: postgres
    image: postgres:16
    restart: unless-stopped
    ports:
      - "127.0.0.1:5432:5432"
    volumes:
      - "postgres/data:/var/lib/postgresql/data"   # → data/postgres/data
    env:
      POSTGRES_DB: app
      POSTGRES_USER: app
    env_file:
      - "postgres/secrets.env"                     # → data/postgres/secrets.env
    networks: [backend]
    resources:
      cpus: "2.0"
      memory: "2g"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U app"]
      interval: 10s
      timeout: 3s
      retries: 5

  - name: redis
    image: redis:7.2-alpine
    restart: unless-stopped
    networks: [backend]
    resources:
      memory: "256m"

  - name: app
    image: registry.example.com/home/app:v1.4.2
    restart: unless-stopped
    ports:
      - "8080:8080"
    env:
      DATABASE_URL: postgres://app@postgres:5432/app
      REDIS_URL: redis://redis:6379/0
    networks: [backend]
    depends_on: [postgres, redis]
    resources:
      cpus: "1.0"
      memory: "512m"

  - name: old-app
    image: app:v1.2.0
    disabled: true
```

---

## 9. Error model

- All user-facing errors are wrapped with context: `"apply container postgres: pull image postgres:16: ..."`.
- Schema validation errors include the YAML path: `containers[2].ports[0]: invalid port spec "80::"`.
- Runtime errors are surfaced verbatim; containerctl does not try to interpret Docker daemon errors.
- Exit codes:
  - `0` success / no-op.
  - `1` configuration or runtime error (nothing was changed).
  - `2` partial failure during apply (some containers reconciled, some didn't).
  - `3` `diff` only: changes are pending.

---

## 10. Future enhancements (not v1)

- **Healthcheck-gated rollout.** Wait for a recreated container to be healthy before proceeding to its dependents.
- **Native Podman bindings.** Swap the Docker-SDK-against-Podman approach for `containers/podman/v5/pkg/bindings` if we need Podman-only features (pods, kube YAML import).
- **Profiles.** `containerctl apply --profile prod` to filter containers by a `profiles:` field.
- **Multi-file includes.** `include: [./db.yaml, ./web.yaml]`.
- **Schema versioning.** Top-level `apiVersion:` field.
- **TUI status view.** `containerctl status --watch` with a refreshing table.
- **`check-update` digest mode for semver tags.** Optionally also compare local digest against remote for pinned semver tags, to detect in-place re-pushes of the same tag (rare for versioned releases but possible).

---

## 11. Build and distribution

```makefile
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD_DATE := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
PKG        := github.com/jkandasa/containerctl/cmd
LDFLAGS    := -ldflags "-s -w -X $(PKG).Version=$(VERSION) -X $(PKG).BuildDate=$(BUILD_DATE)"
GOFLAGS    := -trimpath
```

- `make build` produces a static binary at `./containerctl`.
- Cross-compile for `linux/amd64`, `linux/arm64`. macOS targets work for development against Docker Desktop.
- Release artifacts: tarballs per platform plus `sha256sums.txt`. Single GitHub release per tag.
- `containerctl version` prints: app version, build date, Go version (`debug.ReadBuildInfo`), OS/arch, and whether the configured runtime is reachable.
