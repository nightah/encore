#!/usr/bin/env bash
# Prints the version a release of <current> becomes: "patch", "minor" or "major" bumps that part of X.Y.Z and zeroes the parts after it, and a pre-release bumps to the release it leads to, as npm version does (1.2.0-rc.1 patch is 1.2.0). Any other bump is an explicit version such as 1.2.3 or 2.0.0-rc.1, printed as given when it is newer than <current>.
#
# release-version.sh (make version) uses it to stamp the next version into web/ and docs/package.json.
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <current> <patch|minor|major|X.Y.Z>" >&2
  exit 2
fi

semver='^([0-9]+)\.([0-9]+)\.([0-9]+)(-[0-9A-Za-z.-]+)?$'

if [[ ! "$1" =~ ${semver} ]]; then
  echo "current version '$1' is not X.Y.Z" >&2
  exit 1
fi

major="${BASH_REMATCH[1]}" minor="${BASH_REMATCH[2]}" patch="${BASH_REMATCH[3]}" pre="${BASH_REMATCH[4]}"

case "$2" in
  patch)
    [[ -n "${pre}" ]] || patch=$((patch + 1))
    ;;
  minor)
    [[ -n "${pre}" && "${patch}" -eq 0 ]] || minor=$((minor + 1))
    patch=0
    ;;
  major)
    [[ -n "${pre}" && "${minor}" -eq 0 && "${patch}" -eq 0 ]] || major=$((major + 1))
    minor=0 patch=0
    ;;
  *)
    if [[ ! "$2" =~ ${semver} ]]; then
      echo "'$2' is not patch, minor, major or a version such as 1.2.3" >&2
      exit 1
    fi

    # Newer means a larger X.Y.Z, or the same one as a release of a pre-release, or a later pre-release of it.
    current="${major}.${minor}.${patch}" next="${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.${BASH_REMATCH[3]}" next_pre="${BASH_REMATCH[4]}"

    if [[ "${current}" == "${next}" ]]; then
      if [[ -n "${pre}" && -z "${next_pre}" ]]; then
        newer=1
      elif [[ -n "${pre}" && -n "${next_pre}" && "${pre}" != "${next_pre}" && "$(printf '%s\n' "${pre}" "${next_pre}" | sort -V | tail -n 1)" == "${next_pre}" ]]; then
        newer=1
      else
        newer=0
      fi
    elif [[ "$(printf '%s\n' "${current}" "${next}" | sort -V | tail -n 1)" == "${next}" ]]; then
      newer=1
    else
      newer=0
    fi

    if [[ "${newer}" -eq 0 ]]; then
      echo "$2 is not newer than $1" >&2
      exit 1
    fi

    echo "$2"
    exit 0
    ;;
esac

echo "${major}.${minor}.${patch}"
