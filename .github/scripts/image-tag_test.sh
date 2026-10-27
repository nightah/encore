#!/usr/bin/env bash
# Tests image-tag.sh: run it from anywhere.
set -uo pipefail

script="$(dirname "${BASH_SOURCE[0]}")/image-tag.sh"
failed=0

expect() {
  local branch="$1" want="$2" got
  if got="$("${script}" "${branch}" 2> /dev/null)"; then :; else got="<none>"; fi
  if [[ "${got}" != "${want}" ]]; then
    echo "FAIL: '${branch}' gave '${got}', want '${want}'"
    failed=1
  fi
}

expect fix/abc fix-abc
expect feat/kodi/queue feat-kodi-queue
expect renovate/github.com-coder-websocket-1.x renovate-github.com-coder-websocket-1.x
expect i18n i18n
expect "docs/what's new" docs-what-s-new
expect fix//double fix-double
expect UPPER_case.1 UPPER_case.1
expect -leading-dash leading-dash
expect .hidden hidden
expect "$(printf 'a%.0s' {1..140})" "$(printf 'a%.0s' {1..128})"

# Tags owned by releases, fork pull requests and signatures are never taken by a branch.
expect latest "<none>"
expect main "<none>"
expect 0.1.0 "<none>"
expect 0.1 "<none>"
expect 1 "<none>"
expect v0.2.0 "<none>"
expect 1.0.0-rc.1 "<none>"
expect PR12 "<none>"
expect sha256-abc.sig "<none>"
expect / "<none>"
expect "" "<none>"

# Near misses are still branches.
expect PR-notes PR-notes
expect latest-fix latest-fix
expect 1.x-backport 1.x-backport

if [[ ${failed} -eq 0 ]]; then
  echo "ok"
fi

exit "${failed}"
