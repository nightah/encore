#!/usr/bin/env bash
# Commits "release: vX.Y.Z", stamping the next version into web/ and docs/package.json; <bump> is patch, minor, major or an exact version (see next-version.sh).
#
# On a release/… branch it commits there. Anywhere else it fetches origin/main, works out the next version from it, and commits on a new release/vX.Y.Z branch from it, without an upstream, so a release never carries a feature branch's commits. make version runs it.
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <patch|minor|major|X.Y.Z>" >&2
  exit 2
fi

case "$1" in
  patch | minor | major) ;;
  *)
    if [[ ! "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
      echo "'$1' is not patch, minor, major or a version such as 1.2.3" >&2
      exit 1
    fi
    ;;
esac

pnpm="${PNPM:-pnpm}"
next_version="$(dirname "${BASH_SOURCE[0]}")/next-version.sh"

# version_at prints the version in path's package.json at rev.
version_at() {
  git show "$1:$2/package.json" | node -e 'let s = ""; process.stdin.on("data", (d) => (s += d)).on("end", () => console.log(JSON.parse(s).version))'
}

if [[ "$(git branch --show-current)" == release/* ]]; then
  base=HEAD

  if ! git diff --quiet HEAD -- web/package.json docs/package.json; then
    echo "web/ or docs/package.json has uncommitted changes" >&2
    exit 1
  fi
else
  if ! git diff --quiet HEAD; then
    echo "commit or stash your changes first; a new release branch would carry them" >&2
    exit 1
  fi

  git fetch -q origin main
  base=origin/main
fi

web="$(version_at "${base}" web)"
docs="$(version_at "${base}" docs)"

if [[ "${web}" != "${docs}" ]]; then
  echo "web/package.json is ${web} but docs/package.json is ${docs}" >&2
  exit 1
fi

next="$("${next_version}" "${web}" "$1")"

if [[ "${base}" == origin/main ]]; then
  git switch -q -c "release/v${next}" --no-track origin/main
  echo "Created release/v${next} from origin/main."
fi

"${pnpm}" -C web pkg set version="${next}"
"${pnpm}" -C docs pkg set version="${next}"
git commit -q -s -m "release: v${next}" -m "Stamp ${next} into the web and docs packages for the v${next} release, up from ${web}." -- web/package.json docs/package.json

echo "Committed release: v${next} (${web} -> ${next}). Push this branch and open a pull request titled \"release: v${next}\"."
