# containerctl — Specification

A single static Go binary plus a single YAML file that declaratively manages all containers on one host. Replaces a pile of per-container shell scripts with one source of truth and a reconciliation loop.

---

## 1. Goals and non-goals

### Goals

- **Single binary, single YAML.** No daemon, no separate state store.
- **Declarative.** YAML is the desired state; the host is reconciled toward it.
- **Per-container scope.** Every operation can target one container by name or the whole stack.
- **Two runtimes from day one.** Docker (official SDK) and Podman (via its Docker-compatible API socket), behind a `Runtime` interface.
- **Drift visibility.** `apply --dry-run` and `status` show exactly what will change before `apply` runs.
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
│   ├── status.go
│   ├── upgrade.go              # repull command
│   ├── restart.go          # stop → remove → create → start
│   ├── check_update.go     # update command; registry check; --apply/--follow
│   ├── down.go
│   ├── logs.go
│   ├── pull.go
│   ├── stop.go             # transient stop (next apply restarts)
│   ├── start.go            # transient start
│   ├── disable.go          # persistent off via state file
│   ├── enable.go           # removes from state file, re-reconciles
│   ├── images.go           # list local images; --unused; -o json|yaml
│   ├── volumes.go          # list local volumes; --unused/--size; -o json|yaml
│   ├── networks.go         # list user-defined networks; --unused; -o json|yaml
│   ├── prune.go            # remove unused images/volumes/networks
│   ├── helpers.go          # formatAge, formatImageSize, stdinIsTerminal
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
│   ├── reconcile/          # build plan + apply
│   │   ├── plan.go         # Plan, Action (Create|Recreate|Skip|Remove|Disabled|DeclaredOff)
│   │   └── reconcile.go    # Apply(ctx, plan, runtime, w) — streams per-container status
│   ├── state/              # file-based persistent disabled state
│   │   └── state.go        # Load/Save/IsDisabled/Disable/Enable/DisabledSet
│   └── render/             # human + json output for status/apply/plan
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

serve:                 # optional. Controls behaviour of "containerctl serve".
  exec:
    enabled: bool      # default false. Must be true for any exec command.
    allowed: [string]  # optional allowlist of logical container names.
                       # Empty = all containers permitted when enabled is true.
                       # Non-empty = only listed containers may be exec'd into.
  edit:
    enabled: bool      # default false. Must be true for the browser stack file editor.
  use:
    enabled: bool      # default false. Must be true for the "use" stack-switch command.
```

### Container

```yaml
- name: string         # required. Logical name. Final container name = "<project>_<name>".
  image: string        # required. e.g. "postgres:16" or "registry.example.com/app:v1.2.3".
  disabled: bool       # optional. Default: false. When true, apply removes the container (if
                       # present) and skips creation. See §6 "Disabling containers".
  update_policy: string # optional. auto|manual. Default: auto. When "manual", update skips
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

Applied to every string value at load time via `os.Expand` with a custom mapping:

| Syntax | Behaviour |
|--------|-----------|
| `$VAR` / `${VAR}` | Value of `VAR`; empty string if unset |
| `${VAR:-default}` | Value of `VAR` if set and non-empty, otherwise `default` |
| `$$` | Literal `$` — skips expansion entirely |

```yaml
env:
  MODE: "${APP_MODE:-production}"       # "production" if APP_MODE unset

command:
  - "--log-level=${LOG_LEVEL:-info}"    # containerctl resolves default at load time
  - "--raw=$${LOG_LEVEL:-info}"         # container receives ${LOG_LEVEL:-info} literally
```

The `$$` escape is useful when the container's entrypoint is a shell and should evaluate the default itself rather than having containerctl resolve it.

---

## 5. CLI surface

All commands accept `-f, --file PATH` (default: `./stack.yaml`) and `--runtime docker|podman` (overrides YAML).

