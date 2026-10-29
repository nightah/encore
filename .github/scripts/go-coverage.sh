#!/usr/bin/env bash
# Prints the statement coverage of a Go cover profile, leaving out the paths .codecov.yml ignores, so CI's figure matches Codecov's. With a minimum percentage it also fails when coverage is below it.
#
# Usage: go-coverage.sh <profile> [minimum]. The ignore list is read from .codecov.yml next to go.mod (CODECOV_YML overrides it); a pattern ending in / ignores a directory, any other is a glob in which * also matches /.
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 <profile> [minimum]" >&2
  exit 2
fi

profile="$1" minimum="${2:-}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
codecov="${CODECOV_YML:-${root}/.codecov.yml}"

if [[ -n "${minimum}" && ! "${minimum}" =~ ^[0-9]+(\.[0-9]+)?$ ]]; then
  echo "minimum '${minimum}' is not a percentage" >&2
  exit 2
fi
if [[ ! -r "${profile}" ]]; then
  echo "cannot read profile ${profile}" >&2
  exit 1
fi

module="$(awk '$1 == "module" { print $2; exit }' "${root}/go.mod")"

# The items of the top-level ignore: list, unquoted.
mapfile -t ignores < <(awk '
  /^ignore:/ { in_list = 1; next }
  in_list && /^[^ #]/ { in_list = 0 }
  in_list && /^ *- / { sub(/^ *- */, ""); gsub(/^["\047]|["\047]$/, ""); print }
' "${codecov}")

ignored() {
  local path="$1" pattern
  for pattern in "${ignores[@]}"; do
    if [[ "${pattern}" == */ ]]; then
      [[ "${path}" == "${pattern}"* ]] && return 0
    else
      # shellcheck disable=SC2053 # the pattern is a glob on purpose
      [[ "${path}" == ${pattern} ]] && return 0
    fi
  done
  return 1
}

# The profile's files that count, each checked once rather than per block.
kept=()
while IFS= read -r file; do
  ignored "${file#"${module}/"}" || kept+=("${file}")
done < <(awk -F: 'NR > 1 && NF > 1 { print $1 }' "${profile}" | sort -u)

# A block appears once per test binary built with it (-coverpkg); it counts once, as covered if any run covered it.
read -r hit all < <(awk '
  FNR == NR { keep[$0] = 1; next }
  FNR == 1 { next }
  {
    split($1, parts, ":")
    if (!(parts[1] in keep)) next
    if (!($1 in stmts)) stmts[$1] = $2
    if ($3 > 0) covered[$1] = 1
  }
  END {
    for (block in stmts) { all += stmts[block]; if (block in covered) hit += stmts[block] }
    print hit + 0, all + 0
  }
' <(printf '%s\n' "${kept[@]+"${kept[@]}"}") "${profile}")

if [[ "${all}" -eq 0 ]]; then
  echo "no statements left to measure in ${profile}" >&2
  exit 1
fi

# Truncated rather than rounded, so 94.96% doesn't pass a 95% minimum by its printed value.
total="$(awk -v hit="${hit}" -v all="${all}" 'BEGIN { printf "%.1f", int(1000 * hit / all) / 10 }')"
echo "total: ${total}% of statements (${hit} of ${all})"

if [[ -n "${minimum}" ]] && awk -v hit="${hit}" -v all="${all}" -v minimum="${minimum}" 'BEGIN { exit !(100 * hit < minimum * all) }'; then
  echo "coverage ${total}% is below the minimum of ${minimum}%" >&2
  exit 1
fi
