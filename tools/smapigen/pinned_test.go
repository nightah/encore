package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"testing"

	// The generated client imports the oapi-codegen runtime. Requiring it here pins its version in tools/go.mod (the version the main module must use) and keeps it in the module cache for the compile test.
	_ "github.com/oapi-codegen/runtime"
)

const (
	pinnedSpec = "spec/spec.json"
	pinnedSum  = "spec/spec.json.sha256"
	outputDir  = "../../internal/smapi/smapiclient"
)

func generatePinned(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(pinnedSpec)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyChecksum(raw, pinnedSum); err != nil {
		t.Fatal(err)
	}
	src, err := generate(raw, "smapiclient")
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// TestPinnedOutputUpToDate fails when the checked-in client does not match what the generator produces from the pinned spec.
func TestPinnedOutputUpToDate(t *testing.T) {
	want := generatePinned(t)
	got, err := os.ReadFile(filepath.Join(outputDir, outputFile))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s is stale; run go generate ./internal/smapi/smapiclient", outputFile)
	}
}

// TestPinnedOutputCompiles builds and vets the generated client in a scratch module that requires the same runtime version as this module.
func TestPinnedOutputCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("invokes the go tool")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not found")
	}
	runtimeVersion := depVersion(t, "github.com/oapi-codegen/runtime")

	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "smapiclient")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	gomod := fmt.Sprintf("module example.com/smapicheck\n\ngo 1.27\n\nrequire github.com/oapi-codegen/runtime %s\n", runtimeVersion)
	sum, err := os.ReadFile("../go.sum")
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		filepath.Join(dir, "go.mod"):      []byte(gomod),
		filepath.Join(dir, "go.sum"):      sum,
		filepath.Join(pkgDir, outputFile): generatePinned(t),
	} {
		if err := os.WriteFile(name, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
		cmd := exec.Command(goTool, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %v: %v\n%s", args, err, out)
		}
	}
}

func depVersion(t *testing.T, path string) string {
	t.Helper()
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		t.Fatal("no build info")
	}
	for _, dep := range bi.Deps {
		if dep.Path == path {
			return dep.Version
		}
	}
	t.Fatalf("%s is not a dependency of the test binary", path)
	return ""
}
