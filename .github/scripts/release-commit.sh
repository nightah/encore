#!/usr/bin/env bash
# Prints X.Y.Z when <commit> is a merged release commit: its subject is exactly "release: vX.Y.Z (#N)", as the squash merge of a release pull request writes it. For any other commit it prints nothing. It fails when the subject names a version that web/ or docs/package.json don't carry.
#
# A release branch's own "release: vX.Y.Z" commit has no pull request number, so it never counts: only the squash-merged commit on main does. tag-release.yml uses this to decide what to tag, and release.yml to check a tag before building it.
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <commit>" >&2
  exit 2
fi

# version_at prints the version in path's package.json at rev.
version_at() {
  git show "$1:$2/package.json" | jq -r .version
}

subject="$(git log -1 --format=%s "$1")"

if [[ ! "${subject}" =~ ^release:\ v([0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?)\ \(#[0-9]+\)$ ]]; then
  exit 0
fi
version="${BASH_REMATCH[1]}"

for d in web docs; do
  got="$(version_at "$1" "${d}")"
  if [[ "${got}" != "${version}" ]]; then
    echo "${d}/package.json is ${got}, but \"${subject}\" releases ${version}" >&2
    exit 1
  fi
done

echo "${version}"