| Command                                                          | Purpose                                                                                                                                   | Exit codes                               |
| ---------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------- |
| `containerctl apply [name...] [--dry-run]`                       | Reconcile host to YAML. With names, only those containers are affected. Orphaned containers/networks and unrelated network creation are skipped; run without names for a full cleanup. Streams per-container status as each action completes. `--dry-run` shows the plan without making any changes. | 0 ok, 1 error, 2 partial failure (apply); 0 no changes, 3 changes pending, 1 error (--dry-run) |
| `containerctl status [name...]`                                  | Show all managed containers, their state, ports, created age, uptime, restarts, and sync status. `--stats` adds live CPU/memory usage and a THROTTLE column (appears only when any container has been throttled). `-o json\|yaml` adds network IPs, mount paths, image digest/size, resource limits, timestamps in local timezone, and a `stats` object (present only with `--stats`) containing `cpu_percent`, `cpu_throttled_periods`, `cpu_throttled_time`, `cpu_throttled_time_ns`, `memory_used`, `memory_used_bytes`, and `memory_fail_count`. | 0 ok, 1 error                            |
| `containerctl update [name...] [--apply] [--follow]`             | Query the registry for updates. Semver tags: shows patch/minor and major updates separately. Floating tags: compares local vs remote digest. `--apply` pulls and recreates containers with patch/minor updates or digest changes. `--follow` streams logs after applying (requires `--apply` and exactly one container name). Skips containers with `disabled: true` or `update_policy: manual`. | 0 ok, 1 error |
| `containerctl repull <name>`                                     | Force-pull the image and recreate a container, bypassing the config hash. Use for floating tags (e.g. `:latest`).                         | 0 ok, 1 error                            |
| `containerctl restart <name...> \| --all [--follow]`             | Recreate containers from current config (stop, remove, create, start) without pulling. `--follow` streams logs after restart (single container only). | 0 ok, 1 error                            |
| `containerctl pull [name...]`                                    | Pull images without reconciling. Skips containers with `disabled: true` in YAML.                                                          | 0 ok, 1 error                            |
| `containerctl down [name...]`                                    | Stop and remove managed containers. With no args, the whole project.                                                                      | 0 ok, 1 error                            |
| `containerctl stop <name...> \| --all`                           | Stop containers; they stay on disk and restart on next apply. Requires at least one name or `--all`.                                      | 0 ok, 1 error                            |
| `containerctl start <name...> \| --all [--follow]`               | Start stopped containers without reconciling. Refuses if persistently disabled — run `enable` first. `--follow` streams logs after start (single container only). | 0 ok, 1 error                            |
| `containerctl disable <name...>`                                 | **Persistent** off. Stops the container and records it in the project state file. Survives reboots and `apply`. Container is not removed. | 0 ok, 1 error                            |
| `containerctl enable <name...>`                                  | Removes from state file and reconciles the container (recreates if hash drifted, else starts).                                            | 0 ok, 1 error                            |
| `containerctl logs <name> [--follow] [--tail N]`                 | Stream container logs. Note: `--follow` has no `-f` shorthand (conflicts with global `-f/--file`).                                       | 0 ok, 1 error                            |
| `containerctl images [name...] [--unused]`                       | List local images. `--unused` shows only images not referenced by any running container or stack declaration. `-o json\|yaml` includes per-image container list. | 0 ok, 1 error |
| `containerctl volumes [--unused] [--size]`                       | List local volumes with attached containers. `--unused` shows only dangling volumes. `--size` fetches disk usage via the daemon's disk-usage endpoint (daemon-side scan). `-o json\|yaml` includes host mountpoint, size (when `--size` is given), and per-volume mount details. | 0 ok, 1 error |
| `containerctl networks [--unused]`                               | List user-defined networks (bridge, host, none excluded). `--unused` shows only networks not connected to any container. `-o json\|yaml` includes per-network container list with IP address and gateway. | 0 ok, 1 error |
| `containerctl prune [--images] [--volumes] [--networks] [--all] [--dry-run] [--force]` | Remove unused local resources. At least one resource type flag (or `--all`) required. `--dry-run` previews without removing. `--force` skips the interactive confirmation (required when stdin is not a terminal). | 0 ok, 1 error |
| `containerctl generate [name...] [-O FILE]`                      | Render a `stack.yaml` from containers that already exist on the host (import/migration aid). With no names, every container on the host is captured. Writes to stdout, or to `FILE` (created with mode `0600`) with `-O`. Never touches the host. | 0 ok, 1 error |
| `containerctl version`                                           | Print binary version, build date, Go version, OS/arch, and runtime reachability.                                                         | 0                                        |

### `generate` — importing existing containers

`generate` inspects containers (and the images behind them) and emits the equivalent declarative config. It is the inverse of `apply`, so the output feeds straight back in: for a container that containerctl already manages, `generate` followed by `apply --dry-run` reports **no changes**.

Project and naming:

- The project name comes from `--project`, else the `project:` of the stack file found via `-f`, else `myproject`.
- Container names come from the `containerctl.name` label when present, otherwise the host container name with the `<project>_` prefix trimmed.
- Networks are declared at the top level with the `<project>_` prefix trimmed, because `apply` adds it back. A network that does not carry the project prefix (e.g. a `docker compose` network) cannot be referenced as-is — `apply` would create `<project>_<name>` and attach the container there instead — so `generate` warns about it on stderr.
- Duplicate container names (possible when importing several projects at once) are suffixed `-2`, `-3`, … since `apply` rejects duplicates.

Omitted on purpose:

| Not emitted | Why |
| ----------- | --- |
| Anything equal to the image default (`command`, `entrypoint`, `env`, `user`, `working_dir`, `labels`, `healthcheck`) | Keeps the file to what the operator actually chose. |
| `hostname` when it equals the container name or the runtime's short-ID default | `apply` derives it from the name. |
| `containerctl.*` and `com.docker.compose.*` labels | Tool bookkeeping; `apply` writes its own, and keeping compose labels would leave compose believing it still owns the container. |
| Anonymous volumes | Runtime-generated IDs are not reproducible; emitted as commented-out entries plus a stderr warning so the operator can supply a real path. |
| Exposed-only ports (no host binding) | Usually the image's own `EXPOSE` list. |
| Network aliases the runtime adds itself (container name, hostname, short ID) | Not operator configuration. |
| `env_file`, `depends_on`, `update_policy`, `disabled`, `data_path` | Not recoverable from a running container. |
| `host` / `none` network modes, tmpfs mount options | Not expressible in the schema; reported as stderr warnings. |

Output is deterministic — containers, networks, ports, volumes and tmpfs paths are sorted, so regenerating an unchanged host produces a byte-identical file. `env:` values are copied verbatim and routinely contain credentials, which is why `-O` creates the file with mode `0600`; review before committing.

### `restart` vs `repull` vs `apply` vs `update --apply`

