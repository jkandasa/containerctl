# Release Process

## Cutting a release

Before releasing, add your changes under `## [Unreleased]` in `CHANGELOG.md`. Then run:

```sh
make release REL_VERSION=v1.2.0
```

This will:
1. Validate the version doesn't already exist as a tag or in the changelog.
2. Insert `## [v1.2.0] - YYYY-MM-DD` into `CHANGELOG.md` under the `[Unreleased]` section.
3. Commit the changelog update.
4. Create the tag and push both the commit and the tag to `origin/main`.

The GitHub Actions `release` workflow then triggers automatically and:
- Builds binaries for all platforms.
- Publishes a GitHub release with the changelog section as release notes.

---

## Re-pushing a tag (fix a bad release)

```sh
git push origin :refs/tags/v1.2.0  # delete remote tag
git tag -d v1.2.0                  # delete local tag
git tag v1.2.0                     # recreate at current commit
git push origin v1.2.0             # re-triggers the workflow
```

---

## Devel (rolling) release

Every push to `main` automatically updates the `devel` pre-release on GitHub with fresh binaries. No manual steps needed.

The devel binary is stamped `devel-<short-sha>` (see the workflow `VERSION=`). `containerctl upgrade --version=devel` compares that stamp to the commit recorded in the release body (`Built from commit <sha>`) and skips the download when they already match.

## Self-upgrade

```sh
containerctl upgrade                 # latest stable, only if newer than this binary
containerctl upgrade --check         # report only (exit 3 if an update is available)
containerctl upgrade --version=v1.2.4
containerctl upgrade --version=devel # rolling main build, only if the remote commit differs
```

Requires write access to the current executable. Restart `containerctl serve` after upgrading so it loads the new binary. Set `GITHUB_TOKEN` if you hit GitHub's unauthenticated API rate limit.

---

## CI

Pull requests and pushes to `main` run `golangci-lint` (`.github/workflows/lint.yaml`). Locally: `make lint`.
