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
containerctl status    # see running state and drift
```

---

## Commands

| Command | Description |
|---|---|
| `apply [name...]` | Reconcile host to YAML. Names limit scope to those containers only. |
| `diff [name...]` | Show what `apply` would change without making changes. Exit 3 if changes pending. |
| `status [name...]` | Show image, state, ports, uptime, restart count, and sync status. Use `-o json\|yaml` for rich output including image digest/size, resource limits, container name, and timestamps. |
| `check-update [name...] [--apply]` | Check registry for newer tags or digest changes. `--apply` upgrades patch versions and rewrites `stack.yaml`. |
| `upgrade <name>` | Force-pull and recreate one container regardless of config hash. |
| `restart [name...] \| --all` | Stop, remove, recreate, and start from current config — no pull. |
| `pull [name...]` | Pull images without reconciling. |
| `down [name...]` | Stop and remove managed containers. No names = whole project. |
| `stop <name...> \| --all` | Transient stop. Container kept on disk; next `apply` restarts it. |
| `start <name...> \| --all` | Start a stopped container without reconciling. |
| `disable <name...>` | Persistent off via state file. Survives reboots and `apply`. |
| `enable <name...>` | Remove from state file and reconcile. |
| `logs <name> [--follow] [--tail N]` | Stream container logs. |
| `version` | Print version and runtime reachability. |

Global flags: `-f/--file PATH` (default `./stack.yaml`), `--runtime docker|podman`, `--socket PATH`, `-o text|json|yaml`, `--no-color`, `-v`.

### Structured output

`-o json` and `-o yaml` emit richer data than the text table:

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
  started_at: 2026-05-14T10:22:00Z
  restart_count: 2
  last_restart: 2026-05-15T03:11:42Z
  sync: ok
  resources:
    cpus: "2"
    memory: 2.0 GiB
```

Fields that are not applicable are omitted (`resources` when no limits are set, `exit_code` when running, `last_restart` when `restart_count` is 0, etc.).

---

### Other runtimes (OrbStack, Colima, Rancher Desktop)

Any Docker API-compatible runtime works. Set the socket path in `stack.yaml` and omit `runtime:`:

```yaml
project: myapp
socket: /Users/you/.orbstack/run/docker.sock
```

The `--socket` flag overrides `stack.yaml`; `--runtime` overrides `stack.runtime`.

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
