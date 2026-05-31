# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Added
- `status` now shows a **CREATED** column (between PORTS and UPTIME) displaying how long ago the container was created (e.g. `16d 4h`). Previously only uptime (time since last start) was shown, and it was incorrectly derived from the container creation timestamp. UPTIME now correctly reflects the time since the container last started. Both `created_at` and `started_at` are included in `-o json|yaml` output.

### Fixed
- `status`: containers in the `disabled` state (disabled via state file, container still present on host) now correctly show CREATED and UPTIME. Previously both were always `-` because the `InspectContainer` detail was not consulted for that code path.
- `status -o json|yaml`: `created_at`, `started_at`, and `last_restart` are now always emitted in the host's local timezone (e.g. `2026-05-31T06:51:27+05:30`) for consistent, human-readable output.

---

## [v1.7.0] - 2026-05-27

### Added
- `images [name...]` — positional arguments now filter images by name or tag substring.
- `NO_COLOR` environment variable is now honoured in addition to `--no-color`.
- `images`, `volumes`, `networks`, and `prune` no longer require a `stack.yaml` to be present. The runtime is determined from `--runtime`/`--socket` flags (defaulting to Docker). If a stack file is found it is still used for runtime/socket settings; `images --unused` and `prune --images` will also cross-reference stack declarations when a file is present.
- Web terminal now supports **color themes**: `dark` (default), `light`, and `auto` (follows system `prefers-color-scheme`). A toggle button (◐/☾/☼) is available in the top bar; the choice is persisted in localStorage and applies to the terminal (xterm), editor (CodeMirror), and login page.

### Fixed
- **Critical:** `containerctl.config-hash` labels were non-deterministic for any container using `env:` or `labels:` (map key iteration order in JSON). This caused unnecessary container recreation on every `apply` after a tool restart. Hash computation is now stable across runs.
- Container names were missing from tab completion in the web terminal (regression after syntax highlighting was added). Internal `status --output json` calls used for completion now always receive clean JSON.
- Expired or invalid sessions on long-lived WebSocket connections (terminal, exec, logs) now properly redirect the browser to `/login?error=expired` with a clear message instead of showing a stuck error.

### Changed
- `--output` flag default value changed from `text` to `console` (`-o console|json|yaml`). The behaviour is identical; `console` better describes the formatted table output.
- `check-update` renamed to `update` — shorter and less redundant (it already implies checking).
- `upgrade` renamed to `repull` — unambiguous: force-pulls the image and recreates the container without consulting the config hash.
- `stop`, `start`, `restart` Use strings updated to `<name...> | --all` to make it clear that at least one name or `--all` is required.
- `restart` Short description updated to "Recreate containers from current config (stop, remove, create, start)".
- `start` Short description updated to "Start stopped containers without reconciling".
- `stop` Short description updated to "Stop containers; they stay on disk and restart on next apply".
- `logs --tail` default changed from `0` to `-1`; semantics are now `-1` = all lines (default), `0` = no lines, `N` = last N lines. Previously `0` meant all, which was counter-intuitive.
- `--interval` on `status` now returns an error when used without `--watch`.
- `prune` Short and Long descriptions now say "host-wide" to make clear the command is not project-scoped.
- `volumes --size` description leads with the cost ("triggers a daemon-side scan; may be slow") rather than burying it.
- `serve --address` description now includes usage examples (`:8080` or `127.0.0.1:9090`).
- Removed phantom `--verbose / -v` flag (was registered but never acted on).
- Web terminal help text and command allowlist updated to reflect renames (`check-update` → `update`, `upgrade` → `repull`).
- Web terminal UI/UX improvements: active stack filename now shown in top bar, better initial spacing, improved focus styles, reconnect link on disconnect, more informative login error messages (including session expiry), and various polish.

---

## [v1.6.0] - 2026-05-26