| Command          | Pulls image        | Recreates always | Hash-driven | Updates stack.yaml |
| ---------------- | ------------------ | ---------------- | ----------- | ------------------ |
| `apply`          | yes (new)          | no               | yes         | no                 |
| `repull`         | yes (force)        | yes              | no          | no                 |
| `restart`        | no                 | yes              | no          | no                 |
| `update --apply` | yes (new tag/same) | yes              | no          | yes (patch only)   |

Use `restart` when you want a clean container from the cached image without any network pull (e.g. after editing a mounted config file). Use `repull` when the image tag floats and you need the latest pull. Use `update --apply` for automated patch/minor version upgrades — it rewrites the image tag in stack.yaml before recreating so the change is persistent.

### `update` update detection

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
- `-o, --output console|json|yaml` — output format. Default console. JSON and YAML are indented with 2 spaces.
- `--no-color` — disable ANSI colors (also respects the `NO_COLOR` environment variable).
- `--project NAME` — override YAML's `project:` (use with care; affects which containers are considered managed).

### Output: `apply` / `apply --dry-run` plan format

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

### Output: `status` console format

```
NAME       STATE         IMAGE            CREATED   UPTIME    SYNC
postgres   running       postgres:16      30d 2h    4d 2h     ok
nginx      running       nginx:1.27       30d 2h    4d 2h     drift
redis      stopped       redis:7.2        10d 5h    -         ok
backups    disabled      restic:0.16      10d 5h    -         -
old-app    declared-off  app:v1.2.0       5d 1h     2h 10m    -
orphan     declared-off  -                -         -         -
```

With `--stats` (THROTTLE column only appears when at least one container has non-zero throttle data):

```
NAME       STATE    IMAGE        CREATED  UPTIME  RESTARTS  CPU      MEM       THROTTLE   SYNC
postgres   running  postgres:16  30d 2h   4d 2h   0         0.42%    38.2 MiB  cpu:42 mem:0  ok
nginx      running  nginx:1.27   30d 2h   4d 2h   0         0.11%    12.1 MiB  -          drift
```

State values: `running`, `stopped` (exited — apply will restart), `disabled` (in state file — apply skips), `declared-off` (YAML `disabled: true` — if the container is still on the host, runtime data is shown and NOTE says "apply will remove"; if not on host, all fields are `-`), `missing` (in YAML, not on host — apply will create).

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
   - Container found → `Remove` (plan action). Status shows `declared-off` with live runtime data and note "apply will remove".
   - Container not found → `Skip` (state: `declared-off`, all runtime fields blank).
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
    Exec(ctx context.Context, id string, opts ExecOptions) (int, error)
    ContainerStats(ctx context.Context, id string) (ContainerUsage, error)

    // Networks
    CreateNetwork(ctx context.Context, spec NetworkSpec) (id string, err error)
    RemoveNetwork(ctx context.Context, nameOrID string) error
    ListNetworks(ctx context.Context, filters Filters) ([]NetworkInfo, error)
    NetworkExists(ctx context.Context, name string) (bool, error)

    // Images
    ListImages(ctx context.Context) ([]ImageInfo, error)
    RemoveImage(ctx context.Context, id string, force bool) error

    // Volumes
    ListVolumes(ctx context.Context, f Filters) ([]VolumeInfo, error)
    RemoveVolume(ctx context.Context, name string, force bool) error
    // VolumeSizes queries the daemon's disk-usage endpoint (triggers a daemon-side
    // disk scan). Returns a map of volume name → bytes; -1 when driver doesn't report.
    VolumeSizes(ctx context.Context) (map[string]int64, error)

    // Image update detection (used by update)
    LocalImageMeta(ctx context.Context, image string) (ImageMeta, error)
    RemoteImageDigest(ctx context.Context, image string) (string, error)
    CheckTagUpdates(ctx context.Context, image string, max int) (*registry.TagUpdates, error)

    // Engine info
    EngineVersion(ctx context.Context) (EngineInfo, error)

    // Meta
    Name() string                  // "docker" or "podman"
    Ping(ctx context.Context) error
    Close() error
}

type ContainerSpec struct {
    Name           string
    Image          string
    Command        []string
    Entrypoint     []string
    Env            map[string]string
    Labels         map[string]string
    Ports          []PortBinding
    Mounts         []Mount
    Networks       []string
    NetworkAliases []string
    Resources      Resources
    Healthcheck    *Healthcheck
    RestartPolicy  string
    User           string
    WorkingDir     string
    Hostname       string
    DNS            []string
    GroupAdd       []string
    CapAdd         []string
    CapDrop        []string
    Privileged     bool
    SecurityOpt    []string
    ReadOnly       bool
    Tmpfs          []string
}

type PortBinding struct {
    HostIP        string
    HostPort      string
    ContainerPort string
    Protocol      string // "tcp" | "udp"
}

type Mount struct {
    Type     string // "bind" | "volume" | "tmpfs"
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
    Labels   map[string]string
    Names    []string
    Dangling *bool // nil = no filter; true = unused/dangling only
}

