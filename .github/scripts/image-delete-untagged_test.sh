#!/usr/bin/env bash
# Tests image-delete-untagged.sh against a stand-in gh that serves a fixed list of package versions and records deletions: run it from anywhere.
set -uo pipefail

script="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/image-delete-untagged.sh"
dir="$(mktemp -d)"
trap 'rm -rf "${dir}"' EXIT
failed=0

cat > "${dir}/gh" <<'GH'
#!/usr/bin/env bash
if [[ "$2" == -X ]]; then
  echo "${4##*/}" >> "${GH_DELETED}"
else
  # The real gh applies --jq; this serves what it would print.
  cat "${GH_VERSIONS}"
fi
GH
chmod +x "${dir}/gh"
export PATH="${dir}:${PATH}" GH_VERSIONS="${dir}/versions" GH_DELETED="${dir}/deleted"

# digest, version ID, number of tags.
versions() { printf '%s\t%s\t%s\n' "$@" > "${GH_VERSIONS}"; }

expect() {
  local name="$1" input="$2" want="$3" got
  : > "${GH_DELETED}"
  "${script}" nightah Encore <<< "${input}" > /dev/null
  got="$(paste -sd' ' "${GH_DELETED}")"
  if [[ "${got}" != "${want}" ]]; then
    echo "FAIL: ${name}: deleted '${got}', want '${want}'"
    failed=1
  fi
}

versions sha256:list 10 0 sha256:p1 11 0 sha256:p2 12 0
expect "untagged image" $'sha256:list\nsha256:p1\nsha256:p2' "10 11 12"

versions sha256:list 10 1 sha256:p1 11 0 sha256:p2 12 0
expect "manifest list still tagged" $'sha256:list\nsha256:p1\nsha256:p2' ""

versions sha256:list 10 0 sha256:p2 12 0
expect "a platform manifest already gone" $'sha256:list\nsha256:p1\nsha256:p2' "10 12"

versions sha256:list 10 0 sha256:p1 11 2
expect "a tagged platform manifest" $'sha256:list\nsha256:p1' "10"

versions sha256:other 9 0
expect "nothing to delete" "" ""

exit "${failed}"
