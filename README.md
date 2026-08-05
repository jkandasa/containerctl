# containerctl


Declarative container management for a single host. Write a YAML file describing what should be running — `containerctl apply` makes it so.

No daemon. No cluster. One binary, one file.

---

## Why

Running containers on a single host usually means a pile of shell scripts — one per container, inconsistent flags, drift you can't see. `containerctl` replaces that with a single source of truth:

- **Drift detection.** `apply --dry-run` and `status` show exactly what's out of sync before you touch anything.
- **Hash-driven reconciliation.** Only recreates a container when its config actually changed.
- **Update awareness.** `update` queries the registry for newer semver tags and digest changes. `--apply` upgrades patch versions automatically.
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
containerctl apply --dry-run  # preview what will change
containerctl apply            # reconcile host to desired state
containerctl status           # see running state and sync status
```

---

## Commands

| Command | Description |
|---|---|
| `apply [name...] [-l selector] [--dry-run]` | Reconcile host to YAML. Names and/or `-l`/`--label` limit scope (kubectl-style: `KEY`, `!KEY`, `KEY=VALUE`, `KEY!=VALUE`; comma-separated AND). `--dry-run` shows the plan without making any changes (exits 3 if changes pending). |
| `status [name...] [-l selector] [--stats] [--watch]` | Show image, state, ports, created age, uptime, restarts, and sync status. Filter with names and/or `-l`. `--watch` (`-w`) refreshes repeatedly (default every 2s; override with `--interval 500ms\|5s\|1m`). `--stats` also shows live CPU/memory usage and throttling data (adds ~1-2s). Use `-o json\|yaml` for rich output including image digest/size, resource limits, network IPs, mount paths, and timestamps (`created_at`, `started_at`, `last_restart`) in the host's local timezone. |
| `update [name...] [-l selector] [--apply] [--follow]` | Check registry for newer tags or digest changes. Names and/or `-l` limit scope. `--apply` upgrades patch versions and rewrites `stack.yaml`. Containers held back by `update_policy: manual` or a persistent `disable` are reported with a `(manual)`/`(disabled)` suffix and never applied. `--follow` streams logs after applying (requires `--apply` and exactly one selected container, and attaches only if that container was actually updated). |
| `repull <name>` | Force-pull the image and recreate a container, bypassing the config hash. |
| `restart <name...> \| --all \| -l selector [--follow]` | Recreate containers from current config (stop, remove, create, start) — no pull. `--follow` streams logs after restart (single container only). |
| `pull [name...]` | Pull images without reconciling. |
| `down [name...] [-l selector]` | Stop and remove managed containers. No names/labels = whole project. |
| `stop <name...> \| --all \| -l selector` | Stop containers; they stay on disk and restart on next apply. |
| `start <name...> \| --all \| -l selector [--follow]` | Start stopped containers without reconciling. `--follow` streams logs after start (single container only). |
| `disable <name...>` | Persistent off via state file. Survives reboots and `apply`. |
| `enable <name...>` | Remove from state file and reconcile. |
| `exec <name> [command...]` | Run a command in a running container. Defaults to `/bin/sh`. Attaches a TTY when stdin is a terminal; window resize is handled automatically. |
| `logs <name> [--follow] [--tail N]` | Stream container logs. `--tail -1` shows all (default); `--tail 0` shows none. |
| `images [name...] [--unused]` | List local images. `--unused` shows only images not referenced by any container or stack declaration. `-o json\|yaml` includes per-image container list (name, state). |
| `volumes [--unused] [--size]` | List local volumes with attached containers. `--unused` shows only dangling volumes. `--size` fetches disk usage from the daemon (triggers a daemon-side scan). `-o json\|yaml` includes per-volume mount details (source, destination, read_only) and host mountpoint. |
| `networks [--unused]` | List user-defined networks. `--unused` shows only networks not connected to any container. `-o json\|yaml` includes per-network container list with IP address and gateway. |
| `prune [--images] [--volumes] [--networks] [--all] [--dry-run] [--force]` | Remove unused host-wide resources (not project-scoped). `--all` is equivalent to `--images --volumes --networks`. `--dry-run` previews without removing. `--force` skips the confirmation prompt. |
| `generate [name...] [-O FILE]` | Write a `stack.yaml` describing containers that already exist on the host. This is the import path from `docker run`/`docker compose`. No names = every container on the host. Output goes to stdout, or to `FILE` with `-O` (created mode `0600`, since env values often hold secrets). Nothing on the host is modified. See [Importing existing containers](#importing-existing-containers). |
| `version` | Print version, Go runtime, and container engine details (version, API, OS/arch, kernel). Supports `-o json\|yaml`. |
| `serve` | Start an HTTP/HTTPS server exposing a browser-based management terminal. See [Web terminal](#web-terminal-serve) below. |

Global flags: `-f/--file PATH` (default `./stack.yaml`), `--runtime docker|podman`, `--socket PATH`, `-o console|json|yaml`, `--no-color` (also respects `NO_COLOR` env var).

### Selecting containers with `-l` / `--label`

Six commands accept kubectl-style label selectors against each container's stack YAML `labels:` map:

| Command | Notes |
|---------|--------|
| `apply` | Partial apply when `-l` or names are set (no orphan cleanup) |
| `status` | Filter the status table |
| `update` | Limit registry check / `--apply`; `--follow` needs exactly one selected container |
| `start` | Requires a name, `-l`, or `--all` |
| `stop` | Requires a name, `-l`, or `--all` |
| `restart` | Requires a name, `-l`, or `--all` |
| `down` | No names/`-l` = whole project |

**Not supported on:** `pull`, `repull`, `disable`, `enable`, `logs`, `exec`, `images`, `volumes`, `networks`, `prune`, `generate`.

#### Selector syntax

| Selector | Meaning |
|----------|---------|
| `KEY` | Label **must exist** (any value) |
| `!KEY` | Label **must not exist** |
| `KEY=VALUE` | Present and equal to `VALUE` |
| `KEY!=VALUE` | Absent, or present with a different value |

- Commas separate terms (**AND**): `-l release,environment=production`
- Multiple `-l` flags are also **AND**
- Names + `-l` → **intersection**
- Matching uses **stack YAML** `labels:` only (not live host labels alone)
- Changing a label is part of the config hash and will recreate the container on next full/partial apply of that container

```yaml
# stack.yaml
containers:
  - name: web
    image: myapp:1
    labels:
      app: frontend
      environment: production
      release: "v1"
  - name: web-dev
    image: myapp:1
    labels:
      app: frontend
      environment: development
      release: "v1"
  - name: redis
    image: redis:7
    labels:
      app: cache
      environment: production
