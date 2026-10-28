#!/usr/bin/env bash
# Prints the digests of the image a tag moved off, one per line: the manifest list from <old> (the raw manifest the tag pointed at before), then each of its platform manifests, leaving out any that <new> (the raw manifest it points at now) still uses. Prints nothing when the tag didn't move.
#
# A manifest list's digest is the SHA-256 of its exact bytes, as skopeo inspect --raw prints them. images-publish.yml deletes what this prints with image-delete-untagged.sh.
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <old manifest> <new manifest>" >&2
  exit 2
fi

digest() { echo "sha256:$(sha256sum < "$1" | cut -d' ' -f1)"; }
children() { jq -r '.manifests[]?.digest' "$1"; }

old="$(digest "$1")"
new="$(digest "$2")"

if [[ "${old}" == "${new}" ]]; then
  exit 0
fi

keep="$(printf '%s\n' "${new}"; children "$2")"

{ echo "${old}"; children "$1"; } | grep -vxF -f <(printf '%s\n' "${keep}") || true
