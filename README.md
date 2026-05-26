# containerctl


Declarative container management for a single host. Write a YAML file describing what should be running — `containerctl apply` makes it so.

No daemon. No cluster. One binary, one file.

---

## Why

Running containers on a single host usually means a pile of shell scripts — one per container, inconsistent flags, drift you can't see. `containerctl` replaces that with a single source of truth:

- **Drift detection.** `diff` and `status` show exactly what's out of sync before you touch anything.
- **Hash-driven reconciliation.** Only recreates a container when its config actually changed.
- **Update awareness.** `check-update` queries the registry for newer semver tags and digest changes. `--apply` upgrades patch versions automatically.
- **Safe by default.** Never touches containers it doesn't own. Partial `apply` never removes unrelated containers.
- **Docker and Podman.** Same tool, same YAML, same behaviour on either runtime.

---

## Install

```sh
make build          # produces ./containerctl
```

---

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
      POSTGRES_DB: app
      POSTGRES_USER: app
      POSTGRES_PASSWORD: ${PG_PASSWORD}
    networks: [backend]
    resources:
      memory: "2g"

  - name: redis
    image: redis:7.2-alpine
    restart: unless-stopped
    networks: [backend]
    resources:
      memory: "256m"
```

```sh
containerctl diff      # preview what will change
containerctl apply     # reconcile host to desired state
containerctl status    # see running state and sync status
```

---

## Commands

| Command | Description |
|---|---|
| `apply [name...]` | Reconcile host to YAML. Names limit scope to those containers only. |
| `diff [name...]` | Show what `apply` would change without making changes. Exit 3 if changes pending. |
| `status [name...] [--stats] [--watch]` | Show image, state, ports, uptime, restarts, and sync status. `--watch` (`-w`) refreshes repeatedly (default every 2s; override with `--interval 500ms\|5s\|1m`). `--stats` also shows live CPU/memory usage (adds ~1-2s). Use `-o json\|yaml` for rich output including image digest/size, resource limits, network IPs, mount paths, and timestamps. |
| `check-update [name...] [--apply] [--follow]` | Check registry for newer tags or digest changes. `--apply` upgrades patch versions and rewrites `stack.yaml`. `--follow` streams logs after applying (requires `--apply` and exactly one container name). |
| `upgrade <name>` | Force-pull and recreate one container regardless of config hash. |
| `restart [name...] \| --all [--follow]` | Stop, remove, recreate, and start from current config — no pull. `--follow` streams logs after restart (single container only). |
| `pull [name...]` | Pull images without reconciling. |
| `down [name...]` | Stop and remove managed containers. No names = whole project. |
| `stop <name...> \| --all` | Transient stop. Container kept on disk; next `apply` restarts it. |
| `start <name...> \| --all [--follow]` | Start a stopped container without reconciling. `--follow` streams logs after start (single container only). |
| `disable <name...>` | Persistent off via state file. Survives reboots and `apply`. |
| `enable <name...>` | Remove from state file and reconcile. |
| `exec <name> [command...]` | Run a command in a running container. Defaults to `/bin/sh`. Attaches a TTY when stdin is a terminal; window resize is handled automatically. |
| `logs <name> [--follow] [--tail N]` | Stream container logs. |
| `images [name...] [--unused]` | List local images. `--unused` shows only images not referenced by any container or stack declaration. `-o json\|yaml` includes per-image container list (name, state). |
| `volumes [--unused] [--size]` | List local volumes with attached containers. `--unused` shows only dangling volumes. `--size` fetches disk usage from the daemon (triggers a daemon-side scan). `-o json\|yaml` includes per-volume mount details (source, destination, read_only) and host mountpoint. |
| `networks [--unused]` | List user-defined networks. `--unused` shows only networks not connected to any container. `-o json\|yaml` includes per-network container list with IP address and gateway. |
| `prune [--images] [--volumes] [--networks] [--all] [--dry-run] [--force]` | Remove unused local resources. `--all` is equivalent to `--images --volumes --networks`. `--dry-run` previews without removing. `--force` skips the confirmation prompt. |
| `version` | Print version, Go runtime, and container engine details (version, API, OS/arch, kernel). Supports `-o json\|yaml`. |
| `serve` | Start an HTTP/HTTPS server exposing a browser-based management terminal. See [Web terminal](#web-terminal-serve) below. |

Global flags: `-f/--file PATH` (default `./stack.yaml`), `--runtime docker|podman`, `--socket PATH`, `-o text|json|yaml`, `--no-color`, `-v`.

### Web terminal (`serve`)

`containerctl serve` starts an HTTP/HTTPS server with a browser-based terminal. After logging in with a shared token, the browser session behaves like the CLI.

```bash
# Plain HTTP (behind a TLS-terminating proxy)
CONTAINERCTL_TOKEN=mysecrettoken containerctl serve --listen :9090 --file stack.yaml

