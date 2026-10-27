#!/usr/bin/env bash
# Tests next-version.sh: run it from anywhere.
set -uo pipefail

script="$(dirname "${BASH_SOURCE[0]}")/next-version.sh"
failed=0

expect() {
  local current="$1" bump="$2" want="$3" got
  if got="$("${script}" "${current}" "${bump}" 2> /dev/null)"; then :; else got="<none>"; fi
  if [[ "${got}" != "${want}" ]]; then
    echo "FAIL: '${current}' '${bump}' gave '${got}', want '${want}'"
    failed=1
  fi
}

expect 0.1.0 patch 0.1.1
expect 0.1.0 minor 0.2.0
expect 0.1.0 major 1.0.0
expect 1.9.9 patch 1.9.10
expect 1.9.9 minor 1.10.0
expect 1.9.9 major 2.0.0

# A pre-release bumps to the release it leads to, as npm version does.
expect 1.2.0-rc.1 patch 1.2.0
expect 1.2.3-rc.1 minor 1.3.0
expect 1.2.0-rc.1 minor 1.2.0
expect 2.0.0-rc.1 major 2.0.0
expect 2.1.0-rc.1 major 3.0.0

# An explicit version is taken as given, as long as it's newer.
expect 0.1.0 0.1.1 0.1.1
expect 0.1.0 1.0.0-rc.1 1.0.0-rc.1
expect 1.0.0-rc.1 1.0.0 1.0.0
expect 0.1.0 0.1.0 "<none>"
expect 0.2.0 0.1.9 "<none>"
expect 1.0.0 1.0.0-rc.1 "<none>"

expect 0.1.0 "" "<none>"
expect 0.1.0 Patch "<none>"
expect 0.1.0 v0.1.1 "<none>"
expect 0.1.0 1.2 "<none>"
expect 0.1 patch "<none>"
expect dev patch "<none>"

if [[ ${failed} -eq 0 ]]; then
  echo "ok"
fi

exit "${failed}"