### Added
- `group_add` field in `stack.yaml` — adds supplementary GIDs to the container process without changing the user or primary group. Accepts a list of group IDs or names (e.g. `["1500", "docker"]`). Changes are included in the config hash and trigger recreation on `apply`.
- `exec <name> [command...]` — run a command inside a running container. Defaults to `/bin/sh`. Allocates a PTY and sets raw terminal mode when stdin is a terminal; window resize is forwarded automatically. Non-TTY invocations (piped stdin) run without a PTY.
- `start --follow` and `restart --follow` stream container logs immediately after the container is running. Requires a single container name (not compatible with `--all` or multiple names).
- `check-update --follow` streams logs after `--apply` completes for a single container. Requires both `--apply` and exactly one container name argument.
- `images [--unused]` — list all local images with tags, size, age, and attached container names. `--unused` filters to images not referenced by any running container or stack declaration. `-o json|yaml` includes per-image container list with name and state.
- `volumes [--unused] [--size]` — list local volumes with driver and attached containers. `--unused` filters to dangling (unmounted) volumes. `--size` fetches disk usage via the daemon's disk-usage endpoint (triggers a daemon-side scan) and adds a SIZE column; in JSON/YAML the size is in bytes (`-1` when the driver does not report). `-o json|yaml` additionally includes the host `mountpoint` and per-container mount details (source, destination, read_only).
- `networks [--unused]` — list user-defined networks (system networks `bridge`, `host`, `none` are excluded). `--unused` filters to networks not connected to any container. `-o json|yaml` includes per-network container list with IP address and gateway. The MANAGED column indicates networks created by containerctl.
- `prune [--images] [--volumes] [--networks] [--all] [--dry-run] [--force]` — remove unused local resources. At least one resource type must be selected; `--all` is equivalent to `--images --volumes --networks`. `--dry-run` previews what would be removed without acting. When stdin is not a terminal, `--force` is required to prevent accidental deletion in scripts.
- `status -o json|yaml` now includes `networks` (name, ip_address, gateway) and `mounts` (type, name, source, destination, read_only) for each container. Tmpfs mounts are excluded. These fields are omitted from the text table output.

### Changed
- All JSON output is now indented with 2 spaces for readability (previously compact single-line).
- All YAML output uses 2-space indentation consistently across all commands.
- `volumes -o json|yaml` includes the host `mountpoint` path and an optional `size` field (populated only when `--size` is passed).
- `networks -o json|yaml` fields are now consistently snake_case with explicit JSON tags (`id`, `name`, `driver`, `labels`).
- `ListContainers` now populates full mount details (`ContainerMount`: type, name, source, destination, read_only) and network details (`ContainerNetworkInfo`: name, ip_address, gateway) instead of plain string slices. This powers the enriched JSON output across `status`, `volumes`, `networks`, and `images`.

### Fixed
- `exec` no longer leaves the terminal in raw mode (invisible input) after the container exits with a non-zero code. `os.Exit` bypasses deferred functions; the terminal state is now restored explicitly before exit.
- `serve` browser terminal no longer shows a double prompt after a blocked or unrecognised command. The `error` message handler no longer calls `writePrompt()` — the `done` message that always follows handles the prompt.
- `volumes -o json` no longer silently omits the `size` field for empty volumes. Changed from `int64` with `omitempty` (which drops `0`) to `*int64` — `nil` means not fetched (omitted), `0` means genuinely empty, `-1` means driver does not report.
- `prune --images` no longer marks images that are actively used by running containers as candidates for removal. Image matching now uses the short 12-character image ID from the container's `ImageID` field as the primary check, with tag name as a fallback.

---

## [v1.5.0] - 2026-05-18

### Added
- `status --watch` (`-w`) refreshes the output repeatedly with flicker-free in-place rendering. Default interval is `2s`; `--interval` accepts Go duration strings (`500ms`, `5s`, `1m`, etc.). Exits cleanly on Ctrl+C.
- `status --stats` shows live CPU and memory usage. Omitted by default to keep status fast; collecting stats adds ~1-2s.
- `network_aliases` field in `stack.yaml` — additional DNS names for a container on its connected networks. Useful for reaching a container via a custom hostname (e.g. `db.backend`) without changing its container name. Aliases are registered on every network the container joins and are included in the config hash so changes trigger recreation.
- `version` now shows container engine details: engine version, API version, OS/arch, and kernel version. Supports `-o json|yaml` for structured output.

### Changed
- `status` now runs all per-container API calls (image meta, inspect, stats) in parallel, reducing wall-clock time from ~1s×N to ~1-2s regardless of container count.

### Fixed
- `logs` no longer shows garbage characters at the start of each line. Docker multiplexes stdout and stderr with 8-byte binary frame headers when the container has no TTY; the stream is now demultiplexed transparently before output. TTY containers are unaffected.
- `status` ports column no longer reorders between refreshes. The Docker API returns port bindings in non-deterministic order; bindings are now sorted by container port, protocol, host port, and host IP for stable output.

---

## [v1.4.0] - 2026-05-16