// ContainerInfo is returned by ListContainers and InspectContainer.
// Mounts and NetworkInfos are populated by ListContainers and carry
// full path and network details for structured output.
// CreatedAt, StartedAt, and LastRestart are in the host's local timezone.
type ContainerInfo struct {
    ID           string
    Name         string
    Image        string
    ImageID      string                // full sha256 image ID (sha256:…)
    Mounts       []ContainerMount
    NetworkInfos []ContainerNetworkInfo
    State        string
    Labels       map[string]string
    CreatedAt    time.Time
    StartedAt    time.Time
    ExitCode     int
    Ports        []PortBinding
    RestartCount int
    LastRestart  time.Time
    Resources    ContainerResources
}

type ContainerMount struct {
    Type        string // "bind" | "volume" | "tmpfs"
    Name        string // volume name (empty for bind mounts)
    Source      string // host path or volume backing path
    Destination string // path inside the container
    ReadOnly    bool
}

type ContainerNetworkInfo struct {
    Name      string
    IPAddress string
    Gateway   string
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
    Name       string            `json:"name"`
    Driver     string            `json:"driver"`
    Mountpoint string            `json:"mountpoint,omitempty"` // host path where volume data lives
    Size       *int64            `json:"size,omitempty"`       // nil = not fetched; -1 = driver doesn't report
    Labels     map[string]string `json:"labels,omitempty"`
}

