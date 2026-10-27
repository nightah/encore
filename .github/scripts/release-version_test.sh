#!/usr/bin/env bash
# Tests release-version.sh against a throwaway repository and its origin: run it from anywhere.
set -uo pipefail

script="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/release-version.sh"
failed=0

fail() {
  echo "FAIL: $*"
  failed=1
}

# Commits in the throwaway repositories ignore the user's git config, signing and hooks.
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=test GIT_AUTHOR_EMAIL=test@example.com GIT_COMMITTER_NAME=test GIT_COMMITTER_EMAIL=test@example.com

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

# setup makes a clone of a fresh origin whose main holds web/ and docs/package.json at version 0.1.0.
setup() {
  rm -rf "${tmp:?}"/*
  git init -q --bare -b main "${tmp}/origin.git"
  git init -q -b main "${tmp}/work"
  mkdir -p "${tmp}/work/web" "${tmp}/work/docs"
  for d in web docs; do
    printf '{\n  "name": "%s",\n  "version": "0.1.0"\n}\n' "${d}" > "${tmp}/work/${d}/package.json"
  done
  git -C "${tmp}/work" add -A
  git -C "${tmp}/work" commit -q -m "init"
  git -C "${tmp}/work" remote add origin "${tmp}/origin.git"
  git -C "${tmp}/work" push -q origin main
}

run() {
  (cd "${tmp}/work" && "${script}" "$@") > /dev/null 2>&1
}

version_of() {
  git -C "${tmp}/work" show "$1:$2/package.json" | sed -n 's/^  "version": "\(.*\)"$/\1/p'
}

# From main it branches release/v0.1.1 off origin/main, without an upstream, and commits there.
setup
if run patch; then
  [[ "$(git -C "${tmp}/work" branch --show-current)" == release/v0.1.1 ]] || fail "patch from main didn't switch to release/v0.1.1"
  [[ "$(git -C "${tmp}/work" log -1 --format=%s)" == "release: v0.1.1" ]] || fail "patch from main didn't commit release: v0.1.1"
  [[ "$(version_of HEAD web)" == 0.1.1 && "$(version_of HEAD docs)" == 0.1.1 ]] || fail "patch from main didn't stamp both packages"
  git -C "${tmp}/work" log -1 --format=%b | grep -q '^Signed-off-by: ' || fail "the release commit has no sign-off"
  git -C "${tmp}/work" rev-parse -q --verify '@{u}' > /dev/null && fail "release/v0.1.1 tracks a branch"
  [[ "$(version_of main web)" == 0.1.0 ]] || fail "main changed"
else
  fail "patch from main failed"
fi

# It branches from origin/main, not the branch it was run on, and reads the version there.
setup
git -C "${tmp}/work" switch -q -c feature
sed -i 's/0\.1\.0/0.5.0/' "${tmp}/work/web/package.json" "${tmp}/work/docs/package.json"
git -C "${tmp}/work" commit -q -am "feature"
if run minor; then
  [[ "$(git -C "${tmp}/work" branch --show-current)" == release/v0.2.0 ]] || fail "minor from a feature branch didn't branch release/v0.2.0 from origin/main"
  [[ "$(git -C "${tmp}/work" rev-parse HEAD~1)" == "$(git -C "${tmp}/work" rev-parse origin/main)" ]] || fail "the release branch doesn't start at origin/main"
else
  fail "minor from a feature branch failed"
fi

# On a release branch it commits in place.
setup
git -C "${tmp}/work" switch -q -c release/next --no-track origin/main
if run major; then
  [[ "$(git -C "${tmp}/work" branch --show-current)" == release/next ]] || fail "major on a release branch switched branches"
  [[ "$(version_of HEAD web)" == 1.0.0 ]] || fail "major on a release branch didn't stamp 1.0.0"
else
  fail "major on a release branch failed"
fi

# Refusals leave everything as it was.
setup
echo '{}' > "${tmp}/work/stray.json"
git -C "${tmp}/work" add stray.json
run patch && fail "it ran with a staged change outside a release branch"
[[ "$(git -C "${tmp}/work" branch --show-current)" == main ]] || fail "a refused run switched branches"

setup
git -C "${tmp}/work" switch -q -c release/next --no-track origin/main
sed -i 's/0\.1\.0/0.1.5/' "${tmp}/work/docs/package.json"
git -C "${tmp}/work" commit -q -am "drift"
run patch && fail "it ran with web/ and docs/package.json disagreeing"

setup
run bogus && fail "it ran with a bad bump"
git -C "${tmp}/work" rev-parse -q --verify release/v0.1.1 > /dev/null && fail "a bad bump created a branch"

setup
git -C "${tmp}/work" branch --no-track release/v0.1.1 origin/main
run patch && fail "it ran when release/v0.1.1 already exists"

if [[ ${failed} -eq 0 ]]; then
  echo "ok"
fi

exit "${failed}"
