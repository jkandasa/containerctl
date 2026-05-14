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
| `status [name...]` | Show state, image, uptime, and drift for all managed containers. |
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

Global flags: `-f/--file PATH` (default `./stack.yaml`), `--runtime docker|podman`, `--socket PATH`, `-o text|json`, `--no-color`, `-v`.

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

Containers with `update_policy: manual` in YAML are never touched.

---

## Private registries

`containerctl` reads credentials automatically from the standard locations used by `docker login` and `podman login`:

| Runtime | Credential file |
|---------|----------------|
| Docker | `$DOCKER_CONFIG/config.json` → `~/.docker/config.json` |
| Podman | `$REGISTRY_AUTH_FILE` → `$XDG_RUNTIME_DIR/containers/auth.json` → `~/.config/containers/auth.json` → `/etc/containers/auth.json` |

If credentials live somewhere else (CI secret mounts, non-standard paths), point to the file explicitly:

```yaml
project: myapp
auth_file: /run/secrets/registry-auth.json
```

The file must be in Docker/Podman JSON format (`{"auths": {...}}`), the same file `docker login` writes.

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
    read_only: bool
    tmpfs: [/tmp]
    labels:
      com.example.key: value
```

**Environment variable expansion:** `$VAR` and `${VAR}` in any string value are substituted from the host environment at load time. Undefined variables expand to an empty string.

Use `$$` to pass a literal `$` through to the container without expansion — useful for shell-style defaults you want the container to evaluate:

```yaml
command:
  - "--log-level=$${LOG_LEVEL:-info}"   # container receives --log-level=${LOG_LEVEL:-info}
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