type LogOptions struct {
    Follow     bool
    Tail       int    // 0 = all
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
  - `3` `apply --dry-run` only: changes are pending.

---

## 10. Future enhancements (not v1)

- **Healthcheck-gated rollout.** Wait for a recreated container to be healthy before proceeding to its dependents.
- **Native Podman bindings.** Swap the Docker-SDK-against-Podman approach for `containers/podman/v5/pkg/bindings` if we need Podman-only features (pods, kube YAML import).
- **Profiles.** `containerctl apply --profile prod` to filter containers by a `profiles:` field.
- **Multi-file includes.** `include: [./db.yaml, ./web.yaml]`.
- **Schema versioning.** Top-level `apiVersion:` field.
- **TUI status view.** `containerctl status --watch` with a refreshing table.
- **`update` digest mode for semver tags.** Optionally also compare local digest against remote for pinned semver tags, to detect in-place re-pushes of the same tag (rare for versioned releases but possible).

---

## 12. Web terminal (`containerctl serve`)

An optional HTTP/HTTPS server that exposes a browser-based terminal for remote monitoring and management. After authenticating with a shared token, the user lands in a terminal where they can run the same containerctl subcommands as the CLI.

### Design constraints

- **No daemon.** `serve` is a foreground process. No new persistent state beyond TLS certs.
- **Single token, stateless auth.** No user database. Token is set by the operator and compared in constant time.
- **Restricted shell.** The browser terminal runs containerctl commands only — not arbitrary shell commands. The allowlist is fixed in code.
- **Read flag from global context.** `serve` inherits `--file`, `--runtime`, `--socket`, and `--project` globals so it operates on the same stack as the rest of the CLI.

---

### 12.1 Command: `containerctl serve`

```
containerctl serve [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--address ADDR` | `:8080` | TCP address to listen on. |
| `--token TOKEN` | — | **Required.** Shared auth token. Also read from `CONTAINERCTL_TOKEN` env var. If neither flag nor env is set, `serve` fails with an error. |
| `--tls MODE` | `none` | TLS mode: `none`, `self-signed`, `letsencrypt`, or `custom`. |
| `--tls-domain DOMAIN` | — | Public domain for Let's Encrypt. Required when `--tls=letsencrypt`. |
| `--tls-cert PATH` | — | Certificate file path. Required when `--tls=custom`. |
| `--tls-key PATH` | — | Key file path. Required when `--tls=custom`. |
| `--tls-cache-dir DIR` | `$XDG_DATA_HOME/containerctl/certs` | Let's Encrypt cert cache directory. |
| `--session-ttl DURATION` | `24h` | How long a login session stays valid. |

Startup sequence:

1. Validate token is present (fail fast if missing).
2. Configure TLS (generate/load cert based on mode).
3. Print the listen address and TLS mode to stderr.
4. Block serving until interrupted (SIGINT/SIGTERM triggers graceful shutdown with 10 s timeout).

---

### 12.2 TLS modes

| Mode | How it works |
|------|-------------|
| `none` | Plain HTTP via `http.ListenAndServe`. Safe when behind a TLS-terminating reverse proxy. |
| `self-signed` | Generate an ECDSA P-256 cert + key in memory at startup. Cert is valid for 10 years and SANs `localhost` plus the machine's non-loopback IPs. Logged to stderr so users can pin it. Browser will show a security warning — expected. |
| `letsencrypt` | `golang.org/x/crypto/acme/autocert` with `DirCache` at `--tls-cache-dir`. Requires `--tls-domain`. `autocert.Manager` also starts the HTTP-01 challenge responder on `:80`. Fails to start if `:80` is not bindable (print actionable error). |
| `custom` | `http.ListenAndServeTLS` with operator-supplied cert/key paths. Reloads cert on SIGHUP. |

Self-signed cert generation (in `internal/web/tls.go`):

```go
func generateSelfSigned() (tls.Certificate, error) {
    key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
    tmpl := &x509.Certificate{
        SerialNumber: big.NewInt(1),
        NotBefore:    time.Now(),
        NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
        KeyUsage:     x509.KeyUsageDigitalSignature,
        ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
        IPAddresses:  localIPs(), // all non-loopback IPs + 127.0.0.1
        DNSNames:     []string{"localhost"},
    }
    der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
    return tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
        marshalECKey(key))
}
```

---

### 12.3 HTTP routes

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/` | — | Redirect to `/terminal` if session cookie valid, else redirect to `/login`. |
| `GET` | `/login` | — | Login page (static HTML). |
| `POST` | `/login` | — | Validate token (form field `token`). On success: create session, set cookie, redirect to `/terminal`. On failure: re-render login with error. |
| `POST` | `/logout` | session | Delete session, redirect to `/login`. |
| `GET` | `/terminal` | session | Web terminal page (static HTML). |
| `GET` | `/ws/terminal` | session | WebSocket: interactive containerctl terminal. |
| `GET` | `/ws/exec` | session | WebSocket: PTY-based interactive exec session inside a container. Requires `serve.exec.enabled: true`. |
| `GET` | `/ws/logs` | session | WebSocket: streaming container logs (query params: `?name=<name>&follow=true&tail=N&file=<path>`). |
| `GET` | `/api/v1/status` | session | JSON array of container status entries (same data as `containerctl status --output json`). |
| `GET` | `/api/v1/file` | session | Read a file: returns content + ETag header (sha256 of content). |
| `PUT` | `/api/v1/file` | session | Write a file: requires `If-Match` header matching the current ETag; returns 409 Conflict if the file changed since the read. |
| `GET` | `/static/*` | — | Embedded static assets (CSS, JS, fonts). |

All session-protected routes return `302 /login` (not 401) for browser navigation. `/api/v1/*` and `/ws/*` return `401 {"error":"unauthenticated"}` when session is missing.

---

### 12.4 Session management

- Sessions live in an in-memory `sync.Map` — not persisted across restarts.
- Session ID: 32 bytes from `crypto/rand`, hex-encoded (64 chars).
- Cookie name: `containerctl_session`. Attributes: `HttpOnly; SameSite=Strict; Path=/`. `Secure` flag added when TLS mode is not `none`.
- TTL enforced at read time: expired sessions are treated as absent and lazily reaped.
- Token comparison: `subtle.ConstantTimeCompare` to prevent timing attacks.
- **Long-lived connection re-validation**: WebSocket handlers (`/ws/terminal`, `/ws/exec`, `/ws/logs`) re-validate the session on upgrade and on every inbound message. An invalid/expired session sends a `session_invalid` message; the client redirects to `/login?error=expired`.
- **Brute-force protection**: per-IP failure tracking. After 5 consecutive failed login attempts the IP is blocked for 30 seconds. The login page shows a countdown timer and disables the form during the block. Block resets after the 30 s window expires. The client IP is read from the `X-Forwarded-For` header when present.

```go
type session struct {
    createdAt time.Time
}

type sessionStore struct {
    ttl      time.Duration
    sessions sync.Map // map[string]*session
    failMu   sync.Mutex
    failures map[string]*loginAttempt
}

func (s *sessionStore) validateLogin(ip, token string) (ok, blocked bool, retryAfter time.Duration)
func (s *sessionStore) create() string { /* crypto/rand ID */ }
func (s *sessionStore) valid(id string) bool { /* lookup + TTL check */ }
func (s *sessionStore) delete(id string) { s.sessions.Delete(id) }
```

---

### 12.5 WebSocket terminal protocol (`/ws/terminal`)

The WebSocket connection represents one interactive session. It carries per-connection state: the active stack file (defaults to the server's `--file`, changeable with `use`).

**Client → Server message (JSON):**

```json
{"cmd": "status"}
{"cmd": "logs nginx --follow"}
{"cmd": "apply"}
{"cmd": "__interrupt__"}
```

`__interrupt__` cancels the currently running command (maps to Ctrl+C).

**Server → Client messages (JSON):**

| Type | Fields | Description |
|------|--------|-------------|
| `output` | `data` | Text to write to the terminal. May be partial lines or multi-line chunks. |
| `done` | `code` | Command completed. `code` is the exit code (0 = success). Always follows the last `output`. |
| `error` | `msg` | Error message shown in red. Follows immediately for validation errors; replaces `done` when a bad command is typed. |
| `clear` | — | Clear the terminal (client calls `term.clear()`). |
| `prompt` | `data` | New prompt string to display, e.g. `containerctl [stack.yaml]> `. Sent on connect and whenever `use` changes the active file. |
| `names` | `names` | Updated list of container logical names for Tab completion. Sent on connect and after `use`. |
| `edit` | `data` | Absolute path of the file the browser editor should open. |
| `exec_open` | `data` | URL-encoded query string for the `/ws/exec` connection the client should open. |

- While a command is running, a new command is rejected with `{"type":"error","msg":"command already running"}`.
- `--follow` on `logs` streams until the client closes the WebSocket or sends any message (acts as interrupt).

---

### 12.6 Command dispatch (allowlist)

The terminal does **not** execute arbitrary shell commands. Input is parsed and the first word matched against a fixed allowlist. Recognised commands are dispatched as containerctl subprocesses — the server runs the same `containerctl` binary pointed to by `os.Executable()`, injecting global flags (`--file`, `--runtime`, `--socket`, `--project`) before the user's arguments.

**Per-command `--file` override:** if the user includes `--file` or `-f` in their command, the server does not inject its own `--file`. This lets users target a different stack for a single command without changing the active session file.

**`use` changes the session's active stack:** `use /path/to/other.yaml` updates the per-connection active file. All subsequent commands without an explicit `--file` use the new path. The prompt and Tab completion list update immediately.

Allowed commands:

| User input | Dispatch |
|-----------|----------|
| `apply [name...] [--dry-run]` | subprocess |
| `update [name...] [--apply] [--follow]` | subprocess |
| `clear` | built-in: sends `{"type":"clear"}` |
| `disable <name...>` | subprocess |
| `down [name...]` | subprocess |
| `edit` | built-in: sends `{"type":"edit","data":"<path>"}` |
| `enable <name...>` | subprocess |
| `exec <name> [cmd...]` | see below |
| `generate [name...] [-O FILE]` | subprocess; `-O` requires `serve.edit.enabled` and an absolute `.yaml`/`.yml` path |
| `help [command]` | built-in or subprocess (`<cmd> --help`) |
| `images [name...] [--unused]` | subprocess |
| `logs <name> [--follow] [--tail N]` | subprocess |
| `networks [--unused]` | subprocess |
| `prune [--images] [--volumes] [--networks] [--all] [--dry-run] [--force]` | subprocess |
| `pull [name...]` | subprocess |
| `repull <name>` | subprocess |
| `restart <name...> \| --all [--follow]` | subprocess |
| `start <name...> \| --all [--follow]` | subprocess |
| `status [name...]` | subprocess |
| `stop <name...> \| --all` | subprocess |
| `use <path>` | built-in: updates session's active file |
| `version` | subprocess |
| `volumes [--unused] [--size]` | subprocess |

Any other input: `{"type":"error","msg":"unknown command \"<input>\"; type help for available commands"}`.

**`exec` dispatch rules:** `exec` is gated by `serve.exec.enabled`. When disabled, all exec invocations are rejected with an error message. When enabled, the container name is additionally checked against `serve.exec.allowed` (if non-empty). Interactive shell invocations (bare `bash`/`sh`/`zsh` etc. without `-c`) are routed to a dedicated PTY-based WebSocket (`/ws/exec`) via `{"type":"exec_open","data":"<query-params>"}`. Non-interactive invocations (e.g. `exec myapp ps aux`) run as a regular subprocess.

Tab completion is seeded by the server at connect time: the server runs `containerctl status --output json` and sends `{"type":"names","names":[...]}`. The list refreshes after every `use` command.

---

### 12.7 `/ws/exec` WebSocket

Dedicated endpoint for PTY-based interactive exec sessions (shells, vim, etc.) inside a container.

**Query params (all required unless noted):**

| Param | Description |
|-------|-------------|
| `name` | Logical container name. Rejected if not in the exec allowlist. |
| `file` | Absolute path to the active stack file (passed by the terminal session). |
| `cmd` | Space-separated command (optional; defaults to `/bin/sh`). |
| `rows` | Initial terminal rows (used with `pty.StartWithSize` so vi/vim opens full-screen). |
| `cols` | Initial terminal columns. |

**Client → Server (JSON):**

```json
{"type": "input",  "data": ""}
{"type": "resize", "rows": 40, "cols": 160}
```

**Server → Client (JSON):**

```json
{"type": "output", "data": "...raw PTY bytes..."}
{"type": "done",   "code": 0}
```

The PTY is started with `pty.StartWithSize` so the subprocess sees the correct terminal dimensions from its first `TIOCGWINSZ` call. Window resize messages are forwarded via `pty.Setsize`. When the subprocess exits, the server sends `{"type":"done","code":N}` and closes the WebSocket.

---

### 12.8 `/ws/logs` WebSocket

Dedicated endpoint for following a single container's log stream (mirrors `containerctl logs --follow`).

- Query params: `?name=<logical-name>` (required), `?follow=true`, `?tail=N`, `?file=<path>` (optional; falls back to server default).
- Server messages: `{"type":"output","data":"..."}` (raw log line with optional timestamp).
- Server sends `{"type":"done","code":N}` when the stream ends.
- Client closes WebSocket to stop streaming; any inbound message also interrupts the stream.

---

### 12.9 `/api/v1/file` — File read/write

Supports reading and writing the active stack file from the browser editor with optimistic concurrency control.

**GET `/api/v1/file?path=<absolute-path>`**

Returns the file content as plain text. The response includes an `ETag` header set to the sha256 hex digest of the content. Clients store this ETag and present it on the subsequent write.

**PUT `/api/v1/file?path=<absolute-path>`** with body = new file content and header `If-Match: <etag>`

Reads the current file, recomputes its sha256, and compares with `If-Match`. If they match: writes the new content and returns 200. If they differ (another client wrote in the meantime): returns 409 Conflict with `{"error":"conflict"}`. The browser editor shows a red warning and preserves the user's unsaved edits so no work is lost.

**Gating and accepted paths.** The endpoint is the browser editor's transport, so it is gated on `serve.edit.enabled` exactly like the `edit` command — both methods return `403` when editing is disabled (the default). When enabled, the path must be absolute, must resolve to an existing file, and must end in `.yaml` or `.yml` (`400` otherwise); writes never create new files.

The YAML restriction is deliberate but coarse: because `use` may point the session at any stack file, the target cannot be pinned to a single directory, so restricting the extension is what keeps shell profiles, `authorized_keys` and unit files out of reach of a hijacked session. Beyond that, operators still rely on filesystem permissions and the session token — a session with editing enabled can rewrite any stack file the server process can write, which is by design.

---

### 12.10 `/api/v1/status` JSON response

Returns the same data as `containerctl status --output json`. Shape:

```json
[
  {
    "name":    "postgres",
    "state":   "running",
    "image":   "postgres:16",
    "uptime":  "4d 2h",
    "drift":   false,
    "drift_fields": []
  },
  {
    "name":    "nginx",
    "state":   "running",
    "image":   "nginx:1.27",
    "uptime":  "4d 2h",
    "drift":   true,
    "drift_fields": ["image", "env"]
  }
]
```

This is a polling endpoint — there is no push/SSE for the status panel in v1.

---

### 12.11 Frontend

Embedded pages served via `//go:embed` from `internal/web/assets/`.

