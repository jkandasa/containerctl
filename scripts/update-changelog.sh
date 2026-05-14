#!/usr/bin/env bash
set -euo pipefail

VERSION="$1"
DATE=$(date +%Y-%m-%d)

awk -v ver="$VERSION" -v date="$DATE" '
/^## \[Unreleased\]/ { seen=1 }
/^---$/ && seen && !done {
    print
    print ""
    print "## [" ver "] - " date
    print ""
    print "---"
    done=1
    next
}
{ print }
' CHANGELOG.md > CHANGELOG.tmp && mv CHANGELOG.tmp CHANGELOG.md

echo "Updated CHANGELOG.md — added $VERSION"
