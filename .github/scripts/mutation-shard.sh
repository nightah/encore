#!/usr/bin/env bash
# Prints the gremlins arguments that limit a mutation run of internal/core to one shard, one argument per line, so the nightly job can split a run that takes longer than a hosted runner lasts.
#
# Usage: mutation-shard.sh <shard>
#
# A named shard mutates only its files. The rest shard mutates every other file, so a new file is always tested even before it is given a shard. Paths are relative to internal/core, as gremlins matches them, and the shards are sized by gremlins' runnable mutant count (gremlins unleash --dry-run ./internal/core).
set -euo pipefail

root="internal/core"

declare -A shards=(
  [match-1]="match/similarity.go match/inverted.go match/alias.go match/lang_ar.go match/trigram.go match/config.go match/lang_hi.go"
  [match-2]="match/numbers.go match/normalize.go match/fuzz.go match/phonetic.go match/external.go match/retriever.go match/match.go"
  [match-3]="match/lang_ja.go match/index.go match/score.go match/lang.go match/lang_nl.go"
)

shard="${1:-}"

# exclude prints -E and an anchored regexp matching any of its arguments.
exclude() {
  [[ $# -gt 0 ]] || return 0

  local alternatives=()

  for f in "$@"; do
    alternatives+=("${f//./\\.}")
  done

  local IFS='|'
  printf -- '-E\n^(%s)$\n' "${alternatives[*]}"
}

if [[ "${shard}" == "rest" ]]; then
  listed=()

  for s in "${!shards[@]}"; do
    read -ra files <<< "${shards[${s}]}"
    listed+=("${files[@]}")
  done

  exclude "${listed[@]}"

  exit 0
fi

if [[ -z "${shards[${shard}]+set}" ]]; then
  echo "unknown shard '${shard}'; want rest or one of: ${!shards[*]}" >&2
  exit 2
fi

read -ra mine <<< "${shards[${shard}]}"

for f in "${mine[@]}"; do
  if [[ ! -f "${root}/${f}" ]]; then
    echo "shard ${shard} lists ${f}, which isn't in ${root}" >&2
    exit 2
  fi
done

others=()

while IFS= read -r f; do
  f="${f#"${root}/"}"

  for m in "${mine[@]}"; do
    [[ "${f}" == "${m}" ]] && continue 2
  done

  others+=("${f}")
done < <(find "${root}" -name '*.go' ! -name '*_test.go' | sort)

exclude "${others[@]}"