```

```sh
# has "release" AND environment=production  → web
containerctl apply -l release,environment=production

# frontend, not development  → web
containerctl status -l app=frontend,environment!=development

# no "release" label  → redis (and any unlabeled containers)
containerctl stop -l '!release'

# names ∩ labels
containerctl restart web web-dev redis -l environment=production
# → web, redis
```

Quote `!` in shells that treat it specially: `-l '!debug'` or `-l '!release,environment=production'`.

### Web terminal (`serve`)

`containerctl serve` starts an HTTP/HTTPS server with a browser-based terminal. After logging in with a shared token, the browser session behaves like the CLI.

```bash
# Plain HTTP (behind a TLS-terminating proxy)
CONTAINERCTL_TOKEN=mysecrettoken containerctl serve --address :9090 --file stack.yaml

# Self-signed HTTPS (LAN, browser warning expected)
CONTAINERCTL_TOKEN=mysecrettoken containerctl serve --tls self-signed --file stack.yaml

# Let's Encrypt
CONTAINERCTL_TOKEN=mysecrettoken containerctl serve \
  --tls letsencrypt --tls-domain containerctl.example.com --file stack.yaml
```

| Flag | Default | Description |
|------|---------|-------------|
| `--address ADDR` | `:8080` | TCP address to listen on. |
| `--token TOKEN` | — | Required. Also read from `CONTAINERCTL_TOKEN` env var. |
| `--tls MODE` | `none` | `none` \| `self-signed` \| `letsencrypt` \| `custom` |
| `--tls-domain DOMAIN` | — | Public domain (required for `letsencrypt`). |
| `--tls-cert PATH` | — | Certificate file (required for `custom`). |
| `--tls-key PATH` | — | Key file (required for `custom`). |
| `--session-ttl DURATION` | `24h` | Session validity duration. |

**Browser features:**

- All `containerctl` commands available in the browser terminal.
- Tab-completes command names and container names.
- Color themes: dark (default) / light / auto (follows system preference). Toggle button in the top bar; persists across reloads and also affects the login page and editor.
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

`-o json` and `-o yaml` emit richer data than the console table. All JSON and YAML output is indented with 2 spaces.

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
  created_at: "2026-05-01T13:30:00.580976787+05:30"
  started_at: "2026-05-14T15:52:00.123456789+05:30"
  restart_count: 2
  last_restart: "2026-05-15T08:41:42.456789012+05:30"
  sync: ok
  resources:
    cpus: "2"
    memory: 2.0 GiB
```