# Self-signed HTTPS (LAN, browser warning expected)
CONTAINERCTL_TOKEN=mysecrettoken containerctl serve --tls self-signed --file stack.yaml

# Let's Encrypt
CONTAINERCTL_TOKEN=mysecrettoken containerctl serve \
  --tls letsencrypt --tls-domain containerctl.example.com --file stack.yaml
```

| Flag | Default | Description |
|------|---------|-------------|
| `--listen ADDR` | `:8080` | TCP address to listen on. |
| `--token TOKEN` | — | Required. Also read from `CONTAINERCTL_TOKEN` env var. |
| `--tls MODE` | `none` | `none` \| `self-signed` \| `letsencrypt` \| `custom` |
| `--tls-domain DOMAIN` | — | Public domain (required for `letsencrypt`). |
| `--tls-cert PATH` | — | Certificate file (required for `custom`). |
| `--tls-key PATH` | — | Key file (required for `custom`). |
| `--session-ttl DURATION` | `24h` | Session validity duration. |

**Browser features:**

- All `containerctl` commands available in the browser terminal.
- Tab-completes command names and container names.
- `edit` — opens the active stack file in a full-screen vim-style editor (YAML syntax highlighting, vim keybindings, concurrent-edit protection). Keys: `:w` / `Ctrl+S` save · `:wq` / `:x` save+quit · `:q` quit · `:q!` / `Ctrl+Q` force-quit. Requires `serve.edit.enabled: true`.
- `use /path/to/other-stack.yaml` — switch the active stack file for the current session without restarting the server. Requires `serve.use.enabled: true`.
- `exec <container> bash` — opens a full interactive PTY session in the browser. Requires `serve.exec.enabled: true`.
- Login brute-force protection: 5 failures → 30-second block with countdown timer.

**Exec from the browser** is opt-in because it gives full shell access to the container. Enable it in `stack.yaml`:

```yaml
serve:
  exec:
    enabled: true
    allowed:        # omit or leave empty to permit all containers
      - myapp
      - debug-sidecar
```

### exec

`exec` opens an interactive shell (or runs any command) inside a running container:

```sh
containerctl exec postgres            # /bin/sh
containerctl exec postgres bash       # bash
containerctl exec postgres -- ps aux  # non-interactive
```

When stdin is a terminal, a PTY is allocated and the terminal is put into raw mode automatically — no `-it` flags needed. Window resize is forwarded to the container so `vim`, `less`, and similar tools work correctly. The terminal is always restored cleanly on exit, including when the command exits with a non-zero code.

### Structured output

`-o json` and `-o yaml` emit richer data than the text table. All JSON and YAML output is indented with 2 spaces.

```yaml
# containerctl status -o yaml
- name: postgres
  container_name: home-services_postgres
  image: postgres:16
  image_digest: sha256:3a9f…c21b
  image_size: 127.3 MiB
  state: running
  container_id: a3f2b1c94d8e
  ports:
    - host_ip: 127.0.0.1
      host_port: "5432"
      container_port: "5432"
      protocol: tcp
  networks:
    - name: backend
      ip_address: 172.18.0.3
      gateway: 172.18.0.1
  mounts:
    - type: volume
      name: pgdata
      source: /var/lib/docker/volumes/pgdata/_data
      destination: /var/lib/postgresql/data
    - type: bind
      source: /host/config
      destination: /etc/config
      read_only: true
  started_at: 2026-05-14T10:22:00Z
  restart_count: 2
  last_restart: 2026-05-15T03:11:42Z
  sync: ok
  resources:
    cpus: "2"
    memory: 2.0 GiB
