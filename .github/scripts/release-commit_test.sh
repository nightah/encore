#!/usr/bin/env bash
# Tests release-commit.sh against a throwaway repository: run it from anywhere.
set -uo pipefail

script="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/release-commit.sh"
failed=0

fail() {
  echo "FAIL: $*"
  failed=1
}

# Commits in the throwaway repository ignore the user's git config, signing and hooks.
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=test GIT_AUTHOR_EMAIL=test@example.com GIT_COMMITTER_NAME=test GIT_COMMITTER_EMAIL=test@example.com

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

git init -q -b main "${tmp}"
mkdir -p "${tmp}/web" "${tmp}/docs"

# commit stamps web/ and docs/package.json with the given versions and commits them with subject.
commit() {
  printf '{\n  "name": "web",\n  "version": "%s"\n}\n' "$1" > "${tmp}/web/package.json"
  printf '{\n  "name": "docs",\n  "version": "%s"\n}\n' "$2" > "${tmp}/docs/package.json"
  git -C "${tmp}" add -A
  git -C "${tmp}" commit -q --allow-empty -m "$3"
}

# check runs the script on HEAD, leaving its output in out and its exit status in status.
check() {
  out="$(cd "${tmp}" && "${script}" HEAD 2> /dev/null)"
  status=$?
}

# A squash-merged release commit prints its version.
commit 0.1.1 0.1.1 "release: v0.1.1 (#12)"
check
[[ ${status} -eq 0 && "${out}" == 0.1.1 ]] || fail "the merged release commit printed '${out}' (exit ${status}), not 0.1.1"

commit 1.0.0-rc.1 1.0.0-rc.1 "release: v1.0.0-rc.1 (#40)"
check
[[ ${status} -eq 0 && "${out}" == 1.0.0-rc.1 ]] || fail "a pre-release printed '${out}' (exit ${status}), not 1.0.0-rc.1"

# Anything else isn't a release: nothing printed, success.
commit 0.1.1 0.1.1 "release: v0.1.1"
check
[[ ${status} -eq 0 && -z "${out}" ]] || fail "the release branch's own commit (no pull request number) was taken for a release"

commit 0.1.1 0.1.1 "fix: later (#13)"
check
[[ ${status} -eq 0 && -z "${out}" ]] || fail "an ordinary commit was taken for a release"

commit 0.1.1 0.1.1 "release: v0.1.1 (#12) and more"
check
[[ ${status} -eq 0 && -z "${out}" ]] || fail "a subject with trailing text was taken for a release"

# A release commit whose packages carry another version is an error.
commit 0.1.1 0.1.0 "release: v0.1.1 (#12)"
check
[[ ${status} -ne 0 ]] || fail "it accepted docs/package.json at 0.1.0 for release 0.1.1"

commit 0.1.0 0.1.0 "release: v0.1.1 (#12)"
check
[[ ${status} -ne 0 ]] || fail "it accepted both packages at 0.1.0 for release 0.1.1"

(cd "${tmp}" && "${script}" > /dev/null 2>&1) && fail "it ran without a commit"

if [[ ${failed} -eq 0 ]]; then
  echo "ok"
fi

exit "${failed}"
