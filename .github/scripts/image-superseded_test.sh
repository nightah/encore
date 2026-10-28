#!/usr/bin/env bash
# Tests image-superseded.sh: run it from anywhere.
set -uo pipefail

script="$(dirname "${BASH_SOURCE[0]}")/image-superseded.sh"
dir="$(mktemp -d)"
trap 'rm -rf "${dir}"' EXIT
failed=0

# A manifest list file named $1 with the given platform manifest digests.
list() {
  local name="$1"
  shift
  jq -cn --args '{schemaVersion: 2, manifests: [$ARGS.positional[] | {digest: .}]}' "$@" > "${dir}/${name}"
}

digest() { echo "sha256:$(sha256sum < "${dir}/$1" | cut -d' ' -f1)"; }

expect() {
  local old="$1" new="$2" want="$3" got
  got="$("${script}" "${dir}/${old}" "${dir}/${new}" | paste -sd' ')"
  if [[ "${got}" != "${want}" ]]; then
    echo "FAIL: ${old} -> ${new} gave '${got}', want '${want}'"
    failed=1
  fi
}

list a sha256:a1 sha256:a2 sha256:a3
list b sha256:b1 sha256:b2 sha256:b3
list shared sha256:a1 sha256:b2 sha256:b3
cp "${dir}/a" "${dir}/a-again"

# The old manifest list and all its platform manifests, list first.
expect a b "$(digest a) sha256:a1 sha256:a2 sha256:a3"
# Platform manifests the new list still uses are kept.
expect a shared "$(digest a) sha256:a2 sha256:a3"
# The tag didn't move.
expect a a-again ""

exit "${failed}"
