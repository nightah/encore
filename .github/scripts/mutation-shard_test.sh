#!/usr/bin/env bash
# Tests mutation-shard.sh: run it from anywhere; checks the shards against the repository's internal/core.
set -uo pipefail

dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="${dir}/mutation-shard.sh"
repo="$(cd "${dir}/../.." && pwd)"
shards=(match-1 match-2 match-3 rest)
failed=0

cd "${repo}" || exit 1

declare -A excluded

for s in "${shards[@]}"; do
  if ! args="$("${script}" "${s}")"; then
    echo "FAIL: shard ${s} exited non-zero"
    failed=1
    continue
  fi

  mapfile -t lines <<< "${args}"
  if [[ ${#lines[@]} -ne 2 || "${lines[0]}" != "-E" ]]; then
    echo "FAIL: shard ${s} printed ${args@Q}, want -E and one regexp"
    failed=1
    continue
  fi

  excluded[${s}]="${lines[1]}"
done

# Every file gremlins could mutate is in exactly one shard, so the shards together test what one run of internal/core would.
while IFS= read -r f; do
  f="${f#internal/core/}"
  in=()

  for s in "${shards[@]}"; do
    if ! grep -qE -- "${excluded[${s}]}" <<< "${f}"; then
      in+=("${s}")
    fi
  done

  if [[ ${#in[@]} -ne 1 ]]; then
    echo "FAIL: ${f} is in ${#in[@]} shards (${in[*]}), want 1"
    failed=1
  fi
done < <(find internal/core -name '*.go' ! -name '*_test.go' | sort)

# A shard that names a file which has gone fails rather than quietly testing less.
empty="$(mktemp -d)"
trap 'rm -rf "${empty}"' EXIT
if (cd "${empty}" && "${script}" match-1 > /dev/null 2>&1); then
  echo "FAIL: a shard listing missing files succeeded"
  failed=1
fi

if "${script}" bogus > /dev/null 2>&1; then
  echo "FAIL: an unknown shard succeeded"
  failed=1
fi

if [[ ${failed} -eq 0 ]]; then
  echo "ok"
fi

exit "${failed}"
