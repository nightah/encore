#!/usr/bin/env bash
# Tests go-coverage.sh against small profiles and its own ignore list: run it from anywhere.
set -uo pipefail

script="$(dirname "${BASH_SOURCE[0]}")/go-coverage.sh"
module="$(awk '$1 == "module" { print $2; exit }' "$(dirname "${BASH_SOURCE[0]}")/../../go.mod")"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
failed=0

cat > "${work}/codecov.yml" << 'EOF'
coverage:
  status:
    project:
      default:
        target: auto
ignore:
  - "**/*.gen.go"
  - "internal/fake/"
  - 'cmd/tool/main.go'
comment:
  layout: "diff"
EOF
export CODECOV_YML="${work}/codecov.yml"

# profile <name> <lines...>: a cover profile whose lines are "<file relative to the module>:<block> <statements> <count>".
profile() {
  local name="$1" line
  shift
  {
    echo "mode: atomic"
    for line in "$@"; do echo "${module}/${line}"; done
  } > "${work}/${name}"
}

# expect <name> <minimum or ""> <exit code> <output line>
expect() {
  local name="$1" minimum="$2" want_code="$3" want="$4" got code
  if [[ -n "${minimum}" ]]; then
    got="$("${script}" "${work}/${name}" "${minimum}" 2>&1)"
  else
    got="$("${script}" "${work}/${name}" 2>&1)"
  fi
  code=$?
  if [[ "${code}" -ne "${want_code}" || "${got}" != *"${want}"* ]]; then
    echo "FAIL: ${name} ${minimum:-(no minimum)} exited ${code} with '${got}', want ${want_code} with '${want}'"
    failed=1
  fi
}

profile plain "internal/a/a.go:1.1,2.2 3 1" "internal/a/a.go:3.1,4.2 1 0"
expect plain "" 0 "total: 75.0% of statements (3 of 4)"
expect plain 75 0 "total: 75.0%"
expect plain 75.1 1 "below the minimum of 75.1%"

# Generated code, an ignored directory and an ignored file don't count; a similar name outside them still does.
profile ignores \
  "internal/a/a.go:1.1,2.2 1 1" \
  "internal/a/client.gen.go:1.1,2.2 5 0" \
  "internal/fake/fake.go:1.1,2.2 5 0" \
  "internal/fake/sub/deep.go:1.1,2.2 5 0" \
  "cmd/tool/main.go:1.1,2.2 5 0" \
  "internal/fakes/real.go:1.1,2.2 1 0" \
  "cmd/tool/run.go:1.1,2.2 2 1"
expect ignores "" 0 "total: 75.0% of statements (3 of 4)"

# With -coverpkg each test binary reports every block; a block counts once, covered if any binary covered it.
profile repeated \
  "internal/a/a.go:1.1,2.2 2 0" \
  "internal/a/a.go:1.1,2.2 2 4" \
  "internal/a/a.go:1.1,2.2 2 0" \
  "internal/a/a.go:3.1,4.2 2 0" \
  "internal/a/a.go:3.1,4.2 2 0"
expect repeated "" 0 "total: 50.0% of statements (2 of 4)"

# 949 of 1000 is 94.9%, not 95%, and fails a 95% minimum; the figure is truncated, never rounded up.
profile truncated "internal/a/a.go:1.1,2.2 9499 1" "internal/a/a.go:3.1,4.2 501 0"
expect truncated "" 0 "total: 94.9% of statements (9499 of 10000)"
expect truncated 95 1 "coverage 94.9% is below the minimum of 95%"
expect truncated 94.99 0 "total: 94.9%"

profile everything-ignored "internal/fake/fake.go:1.1,2.2 5 1"
expect everything-ignored "" 1 "no statements left to measure"

profile empty
expect empty "" 1 "no statements left to measure"

expect plain abc 2 "minimum 'abc' is not a percentage"
expect plain -5 2 "is not a percentage"
expect missing "" 1 "cannot read profile"

if got="$("${script}" 2>&1)"; then
  echo "FAIL: no arguments succeeded with '${got}'"
  failed=1
fi

if [[ ${failed} -eq 0 ]]; then
  echo "ok"
fi

exit "${failed}"