### Added
- `status` now shows a **PORTS** column with the actual mapped ports (including host IP when bound to a specific address). UPTIME is shown next to STATE for better readability.
- `status` now shows a **RESTARTS** column with the restart count and time since last restart (e.g. `3 (2h 10m)`).
- `status` now shows **CPU** and **MEM** columns with live usage (CPU % across all cores; working-set memory excluding file cache). Non-running containers show `-`. JSON/YAML output includes `cpu_percent`, `memory_used`, and `memory_used_bytes`.
- `socket` field in `stack.yaml` — set the runtime socket path without using `--socket` flag. Enables Docker API-compatible runtimes (OrbStack, Colima, Rancher Desktop) without any runtime-specific code.
- `-o yaml` output format for `status` — emits structured YAML with typed fields: `container_id`, `container_name`, `started_at` and `last_restart` as RFC3339 timestamps, `restart_count` as integer, `ports` as a list of objects, `image_digest`, `image_size`, `resources` (cpus/memory/pids limits), and `exit_code` when applicable.
- `-o json` output for `status` now uses the same rich typed model as YAML instead of display strings.

### Changed
- Credential resolution now **merges** all auto-detected credential files (Docker and Podman standard paths) with `auth_file` from `stack.yaml`. Previously only the first file containing credentials for a registry was used. Now credentials from all sources are available simultaneously; `auth_file` overrides auto-detected entries for the same registry.
- `status` column **DRIFT** renamed to **SYNC**; values changed from `yes`/`no` to `drift`/`ok`. `drift` is highlighted in yellow.
- `check-update` and `RemoteImageDigest` now pass registry credentials (from the same auto-detected + `auth_file` sources used by `pull`) to the token endpoint. Private registry images no longer error with `context deadline exceeded` or `401 Unauthorized`.
- `check-update` now checks `update_policy: manual` containers and reports their update status. The STATUS column shows `up-to-date (manual)` or `patch update (manual)` so the policy is visible at a glance. `--apply` still skips them.

### Fixed
- `check-update` would hang indefinitely when a registry was slow or unresponsive. All registry HTTP calls now have a 30-second per-request timeout; each per-container check is additionally capped at 45 seconds.
- `status` port display no longer duplicates entries — Docker reports each binding twice (IPv4 `0.0.0.0` and IPv6 `::`); bindings are now deduplicated and ports bound to all interfaces are shown without an IP prefix.
- `status` now shows exposed-only ports (internal network ports with no host binding) formatted as `port/proto`, matching `docker ps` style.

---

## [v1.3.0] - 2026-05-15

### Added
- `security_opt` field in `stack.yaml` — passes security options to the container runtime (e.g. `seccomp=unconfined`, `apparmor=unconfined`).

---

## [v1.2.1] - 2026-05-15

### Added
- `${VAR:-default}` syntax in YAML values — uses `default` when `VAR` is unset or empty, resolved by containerctl at load time (distinct from `$$` which passes the expression through to the container).

---

## [v1.2.0] - 2026-05-15

### Added
- `$$` escape in YAML values produces a literal `$` without triggering variable expansion — use it to pass shell-style defaults (e.g. `$${LOG_LEVEL:-info}`) through to the container unchanged.

### Fixed
- Variable expansion now uses `os.Expand` with a custom mapping instead of `os.ExpandEnv`, enabling the `$$` escape.

---

## [v1.1.0] - 2026-05-14

### Added
- Credential auto-detection covers both Docker (`~/.docker/config.json`) and Podman (`$XDG_RUNTIME_DIR/containers/auth.json`, `~/.config/containers/auth.json`, `/etc/containers/auth.json`) out of the box. Environment overrides `$DOCKER_CONFIG` and `$REGISTRY_AUTH_FILE` are respected.
- `auth_file` field in `stack.yaml` — point to an explicit credential file (Docker/Podman JSON format) when auto-detection is not sufficient (e.g. CI, rootless Podman with non-standard paths, or multiple credential stores on the same host).

### Fixed
- Private registry pulls now work correctly. The Docker/Podman SDK does not read credential files automatically; credentials are now loaded and passed explicitly on every pull.

---

## [v1.0.0] - 2026-05-14

### Added
- Initial release of `containerctl`
- Declarative container management via `stack.yaml`
- Docker and Podman runtime support
- `apply`, `diff`, `status` — reconcile, preview, and inspect managed containers
- `check-update [--apply]` — semver-aware registry update detection with automatic patch upgrades
- `upgrade`, `restart`, `pull` — targeted container lifecycle operations
- `stop`, `start` — transient container state control
- `disable`, `enable` — persistent off via state file (survives reboots and `apply`)
- `down` — stop and remove managed containers
- `logs` — stream container logs
- Hash-driven reconciliation — only recreates containers when config actually changes
- Dependency ordering via `depends_on`
- Resource limits: CPU, memory, pids
- `update_policy: manual` to exclude containers from update checks
- Cross-platform binaries: Linux (amd64, arm64, armv7), Windows (amd64, arm64), macOS (amd64, arm64)
