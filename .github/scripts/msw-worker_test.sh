#!/usr/bin/env bash
# Tests msw-worker.sh against a throwaway web directory: run it from anywhere.
set -uo pipefail

script="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/msw-worker.sh"
failed=0

fail() {
  echo "FAIL: $*"
  failed=1
}

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

# setup makes ${tmp}/web with a worker from msw <worker version> and msw <installed version> installed the way pnpm does, through a symlink into .pnpm; an empty version leaves that file out.
setup() {
  rm -rf "${tmp:?}"/*
  mkdir -p "${tmp}/web/mocks" "${tmp}/web/node_modules/.pnpm/msw/node_modules/msw"
  if [[ -n "$1" ]]; then
    printf "/* eslint-disable */\n\nconst PACKAGE_VERSION = '%s'\nconst INTEGRITY_CHECKSUM = 'abc'\n" "$1" > "${tmp}/web/mocks/mockServiceWorker.js"
  fi
  if [[ -n "$2" ]]; then
    printf '{\n  "name": "msw",\n  "version": "%s"\n}\n' "$2" > "${tmp}/web/node_modules/.pnpm/msw/node_modules/msw/package.json"
    ln -s .pnpm/msw/node_modules/msw "${tmp}/web/node_modules/msw"
  fi
}

# expect <exit code> <description> runs the script on ${tmp}/web from ${tmp}.
expect() {
  local got
  (cd "${tmp}" && "${script}" web) > "${tmp}/out" 2>&1
  got=$?
  [[ "${got}" -eq "$1" ]] || fail "$2: exit ${got}, want $1 ($(cat "${tmp}/out"))"
}

setup 3.0.3 3.0.3
expect 0 "matching versions"

setup 3.0.2 3.0.3
expect 1 "a worker older than msw"
grep -q 'msw 3.0.2 but msw 3.0.3 is installed' "${tmp}/out" || fail "the mismatch doesn't name both versions: $(cat "${tmp}/out")"

setup 3.0.3 3.1.0
expect 1 "msw newer than the worker"

setup 3.0.3 3.0.3-beta.1
expect 1 "a pre-release of the same version"

setup "" 3.0.3
expect 1 "a missing worker"

setup 3.0.3 ""
expect 1 "msw not installed"

setup 3.0.3 3.0.3
sed -i '/PACKAGE_VERSION/d' "${tmp}/web/mocks/mockServiceWorker.js"
expect 1 "a worker without PACKAGE_VERSION"

# The web directory defaults to web, relative to where it runs.
setup 3.0.3 3.0.3
(cd "${tmp}" && "${script}") > /dev/null 2>&1 || fail "it doesn't default to web"

(cd "${tmp}" && "${script}" web extra) > /dev/null 2>&1
[[ $? -eq 2 ]] || fail "two arguments didn't print the usage"

if [[ ${failed} -eq 0 ]]; then
  echo "ok"
fi

exit "${failed}"
