# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

---

## [v1.4.1] - 2026-05-16

### Added
- `status --watch` (`-w`) refreshes the output repeatedly with flicker-free in-place rendering. Default interval is `2s`; `--interval` accepts Go duration strings (`500ms`, `5s`, `1m`, etc.). Exits cleanly on Ctrl+C. Each line is erased to end-of-line (`\033[K`) before overwriting so no characters from a wider previous render bleed through.

### Changed
- `status` now runs all per-container API calls (image meta, inspect, stats) in parallel, reducing wall-clock time from ~1s×N to ~1-2s regardless of container count.
- `status` no longer shows CPU/MEM columns by default. Use `--stats` to enable live usage collection and display. This keeps the default output fast and avoids the Docker stats collection delay for every run.

### Fixed
- `logs` no longer shows garbage characters (`\xef\xbf\xbd` / `?`) at the start of each line. Docker multiplexes stdout and stderr with 8-byte binary frame headers when the container has no TTY; the stream is now demultiplexed transparently before output. TTY containers are unaffected.
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

### Fixed
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

### Fixed
- Private registry pulls now work correctly. The Docker/Podman SDK does not read credential files automatically; credentials are now loaded and passed explicitly on every pull.

### Added
- Credential auto-detection covers both Docker (`~/.docker/config.json`) and Podman (`$XDG_RUNTIME_DIR/containers/auth.json`, `~/.config/containers/auth.json`, `/etc/containers/auth.json`) out of the box. Environment overrides `$DOCKER_CONFIG` and `$REGISTRY_AUTH_FILE` are respected.
- `auth_file` field in `stack.yaml` — point to an explicit credential file (Docker/Podman JSON format) when auto-detection is not sufficient (e.g. CI, rootless Podman with non-standard paths, or multiple credential stores on the same host).

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
