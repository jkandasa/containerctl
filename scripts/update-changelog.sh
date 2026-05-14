#!/usr/bin/env bash
set -euo pipefail

VERSION="$1"
DATE=$(date +%Y-%m-%d)

# Moves content from [Unreleased] into the new version section and clears [Unreleased].
awk -v ver="$VERSION" -v date="$DATE" '
/^## \[Unreleased\]$/ {
    in_unreleased = 1
    print
    next
}

in_unreleased && /^---$/ {
    in_unreleased = 0
    # Empty [Unreleased] section followed by new versioned section
    print ""
    print "---"
    print ""
    print "## [" ver "] - " date
    printf "%s", content
    print "---"
    content = ""
    next
}

in_unreleased {
    content = content $0 "\n"
    next
}

{ print }
' CHANGELOG.md > CHANGELOG.tmp && mv CHANGELOG.tmp CHANGELOG.md

echo "Updated CHANGELOG.md — added $VERSION"