```yaml
# containerctl status --stats -o yaml
- name: postgres
  ...
  stats:
    cpu_percent: 0.42
    cpu_throttled_periods: 42
    cpu_throttled_time: 1.3s
    cpu_throttled_time_ns: 1300000000
    memory_used: 38.2 MiB
    memory_used_bytes: 40042496
```

Fields that are not applicable are omitted. The `stats` object is present only when `--stats` is passed. Within it, `cpu_throttled_time` and `cpu_throttled_time_ns` are omitted when zero; `memory_fail_count` is omitted when zero (cgroups v1 only; always absent on cgroups v2 systems). Timestamps (`created_at`, `started_at`, `last_restart`) are emitted in the host's local timezone (e.g. `"2026-05-31T06:51:27.580976787+05:30"`).

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

## Importing existing containers

Already running containers you started with `docker run` or `docker compose`? `generate` writes the equivalent `stack.yaml` so you can adopt them instead of retyping them:

```bash
# Everything on the host, to stdout
containerctl generate --project home-services

# Just two containers, into a file (mode 0600)
containerctl generate --project home-services home-services_mosquitto home-services_whoami -O stack.yaml

# Verify before touching anything
containerctl apply --dry-run -f stack.yaml
```

For containers containerctl already manages, `generate` → `apply --dry-run` reports **no changes**: the output is the inverse of what `apply` created.

What it deliberately leaves out, so the file stays readable:

- settings identical to the image defaults (`command`, `entrypoint`, `env`, `user`, `working_dir`, `labels`, `healthcheck`)
- `containerctl.*` and `com.docker.compose.*` labels, which are tool bookkeeping rather than your configuration
- anonymous volumes, whose IDs cannot be reproduced, so they are emitted as commented-out entries for you to replace with a real path
- exposed-only ports (no host binding), and DNS aliases the runtime invents (container name, hostname, short ID)
- `env_file`, `depends_on`, `update_policy` and `disabled`, none of which are recoverable from a running container

Anything that cannot be represented is reported as a `WARN:` line on stderr, so `2>/dev/null` gives you a clean file and the warnings stay reviewable. Two cases deserve a look:

- **Foreign networks.** containerctl prefixes networks with the project name, so a compose network like `shop_default` cannot be reused as-is: `apply` would create `<project>_shop_default` and attach the container there. Either rename the network or keep the container out of the generated stack.
- **Secrets.** `env:` values are copied verbatim, tokens and passwords included. That is why `-O` creates the file with mode `0600`. Review it before committing.

Output is deterministic: regenerating an unchanged host produces a byte-identical file, so it is safe to keep the result in git and re-run.

---

## Update detection

```sh
containerctl update

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

Credentials are used by `pull`, `apply`, `repull`, `update`, and remote digest checks — all registry operations go through the same credential resolution.

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
data_path: string        # optional. Base dir for relative volume and env_file paths
                         # (relative values are resolved against the stack file's directory).
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
    update_policy: auto|manual  # optional. manual = skip update entirely.
    command: [string]    # optional. Overrides image CMD (args to the entrypoint).
    entrypoint: [string] # optional. Overrides image ENTRYPOINT.
    restart: no|on-failure|always|unless-stopped
    ports:
      - "HOST:CONTAINER"
      - "IP:HOST:CONTAINER/proto"
    volumes:
      - "/host/path:/container/path"
      - "named-volume:/container/path:ro"
    env:
      KEY: value
    env_file:                    # string or list; relative paths use data_path (or stack dir)
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
    labels:                    # optional. Applied to the container; also used by -l/--label filters
      app: frontend
      environment: production
      release: "v1"
```

See [Selecting containers with `-l` / `--label`](#selecting-containers-with--l----label) for filter syntax.

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
