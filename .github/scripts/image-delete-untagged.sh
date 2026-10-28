#!/usr/bin/env bash
# Deletes the container image whose digests image-superseded.sh prints, read from stdin: a manifest list first, then its platform manifests. Each is deleted only while it has no tags, and the platform manifests only once their manifest list is gone, so an image that still has another tag is left whole.
#
# Usage: image-delete-untagged.sh <owner> <package> < digests. Needs GH_TOKEN with packages:write; the package belongs to a user, as ghcr.io/nightah/encore does.
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <owner> <package> < digests" >&2
  exit 2
fi

owner="$1"
package="${2,,}"
api="/users/${owner}/packages/container/${package}/versions"

mapfile -t digests
[[ ${#digests[@]} -gt 0 ]] || exit 0

# digest, version ID and number of tags for every version.
versions="$(gh api --paginate "${api}?per_page=100" --jq '.[] | [.name, .id, (.metadata.container.tags | length)] | @tsv')"

for i in "${!digests[@]}"; do
  id="" tags=""
  read -r id tags < <(awk -F'\t' -v d="${digests[i]}" '$1 == d { print $2, $3 }' <<< "${versions}") || true
  if [[ -z "${id}" ]]; then
    echo "${digests[i]}: not in the registry"
    continue
  fi
  if [[ "${tags}" -gt 0 ]]; then
    echo "${digests[i]}: still tagged; left alone"
    # A manifest list that's still tagged still needs its platform manifests.
    [[ ${i} -eq 0 ]] && exit 0
    continue
  fi
  gh api -X DELETE "${api}/${id}" > /dev/null
  echo "${digests[i]}: deleted (version ${id})"
done
