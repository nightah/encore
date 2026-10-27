package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const outputDir = "../../internal/backend/kodi/kodirpc"

func generatePinned(t *testing.T) map[string][]byte {
	t.Helper()
	spec, err := loadSpec("schema/piers")
	if err != nil {
		t.Fatal(err)
	}
	compat, err := loadSpec("schema/omega")
	if err != nil {
		t.Fatal(err)
	}
	files, err := generate(spec, compat, "kodirpc")
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestPinnedOutputUpToDate fails when the checked-in generated files do not match what the generator produces from the pinned schema.
func TestPinnedOutputUpToDate(t *testing.T) {
	files := generatePinned(t)
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(outputDir, name))
		if err != nil {
			t.Fatalf("%s: %v (run go generate ./internal/backend/kodi/kodirpc)", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is stale; run go generate ./internal/backend/kodi/kodirpc", name)
		}
	}
	stale, _ := filepath.Glob(filepath.Join(outputDir, "*.gen.go"))
	for _, path := range stale {
		if _, ok := files[filepath.Base(path)]; !ok {
			t.Errorf("%s is not produced by the generator", path)
		}
	}
}

// TestPinnedOutputCompiles builds and vets the generated code together with the hand-written kodirpc files in a scratch module.
func TestPinnedOutputCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("invokes the go tool")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not found")
	}

	dir := t.TempDir()
	write := func(name string, data []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte("module example.com/kodirpccheck\n\ngo 1.27\n"))
	for name, data := range generatePinned(t) {
		write(name, data)
	}

	handWritten, err := filepath.Glob(filepath.Join(outputDir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range handWritten {
		base := filepath.Base(path)
		if strings.HasSuffix(base, ".gen.go") || strings.HasSuffix(base, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		write(base, data)
	}

	for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
		cmd := exec.Command(goTool, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}