**Login page (`login.html`):**

- Centered card: "containerctl" heading, single `<input type="password" name="token">` field, submit button.
- Error rendering:
  - `?error=1` → "Invalid token. Please try again."
  - `?error=expired` → "Your session has expired. Please sign in again."
  - `?error=blocked&sec=N` → countdown timer + form disabled for the rate-limit window.
- JavaScript countdown re-enables the form when a block expires.
- Respects the user's chosen color theme (see Terminal page below).
- Minimal CSS using CSS custom properties for theming.

**Terminal page (`terminal.html`):**

- Full-viewport terminal rendered by xterm.js v5.3.0 + FitAddon (embedded assets, not CDN).
- **Color themes:** Supports three modes — `dark` (default), `light`, and `auto` (follows `prefers-color-scheme`). A theme toggle button (◐ / ☾ / ☼) lives in the top bar. Selection is persisted in `localStorage` and applied on load. The theme affects:
  - xterm.js terminal colors
  - CodeMirror editor (syntax highlighting, UI elements)
  - Login page
  - All status bars, badges, and chrome
- **Top bar:** "containerctl" title on the left, current stack filename pill (basename, updates on `use`), exec badge (amber, shown only during exec sessions), theme toggle button, and Logout button on the right.
- **Dynamic prompt:** set by the `prompt` server message. Default `containerctl [stack.yaml]> ` (basename of active file). Updates after `use`.
- **Tab completion:** two-level. First Tab shows completions; second Tab cycles. Completes: command names (from `ALL_CMDS`) and container names (from the `names` server message). Also completes command flags when the partial input matches a command.
- **Command history:** up/down arrow recalls previous commands (client-side JS array, not sent to server).
- **Exec mode:** when the server sends `exec_open`, the client opens `/ws/exec` as a second WebSocket. While open: `term.onData` forwards all keystrokes and paste events to the PTY; `term.onKey` is suppressed; the top bar shows an amber badge with container name and command; the border turns amber. An "Exit" button sends Ctrl+D. On exec WS close, the client sends `{"type":"resize",…}` to restore the terminal to its current dimensions and re-renders the main prompt.
- **Browser editor (`edit` command):** full-screen overlay using CodeMirror 5 with vim keymap and YAML syntax highlighting. Crosshair (horizontal active-line + vertical column highlight via CSS `--cur-x` variable). Status bar shows vim mode (NORMAL/INSERT/VISUAL) and cursor position. Keys: `:w` / `Ctrl+S` save; `:wq` / `:x` save+quit; `:q` quit (blocked with unsaved changes); `:q!` / `Ctrl+Q` force-quit discarding changes. Ex-commands require the CodeMirror dialog addon (`codemirror-dialog.js`) which is loaded before the vim keymap. ETag-based concurrent edit protection: 409 from the server shows a red "conflict" warning; unsaved edits are preserved.
- **`clear` message:** calls `term.clear()`.
- **Disconnect handling:** On unexpected WebSocket close, shows a "Reconnect" link in addition to the disconnect message. Session expiry on long-lived connections redirects the page to `/login?error=expired`.

