#!/usr/bin/env bash
# Prints the container image tag a branch is published under: every run of characters a tag can't hold ([A-Za-z0-9_.-] only) becomes "-", so fix/abc becomes fix-abc. A tag can't start with "." or "-" and holds at most 128 characters.
#
# Fails, printing nothing, when the result is empty or could overwrite a tag something else owns: releases (latest, 0.1.0, 0.1, 0), the main branch's image (main), pull requests from forks (PR<number>) and signatures or attestations (sha256-…). Renovate's branches get no image either: a dependency bump is tested by CI, and nobody pulls its image. CI, publish and cleanup all name tags with this script, so they agree.
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <branch>" >&2
  exit 2
fi

if [[ "$1" == renovate/* ]]; then
  echo "branch '$1' is Renovate's and gets no image" >&2
  exit 1
fi

tag="$(sed -E 's/[^A-Za-z0-9_.-]+/-/g; s/^[.-]+//' <<< "$1")"
tag="${tag:0:128}"

if [[ -z "${tag}" ]] || [[ "${tag}" =~ ^(latest|main|PR[0-9]+|v?[0-9]+(\.[0-9]+)*([-+].*)?|sha256-.*)$ ]]; then
  echo "branch '$1' has no image tag of its own" >&2
  exit 1
fi

echo "${tag}"
