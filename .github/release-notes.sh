#!/usr/bin/env bash
# Print the CHANGELOG.md section for a release tag, as its release notes.
#
#   .github/release-notes.sh v0.1.3
#
# The changelog is the one place a release is described; the release workflow
# reads its notes from here rather than from the tag's message. A tag with no
# section, or an empty one, fails -- a release nobody has described is not
# ready to publish.
set -euo pipefail

tag="${1:?usage: release-notes.sh <tag>}"
version="${tag#v}"
changelog="${2:-CHANGELOG.md}"

# From "## [0.1.3]" up to the next version heading or the link references at
# the foot, whichever comes first. index() rather than a regex, so the dots in
# the version match only themselves.
notes=$(awk -v heading="## [$version]" '
  index($0, heading) == 1 { found = 1; next }
  found && (/^## \[/ || /^\[[^]]+\]: /) { exit }
  found { print }
' "$changelog" | sed -e '/./,$!d' | sed -e ':a' -e '/^\n*$/{$d;N;ba' -e '}')

if [ -z "$notes" ]; then
  echo "$changelog has no section for $version; add one before tagging $tag" >&2
  exit 1
fi
printf '%s\n' "$notes"
