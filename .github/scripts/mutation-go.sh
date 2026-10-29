#!/usr/bin/env bash
# Stands in for go during mutation testing: go test exits 1 both when tests fail and when the package doesn't build, so gremlins counts a mutant that doesn't compile as killed (go-gremlins/gremlins#321). This exits 2 for a go test whose package failed to build or vet, which gremlins reports as NOT VIABLE; anything else runs unchanged.
#
# The nightly mutation job links it in as go ahead of the real one on PATH, with MUTATION_GO set to the real go.
set -uo pipefail

if [[ -z "${MUTATION_GO:-}" ]]; then
  echo "MUTATION_GO must be set to the real go binary" >&2
  exit 2
fi

if [[ "${1:-}" != "test" ]]; then
  exec "${MUTATION_GO}" "$@"
fi

stdout="$(mktemp)" stderr="$(mktemp)"
trap 'rm -f "${stdout}" "${stderr}"' EXIT

"${MUTATION_GO}" "$@" > "${stdout}" 2> "${stderr}"
code=$?

cat "${stdout}"
cat "${stderr}" >&2

if [[ ${code} -eq 1 ]] && grep -qE '\[(build|setup) failed\]' "${stdout}" "${stderr}"; then
  exit 2
fi

exit "${code}"