```

```yaml
# containerctl status --stats -o yaml
- name: postgres
  ...
  cpu_percent: 0.42
  memory_used: 38.2 MiB
  memory_used_bytes: 40042496
```

Fields that are not applicable are omitted (`resources` when no limits are set, `exit_code` when running, `last_restart` when `restart_count` is 0, `networks`/`mounts` when empty, etc.). `cpu_percent`, `memory_used`, and `memory_used_bytes` only appear when `--stats` is passed.

**Resource listing commands** also produce enriched structured output:

```yaml
# containerctl volumes -o yaml
- name: pgdata
  driver: local
  mountpoint: /var/lib/docker/volumes/pgdata/_data
  containers:
    - name: postgres
      state: running
      source: /var/lib/docker/volumes/pgdata/_data
      destination: /var/lib/postgresql/data

# containerctl volumes --size -o yaml  (triggers daemon-side disk scan)
- name: pgdata
  driver: local
  mountpoint: /var/lib/docker/volumes/pgdata/_data
  size: 47399936    # bytes; -1 when driver doesn't report
  containers: [...]

# containerctl networks -o yaml
- id: abc123
  name: backend
  driver: bridge
  containers:
    - name: postgres
      state: running
      ip_address: 172.18.0.3
      gateway: 172.18.0.1

# containerctl images -o yaml
- id: a3f2b1c94d8e
  tags: [postgres:16]
  size: 133169152
  created: 2026-01-10T00:00:00Z
  containers:
    - name: postgres
      state: running
```

---

## Other runtimes (OrbStack, Colima, Rancher Desktop)

Any Docker API-compatible runtime works. Set the socket path in `stack.yaml` and omit `runtime:`:

```yaml
project: myapp
socket: /Users/you/.orbstack/run/docker.sock
```

The `--socket` flag overrides `stack.yaml`; `--runtime` overrides `stack.runtime`.

---

## Network aliases

By default, containers on the same user-defined network reach each other using the container name (e.g. `http://postgres:5432`). `network_aliases` lets you add extra DNS names without changing the container name:

```yaml
containers:
  - name: postgres
    image: postgres:16
    networks: [backend]
    network_aliases:
      - db.backend
      - primary.backend
```

Any container on `backend` can now reach it via any of:

```
http://postgres:5432      # container name (always works)
http://db.backend:5432    # alias
http://primary.backend:5432 # alias
```

Aliases are registered on every network the container joins. Adding, removing, or changing aliases is detected by the config hash and triggers recreation on the next `apply`.

---

## Update detection

```sh
containerctl check-update

NAME        IMAGE                STATUS        NOTE
postgres    postgres:16           patch update  16.1, 16.2, 16.3; major: 17.0.0
redis       redis:7.2-alpine      up-to-date
app         registry.../app:v1.4  digest changed  sha256:3a9f… → sha256:c21b…
vault       vault:1.15            manual
```

`--apply` pulls and recreates containers with patch/minor updates or digest changes. Major version bumps are shown but require a manual tag edit — breaking changes are your call.

Containers with `update_policy: manual` in YAML are checked and reported but never touched by `--apply`. Their STATUS shows `up-to-date (manual)` or `patch update (manual)` so available updates are visible without automatic action.

---

## Private registries

`containerctl` merges credentials from all auto-detected locations plus any explicit `auth_file`. Auto-detected files are checked in this order (first-wins among them); `auth_file` from `stack.yaml` is overlaid last and takes highest precedence on any conflict.

| Source | Path |
|--------|------|
| Podman env | `$REGISTRY_AUTH_FILE` |
| Docker env | `$DOCKER_CONFIG/config.json` |
| Docker default | `~/.docker/config.json` |
| Podman rootless | `$XDG_RUNTIME_DIR/containers/auth.json` |
| Podman rootless fallback | `~/.config/containers/auth.json` |
| Podman root | `/etc/containers/auth.json` |
| **stack.yaml** (highest) | value of `auth_file:` |

If credentials live somewhere else (CI secret mounts, non-standard paths), point to the file explicitly:

```yaml
project: myapp
auth_file: /run/secrets/registry-auth.json
```

