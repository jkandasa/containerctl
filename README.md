# containerctl

Declarative container management for one host. One YAML file is the desired state; `containerctl apply` makes the host match it.

No daemon. No cluster. One static binary. Works with Docker, Podman, and any Docker-API socket (OrbStack, Colima, Rancher Desktop).

Compose is good at bringing a whole stack up. It is weak at drift, per-container work, and upgrades. containerctl is built for that gap, not as a Compose rewrite.

- `status` and `apply --dry-run` show what is out of sync before anything changes.
- Only a container whose config hash changed is recreated. Identical config is skipped.
- `update` checks the registry; `--apply` can move patch/minor tags. You can `apply`, `restart`, or `update` one name.
- Unmanaged containers are invisible. Only objects labeled `containerctl.managed=true` are touched.

Full design: [docs/SPEC.md](docs/SPEC.md).

## Install

```sh
make build                          # ./containerctl
containerctl upgrade                # latest stable GitHub release
containerctl upgrade --version=devel
containerctl upgrade --check        # report only (exit 3 if an update exists)
```

`upgrade` replaces this binary. It installs a tagged release only if that tag is greater than the current version. `devel` is the rolling `main` build; it is installed only when the remote commit differs. Needs write access to the executable. Restart `serve` after upgrading. `GITHUB_TOKEN` raises the GitHub API rate limit.

## Quick start

```yaml
# stack.yaml
project: home-services
runtime: docker

networks:
  - name: backend

containers:
  - name: postgres
    image: postgres:16
    restart: unless-stopped
    ports:
      - "127.0.0.1:5432:5432"
    volumes:
      - "/srv/pg:/var/lib/postgresql/data"
    env:
      POSTGRES_PASSWORD: ${PG_PASSWORD}
    networks: [backend]
    resources:
      memory: "2g"

  - name: redis
    image: redis:7.2-alpine
    restart: unless-stopped
    networks: [backend]
```

```sh
containerctl apply --dry-run
containerctl apply
containerctl status
```

## Commands

