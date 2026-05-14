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