`auth_file` overrides auto-detected credentials for the same registry, but credentials from auto-detected files for other registries remain available. The file must be in Docker/Podman JSON format (`{"auths": {...}}`), the same file `docker login` writes.

Credentials are used by `pull`, `apply`, `upgrade`, `check-update`, and remote digest checks — all registry operations go through the same credential resolution.

---

## Three ways to turn something off

| Need | How |
|---|---|
| Quick troubleshoot | `containerctl stop nginx` — transient, next `apply` restarts it |
| Off until I say so | `containerctl disable nginx` — persistent, survives `apply` and reboots |
| Gone from the stack | `disabled: true` in YAML — tracked in git, container removed on next `apply` |

---

## stack.yaml reference

```yaml
project: string          # required. Namespace; final container name = <project>_<name>.
runtime: docker|podman   # optional. Default: docker.
socket: string           # optional. Override socket path. If set, runtime type is optional.
data_path: string        # optional. Base dir for relative volume and env_file paths.
auth_file: string        # optional. Path to a Docker/Podman credential JSON file.

serve:                   # optional. Controls "containerctl serve" behaviour.
  exec:
    enabled: false       # set true to allow exec commands.
    allowed: []          # container names permitted for exec; empty = all allowed.
  edit:
    enabled: false       # set true to allow the browser stack file editor.
  use:
    enabled: false       # set true to allow switching stacks with the "use" command.

networks:
  - name: string         # required.
    driver: string       # optional. Default: bridge.

containers:
  - name: string         # required.
    image: string        # required. e.g. postgres:16
    disabled: bool       # optional. apply removes the container and skips creation.
    update_policy: auto|manual  # optional. manual = skip check-update entirely.
    restart: no|on-failure|always|unless-stopped
    ports:
      - "HOST:CONTAINER"
      - "IP:HOST:CONTAINER/proto"
    volumes:
      - "/host/path:/container/path"
      - "named-volume:/container/path:ro"
    env:
      KEY: value
    env_file:
      - "secrets.env"
    networks: [backend]
    network_aliases:
      - db.backend            # reachable as this name on all connected networks
    depends_on: [postgres]   # start order only; not a healthcheck gate.
    resources:
      cpus: "2.0"
      memory: "512m"
      pids_limit: 200
    healthcheck:
      test: ["CMD-SHELL", "pg_isready"]
      interval: 10s
      timeout: 3s
      retries: 5
      start_period: 30s
    user: "1000:1000"
    hostname: string
    working_dir: string
    dns: [8.8.8.8]
    group_add:
      - "1500"             # add supplementary GID without changing user or primary group
    cap_add: [NET_ADMIN]
    cap_drop: [ALL]
    privileged: bool
    security_opt:
      - "seccomp=unconfined"
      - "apparmor=unconfined"
    read_only: bool
    tmpfs: [/tmp]
    labels:
      com.example.key: value
```

**Environment variable expansion** is applied to every string value at load time:

| Syntax | Behaviour |
|--------|-----------|
| `$VAR` / `${VAR}` | Value of `VAR`; empty string if unset |
| `${VAR:-default}` | Value of `VAR` if set and non-empty, otherwise `default` |
| `$$` | Literal `$` — no expansion, passed through to the container as-is |

```yaml
env:
  MODE: "${APP_MODE:-production}"       # uses "production" if APP_MODE is unset

command:
  - "--log-level=${LOG_LEVEL:-info}"    # containerctl resolves the default at load time
  - "--raw=$${LOG_LEVEL:-info}"         # container receives ${LOG_LEVEL:-info} literally
```

---

## How reconciliation works

Each managed container carries a `containerctl.config-hash` label — a SHA-256 of its normalized spec. On `apply`:

1. Pull images for containers that will be created or recreated (parallel).
2. Create any declared networks that don't exist.
3. For each container in dependency order: **create** if missing, **recreate** if hash changed, **skip** if identical.
4. On a full apply (no name filter): remove containers and networks that are managed but no longer declared.

Partial `apply name` only affects the named containers — it never removes orphaned containers or networks.

---

## Full specification

See [docs/SPEC.md](docs/SPEC.md) for the complete design — runtime interface, hash inputs, plan algorithm, error model, and future roadmap.
