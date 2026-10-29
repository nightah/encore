#!/usr/bin/env bash
# Tests mutation-go.sh: run it from anywhere; needs go.
set -uo pipefail

script="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/mutation-go.sh"
failed=0

MUTATION_GO="$(command -v go)"
export MUTATION_GO

module="$(mktemp -d)"
trap 'rm -rf "${module}"' EXIT
printf 'module probe\n\ngo 1.21\n' > "${module}/go.mod"
printf 'package probe\n\nimport "testing"\n\nfunc TestF(t *testing.T) {\n\tif F() != 2 {\n\t\tt.Fatal("F() != 2")\n\t}\n}\n' > "${module}/probe_test.go"

# expect <name> <source of probe.go> <want exit code> <go args...>
expect() {
  local name="$1" source="$2" want="$3" got
  shift 3
  printf '%s' "${source}" > "${module}/probe.go"
  (cd "${module}" && "${script}" "$@" > /dev/null 2>&1)
  got=$?
  if [[ "${got}" != "${want}" ]]; then
    echo "FAIL: ${name} exited ${got}, want ${want}"
    failed=1
  fi
}

expect "tests pass" $'package probe\n\nfunc F() int { return 2 }\n' 0 test -count=1 .
expect "tests fail" $'package probe\n\nfunc F() int { return 3 }\n' 1 test -count=1 .

# A mutant that doesn't compile, or that go test's vet checks reject, is not viable rather than killed.
expect "compile error" $'package probe\n\nfunc F() int { x := 1; return 2 }\n' 2 test -count=1 .
expect "vet error" $'package probe\n\nimport "fmt"\n\nfunc F() int { fmt.Printf("%d\\n"); return 2 }\n' 2 test -count=1 .

# Anything but go test passes straight through, exit code included.
expect "go build ok" $'package probe\n\nfunc F() int { return 2 }\n' 0 build .
expect "go build error" $'package probe\n\nfunc F() int { x := 1; return 2 }\n' 1 build .

# go test's output reaches the caller unchanged.
printf 'package probe\n\nfunc F() int { return 3 }\n' > "${module}/probe.go"
output="$(cd "${module}" && "${script}" test -count=1 . 2>&1)"
if [[ "${output}" != *'F() != 2'* ]]; then
  echo "FAIL: go test output was not passed through"
  failed=1
fi

if [[ ${failed} -eq 0 ]]; then
  echo "ok"
fi

exit "${failed}"