**Static assets build step:**

```makefile
build: $(ASSETS)
    go build ...

$(ASSETS):  # each asset is a Makefile file target downloaded only if missing
    curl -fsSL <cdn-url> -o <path>
```

Downloaded assets (xterm.js, xterm.css, xterm-addon-fit.js, codemirror.js, codemirror.css, codemirror-dialog.js, codemirror-dialog.css, codemirror-vim.js, codemirror-yaml.js) are listed in `.gitignore`. `make build` downloads any missing assets automatically, so the build is self-contained. `make assets` force-refreshes all assets from the CDN.

---

### 12.12 Package layout additions

```
cmd/
  serve.go              # cobra command; flag parsing, server startup
internal/
  web/
    server.go           # http.Server setup, route registration, graceful shutdown, Config struct
    auth.go             # token validation (constant-time), sessionStore, per-IP rate limiting
    handlers.go         # login/logout/terminal page/status API/file read-write handlers
    terminal.go         # /ws/terminal and /ws/exec WebSocket handlers, command dispatch
    tls.go              # generateSelfSigned(), autocert setup
    assets/             # embedded static files (go:embed target)
      login.html
      terminal.html
      style.css
      xterm.js          # downloaded by make build/assets if missing
      xterm.css         # downloaded
      xterm-addon-fit.js
      codemirror.js          # downloaded
      codemirror.css
      codemirror-dialog.js   # downloaded; required by vim keymap for ex-commands (:w, :q, etc.)
      codemirror-dialog.css
      codemirror-vim.js
      codemirror-yaml.js
```