| Command | What it does |
|---|---|
| `apply [name...]` | Reconcile the host to YAML. `--dry-run` prints the plan (exit 3 if changes). `--follow` streams logs after a single container is created or recreated. |
| `status [name...]` | Image, state, ports, age, uptime, restarts, sync. `-w/--watch` (default 2s, `--interval`). `--stats` adds live CPU/memory. |
| `update [name...]` | Check the registry for newer tags or digest changes. `--apply` pulls patch/minor and digest updates and rewrites `stack.yaml`. Major bumps are listed only. `--follow` needs `--apply` and one container. |
| `repull <name>` | Force-pull the image and recreate, ignoring the config hash. |
| `restart <name...>` | Recreate from current config. No pull. Needs names, `-l`, or `--all`. |
| `pull [name...]` | Pull images. No reconcile. |
| `down [name...]` | Stop and remove. No names or `-l` means the whole project. |
| `stop <name...>` | Stop. Stays on disk; next `apply` starts it. Needs names, `-l`, or `--all`. |
| `start <name...>` | Start a stopped container. Needs names, `-l`, or `--all`. |
| `disable <name...>` | Persistent off (state file). Survives reboot and `apply`. |
| `enable <name...>` | Clear disable and reconcile (recreate if hash drifted, else start). |
| `exec <name> [cmd...]` | Run a command. Default `/bin/sh`. TTY and resize when stdin is a terminal. |
| `cp SRC DEST` | Copy host ↔ container or container ↔ container. See [cp](#cp). |
| `logs <name>` | Stream logs. `--follow`, `--tail N` (`-1` all, `0` none). `--info` prints driver/path/size. `--clear` empties a file-backed log in place (not with `--follow` or `--tail`). |
| `images [name...]` | Local images. `--unused` drops anything referenced by a container or the stack. |
| `volumes` | Local volumes. `--unused`, `--size` (daemon disk scan). |
| `networks` | User-defined networks. `--unused`. |
| `prune` | Remove unused host resources. Needs `--images`, `--volumes`, `--networks`, or `--all`. `--dry-run`, `--force`. |
| `generate [name...]` | Write a `stack.yaml` from existing host containers. `-O FILE` (mode `0600`). Host is not changed. |
| `stack [file]` | Set or print the default stack file. `--unset` clears it. |
| `serve` | Browser terminal. See [Web terminal](#web-terminal). |
| `upgrade` | Replace this binary from GitHub. `--version TAG`, `--check`. CLI only. |

`apply`, `status`, `update`, `start`, `stop`, `restart`, and `down` accept `-l/--label`. `--follow` on `apply`, `update`, `repull`, `restart`, `start`, and `enable` needs exactly one selected container.

**Global flags:** `-f/--file` (see [Stack file](#stack-file)), `--runtime docker|podman`, `--socket PATH`, `-o console|json|yaml`, `--no-color` (also `NO_COLOR`), `--version`, `--project`.

## Stack file

Resolution, first match wins:

1. `-f` / `--file`
2. Path saved by `containerctl stack` (`$XDG_CONFIG_HOME/containerctl/config.json`, else `~/.config/containerctl/config.json`)
3. `./stack.yaml`

```sh
containerctl stack /path/to/stack.yaml
containerctl status                     # uses that file
containerctl apply -f other.yaml        # -f wins this run
containerctl stack --unset
```

This is separate from `use` in the web terminal, which is session-only.

## Labels

`-l` matches the container's stack YAML `labels:` (not live host labels alone). Changing a label updates the config hash.

| Selector | Meaning |
|---|---|
| `KEY` | Key present |
| `!KEY` | Key absent |
| `KEY=VALUE` | Equal |
| `KEY!=VALUE` | Absent or different |

Comma-separated terms and repeated `-l` are AND. Names plus `-l` are the intersection. Quote `!` in the shell: `-l '!debug'`.

```sh
containerctl apply -l release,environment=production
containerctl status -l app=frontend,environment!=development
containerctl stop -l '!release'
containerctl restart web redis -l environment=production
```

Partial `apply` (names and/or `-l`) does not remove orphaned containers or networks.

## Stop, disable, remove

| Need | How |
|---|---|
| Temporary | `stop` (next `apply` starts it) |
| Off until you enable | `disable` (state file, survives `apply` and reboot) |
| Removed from the stack | `disabled: true` in YAML (container removed on next `apply`) |

`enable` refuses nothing except a missing name; it clears the state-file disable and then reconciles.

## Updates

```sh
containerctl update
containerctl update --apply
```

`--apply` pulls and recreates for patch/minor tag moves and digest changes. Major tag moves are printed; edit the image tag yourself.

`update_policy: manual` is still checked and shown (`(manual)`), but `--apply` never touches it. Persistently disabled containers show `(disabled)` and are skipped.

`repull` force-pulls a floating tag (`:latest`) and recreates without consulting the hash.

## exec

```sh
containerctl exec postgres
containerctl exec postgres bash
containerctl exec postgres -- ps aux
```

When stdin is a TTY, a PTY is allocated, the host terminal goes raw, and window size (including resize) is forwarded. No `-it`. The terminal is restored on exit, including a non-zero exit code.

## cp

Logical stack name, not `<project>_<name>`. Works on running or stopped containers. Distinct containers stream a tar through the engine. Same-container copies drain to a host temp file first (overlapping archive GET and PUT on one container deadlocks the engine).

```sh
containerctl cp postgres:/dump.sql ./dump.sql
containerctl cp ./config.yaml app:/etc/app/config.yaml
containerctl cp postgres:/dump.sql app:/backup/dump.sql
containerctl cp -L app:/current ./current
containerctl cp -a ./data app:/var/lib/data
containerctl cp app:/etc/nginx/nginx.conf -
containerctl cp - app:/restore < backup.tar
```

| Flag | Meaning |
|---|---|
| `-a`, `--archive` | Keep uid/gid when copying into a container |
| `-L`, `--follow-link` | Follow a symlink at `SRC_PATH` |
| `--overwrite` | Allow replacing a directory with a file in the container (and the reverse) |
| `-q`, `--quiet` | No success line |

A directory copied into an existing directory becomes a subdirectory. Trailing `/.` copies contents only. `-` on the host side is a tar stream (stdin dest must be a directory).

In the web terminal, `cp` needs `serve.exec.enabled` (and `serve.exec.allowed` when set; both containers for container-to-container). Host-path copies are rejected there; stream with `-`, copy between containers, or use the CLI.

## Import (`generate`)

Adopts containers created with `docker run` or Compose:

```sh
containerctl generate --project home-services
containerctl generate --project home-services name1 name2 -O stack.yaml
containerctl apply --dry-run -f stack.yaml
```

For containers containerctl already manages, `generate` then `apply --dry-run` reports no changes.

Omitted on purpose: image-default settings, `containerctl.*` / `com.docker.compose.*` labels, anonymous volumes (commented), exposed-only ports, and fields that cannot be recovered (`env_file`, `depends_on`, `update_policy`, `disabled`). Unrepresentable items print `WARN:` on stderr. `env:` values are copied verbatim (that is why `-O` is mode `0600`). Output is deterministic.

## Web terminal

```sh
CONTAINERCTL_TOKEN=secret containerctl serve --address :9090 --file stack.yaml
CONTAINERCTL_TOKEN=secret containerctl serve --tls self-signed --file stack.yaml
CONTAINERCTL_TOKEN=secret containerctl serve \
  --tls letsencrypt --tls-domain containerctl.example.com --file stack.yaml
```

| Flag | Default | Meaning |
|---|---|---|
| `--address` | `:8080` | Listen address |
| `--token` | (required) | Also `CONTAINERCTL_TOKEN` |
| `--tls` | `none` | `none`, `self-signed`, `letsencrypt`, `custom` |
| `--tls-domain` | | Required for `letsencrypt` |
| `--tls-cert`, `--tls-key` | | Required for `custom` |
| `--session-ttl` | `24h` | Session lifetime |

Browser: same commands as the CLI (except `upgrade`), tab completion, dark/light/auto theme. Login: 5 failures, then a 30s block.

Gated in YAML (off by default):

```yaml
serve:
  exec:
    enabled: true
    allowed: [myapp]          # empty or omitted = all
  edit:
    enabled: true             # vim-style stack editor
  use:
    enabled: true             # session-only `use /path/to/stack.yaml`
```

`edit` keys: `:w` / `Ctrl+S` save, `:wq` / `:x` save and quit, `:q` quit, `:q!` / `Ctrl+Q` discard. Concurrent edits return 409 (ETag). `exec <name> bash` opens a PTY in the same terminal.

## Structured output

`-o json` and `-o yaml` (2-space indent) add fields the console table hides: image digest and size, network IPs, mounts, resource limits, timestamps in the host timezone (`created_at`, `started_at`, `last_restart`). `status --stats` adds a `stats` object (`cpu_percent`, throttle counters, `memory_used`). Zero throttle/fail values are omitted. `images`, `volumes`, and `networks` include the containers attached to each object. `volumes --size` adds byte size (`-1` if the driver does not report).

## Other runtimes

Any Docker-compatible socket works. Set `socket:` and you can omit `runtime:`:

```yaml
project: myapp
socket: /Users/you/.orbstack/run/docker.sock
```

`--socket` overrides YAML. `--runtime` overrides `runtime:`.

## Network aliases

On a user-defined network, the container name is already a DNS name. `network_aliases` adds more, on every network the container joins. Alias changes update the config hash.

```yaml
containers:
  - name: postgres
    networks: [backend]
    network_aliases: [db.backend, primary.backend]
```

Reachable as `postgres`, `db.backend`, and `primary.backend`.

## Private registries

Credentials are merged from auto-detected files (first match per registry), then `auth_file:` from the stack (wins on conflict). Docker/Podman JSON: `{"auths": {...}}`.

| Source | Path |
|---|---|
| Podman env | `$REGISTRY_AUTH_FILE` |
| Docker env | `$DOCKER_CONFIG/config.json` |
| Docker default | `~/.docker/config.json` |
| Podman rootless | `$XDG_RUNTIME_DIR/containers/auth.json` |
| Podman rootless fallback | `~/.config/containers/auth.json` |
| Podman root | `/etc/containers/auth.json` |
| Stack (highest) | `auth_file:` |

Used by `pull`, `apply`, `repull`, `update`, and remote digest checks.

## How apply works

Each managed container has a `containerctl.config-hash` label (SHA-256 of the normalized spec).

1. Pull images for creates and recreates (parallel).
2. Create missing declared networks.
3. In `depends_on` order: create if missing, recreate if the hash changed, skip if identical.
4. Full apply only (no name/`-l` filter): remove managed containers and networks that are no longer declared.

`depends_on` is start order only, not a health gate.

## stack.yaml

```yaml
# Required. Namespace for every managed object on this host.
# Runtime container name is <project>_<name>, e.g. home_postgres.
project: string

# Engine. Default docker. Overridden by --runtime.
runtime: docker|podman

# Docker-API socket. If set, runtime: is optional (OrbStack, Colima, Podman, …).
# Overridden by --socket.
socket: string

# Base dir for relative volume sources and env_file paths (resolved against
# the stack file directory, not the process CWD). Required when files: is used;
# content is always written to <data_path>/files/<name>.
data_path: string

# Docker/Podman auth JSON ({"auths":{...}}). Highest precedence for that registry.
auth_file: string

# Small files embedded in the stack. YAML is the source of truth: the host
# copy is rewritten on every create/recreate. name is a plain identifier.
files:
  - name: app-conf
    content: |
      log_level: info

# Browser terminal (containerctl serve). All off by default.
serve:
  exec:
    enabled: false          # interactive exec and web cp
    allowed: []             # logical names; empty = every container
  edit:
    enabled: false          # vim-style editor for this stack file
  use:
    enabled: false          # session-only `use /other/stack.yaml`

# User-defined networks. Created as <project>_<name> if missing.
networks:
  - name: string
    driver: string          # default bridge

containers:
  - name: string            # required. Logical name used by every command
    image: string           # required. e.g. postgres:16 or registry/app@sha256:…

    # apply removes the container and will not create it again.
    # Different from `containerctl disable` (state file, survives apply).
    disabled: bool

    # auto (default): update --apply may pull patch/minor and digest changes.
    # manual: still listed by update, never applied.
    update_policy: auto|manual

    # Overrides image CMD / ENTRYPOINT. A string is shell-split into argv
    # (quotes and backslashes). It is not executed by a shell.
    #   command: serve --port 8080
    #   command: ["serve", "--port", "8080"]
    command: string|[string]
    entrypoint: string|[string]

    restart: no|on-failure|always|unless-stopped

    ports:
      - "HOST:CONTAINER"              # 8080:80
      - "IP:HOST:CONTAINER/proto"     # 127.0.0.1:5432:5432/tcp

    volumes:
      - "/host/path:/container/path"           # bind
      - "named-volume:/container/path:ro"      # named, read-only

    # Bind-mount a files: entry. Written under data_path/files/ first.
    files:
      - source: app-conf               # must match files[].name
        target: /etc/app/config.yaml   # absolute path in the container
        mode: "0644"                   # host file mode; default 0644
        read_only: true                # default true

    env:
      KEY: value                       # ${VAR} expanded at load time
    env_file: secrets.env              # string or list; KEY=VALUE file

    networks: [backend]                # join these declared networks
    network_aliases: [db.backend]      # extra DNS names on every joined network

    # Start/create order only. Not a healthcheck gate.
    depends_on: [postgres]

    resources:
      cpus: "2.0"                      # CFS quota
      memory: "512m"                   # 512m, 2g, …
      pids_limit: 200

    healthcheck:
      test: ["CMD-SHELL", "pg_isready"]
      interval: 10s
      timeout: 3s
      retries: 5
      start_period: 30s

    user: "1000:1000"                  # uid:gid inside the container
    hostname: string
    working_dir: string
    dns: [8.8.8.8]
    group_add: ["1500"]                # extra GIDs; user/primary group unchanged
    cap_add: [NET_ADMIN]
    cap_drop: [ALL]
    privileged: bool
    security_opt: ["seccomp=unconfined"]
    read_only: bool                    # root filesystem read-only
    tmpfs: [/tmp]

    # Applied on the container. Also the target of -l/--label.
    # Changing a label updates the config hash.
    labels:
      app: frontend
      environment: production
```

Every string is expanded at load time:

| Syntax | Result |
|---|---|
| `$VAR` / `${VAR}` | Value, or empty if unset |
| `${VAR:-default}` | Value if set and non-empty, else `default` |
| `$$` | Literal `$` (passed through to the container) |

```yaml
env:
  MODE: "${APP_MODE:-production}"
command:
  - "--log-level=${LOG_LEVEL:-info}"
  - "--raw=$${LOG_LEVEL:-info}"
```
