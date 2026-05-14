# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

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
