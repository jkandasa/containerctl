# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Added
- `status` now shows a **PORTS** column with the actual mapped ports (including host IP when bound to a specific address). UPTIME is shown next to STATE for better readability.

### Fixed
- `status` port display no longer duplicates entries — Docker reports each binding twice (IPv4 `0.0.0.0` and IPv6 `::`); bindings are now deduplicated and ports bound to all interfaces are shown without an IP prefix.

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
