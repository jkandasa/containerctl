#!/usr/bin/env bash
# Extracts the changelog section for a given version tag.
# Usage: ./extract-changelog.sh v1.2.0
set -euo pipefail

VERSION="${1#v}"  # strip leading v if present

awk -v tag="$VERSION" '
/^## \[/ {
    if (found) exit
    if ($0 ~ "\\[v?" tag "\\]") found=1
    next
}
found { print }
' CHANGELOG.md
