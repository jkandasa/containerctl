# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Added
- `group_add` field in `stack.yaml` — adds supplementary GIDs to the container process without changing the user or primary group. Accepts a list of group IDs or names (e.g. `["1500", "docker"]`). Changes are included in the config hash and trigger recreation on `apply`.
- `exec <name> [command...]` — run a command inside a running container. Defaults to `/bin/sh`. Allocates a PTY and sets raw terminal mode when stdin is a terminal; window resize is forwarded automatically. Non-TTY invocations (piped stdin) run without a PTY.
- `start --follow` and `restart --follow` stream container logs immediately after the container is running. Requires a single container name (not compatible with `--all` or multiple names).
- `serve` — starts an HTTP/HTTPS web server exposing a browser-based management terminal. After authenticating with a shared token, the browser session can run all containerctl subcommands. Supports plain HTTP, self-signed TLS, Let's Encrypt, and custom cert/key.
- `serve` brute-force login protection: 5 consecutive failures from the same IP trigger a 30-second block with a browser-side countdown timer.
- `serve` browser terminal tab-completion for command names, container names, and per-command flags.
- `serve` command history (up/down arrow) per browser session.
- `serve` browser exec sessions: `exec <container> [command]` opens a full PTY-based interactive shell in the browser terminal. Gated by `serve.exec.enabled: true` in `stack.yaml`; an optional `serve.exec.allowed` list restricts which containers may be exec'd into. Both interactive and non-interactive `exec` invocations are blocked when disabled.
- `serve` browser stack file editor: `edit` opens the active stack file in a full-screen CodeMirror editor with vim keybindings and YAML syntax highlighting. Keys: `:w`/`Ctrl+S` save; `:wq`/`:x` save+quit; `:q` quit; `:q!`/`Ctrl+Q` force-quit. Concurrent edit protection via ETag — 409 Conflict preserves unsaved edits. Gated by `serve.edit.enabled: true`.
- `serve` `use <path>` terminal command switches the active stack file for the current browser session without restarting the server. The prompt updates to show the new stack basename. Gated by `serve.use.enabled: true`.
- `serve` per-command `--file` override: if the user includes `--file` or `-f` in a terminal command, the server uses that path instead of injecting the session default.
- `serve.exec.enabled`, `serve.exec.allowed`, `serve.edit.enabled`, `serve.use.enabled` fields in `stack.yaml` — opt-in gates for web terminal features. All default to disabled for minimal-privilege deployments.

### Fixed
- `exec` no longer leaves the terminal in raw mode (invisible input) after the container exits with a non-zero code. `os.Exit` bypasses deferred functions; the terminal state is now restored explicitly before exit.

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
