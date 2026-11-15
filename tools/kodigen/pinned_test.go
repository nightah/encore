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

// pinnedArgs reads the -schema and -compat directories from the go:generate line in kodirpc, so the tests generate from the same chain as go generate.
func pinnedArgs(t *testing.T) (schema string, compat []string) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(outputDir, "generate.go"))
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(src))
	for i := 0; i+1 < len(fields); i++ {
		dir := strings.TrimPrefix(strings.Trim(fields[i+1], `"`), "kodigen/")
		switch fields[i] {
		case "-schema":
			schema = dir
		case "-compat":
			compat = append(compat, dir)
		}
	}
	if schema == "" {
		t.Fatal("generate.go has no -schema argument")
	}
	return schema, compat
}

func generatePinned(t *testing.T) map[string][]byte {
	t.Helper()
	schemaDir, compatDirs := pinnedArgs(t)
	spec, err := loadSpec(schemaDir)
	if err != nil {
		t.Fatal(err)
	}
	compat := make([]*Spec, len(compatDirs))
	for i, dir := range compatDirs {
		if compat[i], err = loadSpec(dir); err != nil {
			t.Fatal(err)
		}
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