`internal/web` dispatches via subprocess (`os.Executable()` + `exec.CommandContext`) rather than importing internal packages directly. This keeps the web layer thin and ensures CLI and web terminal behaviour are always identical.

---

### 12.13 Subprocess dispatch model

The web terminal runs commands by invoking the same `containerctl` binary that is currently running (resolved via `os.Executable()` with symlink dereferencing). This avoids any writer-injection refactor and guarantees that CLI and web-terminal behaviour are always identical.

For each command:

1. Parse the user's input into `(name, args)`.
2. Build global flags: `--file <activeFile>` (omitted if user already supplied `-f/--file`), `--runtime`, `--socket`, `--project`.
3. Run `exec.CommandContext(ctx, executable, globalFlags..., args...)` with a combined stdout+stderr pipe.
4. Stream pipe output as `{"type":"output","data":"..."}` messages.
5. On exit: send `{"type":"done","code":N}`.

Cancellation via `__interrupt__` cancels the `context.Context`, which kills the subprocess via `SIGKILL` (Go default). Exit code 130 is used for interrupted commands.

---

### 12.14 New dependencies

| Package | Reason |
|---------|--------|
| `github.com/gorilla/websocket` | WebSocket server. More complete than `golang.org/x/net/websocket` (ping/pong, close frames, concurrent writes). |
| `golang.org/x/crypto` | `acme/autocert` for Let's Encrypt. Already a transitive dep; add to `require` directly. |
| `github.com/creack/pty` | Allocate a PTY and start a subprocess inside it (`pty.StartWithSize`). Required for exec sessions that need a real TTY (interactive shells, vim, etc.). |

Frontend JS (xterm.js, CodeMirror) is downloaded at build time and embedded — no Go module dependency.

---

### 12.15 Security considerations

- Token must be treated as a secret. Recommend: long random string (32+ chars), set via env var (`CONTAINERCTL_TOKEN`) rather than CLI flag to avoid shell history exposure.
- Token comparison is constant-time via `crypto/subtle`.
- Session cookie is `HttpOnly` (not readable by JS) and `SameSite=Strict` (CSRF protection).
- **Login brute-force protection:** after 5 consecutive failures from the same IP, that IP is blocked for 30 s. Counts reset after a successful login.
- The terminal allowlist eliminates arbitrary command injection — user input is parsed and only recognised commands are dispatched.
- **`exec` is opt-in and double-gated:** `serve.exec.enabled: true` must be set in stack.yaml AND the container name must be in the allowlist (if `allowed` is non-empty). Exec gives full shell access to the container, so it must be deliberately enabled.
- **`edit` and `use` are opt-in** (see `serve.edit.enabled` / `serve.use.enabled`): the editor writes to files on disk; `use` allows browsing other stacks. Both default to disabled for minimal-privilege deployments.
- **Every server-side write path honours `serve.edit.enabled`.** That covers the `edit` command, the `/api/v1/file` endpoint behind it (§12.9), and `generate -O FILE`, which makes the subprocess write to the server's disk. All three additionally require an absolute `.yaml`/`.yml` target, so an authenticated session cannot create or overwrite arbitrary files. `generate` without `-O` writes only to the terminal and needs no gate.
- WebSocket origin check: the server compares the `Origin` header against the request `Host`; cross-origin WebSocket upgrades are rejected.
- When `--tls=none`, the operator is responsible for TLS termination at the reverse proxy. A startup warning is printed to stderr.
- Client IP is read from `X-Forwarded-For` when present (for rate limiting behind a proxy). Operators should ensure untrusted clients cannot spoof this header.

---

### 12.16 Example usage

**stack.yaml:**

```yaml
project: home-services
runtime: docker

serve:
  exec:
    enabled: true
    allowed:       # omit or empty = all containers permitted
      - app
      - debug
  edit:
    enabled: true  # allow browser editor for the stack file
  use:
    enabled: false # prevent switching to other stack files from the browser
```

**Starting the server:**

```bash
# Plain HTTP (behind nginx/caddy proxy)
CONTAINERCTL_TOKEN=mysecrettoken containerctl serve --address :9090 --file stack.yaml

# Self-signed HTTPS (LAN access, browser warning expected)
CONTAINERCTL_TOKEN=mysecrettoken containerctl serve \
  --address :9090 --tls self-signed --file stack.yaml

# Let's Encrypt (publicly reachable server)
CONTAINERCTL_TOKEN=mysecrettoken containerctl serve \
  --address :443 --tls letsencrypt --tls-domain containerctl.example.com --file stack.yaml
```

**Browser session flow:**

1. Navigate to `https://<host>:9090` → redirected to `/login`.
2. Enter token → session cookie set → redirected to `/terminal`.
3. Prompt shows `containerctl [stack.yaml]> `.
4. Type `status` → output streams to terminal.
5. Type `logs nginx --follow` → log lines stream; press Enter to stop.
6. Type `exec app bash` → amber PTY session opens in the same terminal; type `exit` or press Ctrl+D to return.
7. Type `edit` → full-screen CodeMirror editor opens; `:w` saves, `:q` closes.
8. Type `use /other/stack.yaml` → prompt updates to `containerctl [other-stack.yaml]> `.
9. Click Logout → session cleared → redirected to `/login`.

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
