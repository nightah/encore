package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pinnedConfig reads kodigen's arguments from the go:generate line in kodirpc, relative to this package, so the tests check what go generate writes.
func pinnedConfig(t *testing.T) config {
	t.Helper()
	src, err := os.ReadFile("../../internal/backend/kodi/kodirpc/generate.go")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config{pkg: "kodirpc", releasesPkg: "kodi"}
	fields := strings.Fields(string(src))
	for i := 0; i+1 < len(fields); i++ {
		// The paths are relative to tools/, the directory go generate runs kodigen from.
		path := filepath.Join("..", strings.Trim(fields[i+1], `"`))
		switch fields[i] {
		case "-schemas":
			cfg.schemas = path
		case "-matrix":
			cfg.matrix = path
		case "-out":
			cfg.out = path
		case "-releases":
			cfg.releases = path
		}
	}
	if cfg.schemas == "" || cfg.matrix == "" || cfg.out == "" || cfg.releases == "" {
		t.Fatalf("generate.go lacks one of -schemas, -matrix, -out and -releases: %+v", cfg)
	}
	return cfg
}

// renderPinned renders every generated file from the checked-in schemas, without downloading or removing any.
func renderPinned(t *testing.T) (config, *rendered) {
	t.Helper()
	cfg := pinnedConfig(t)
	pins, err := readMatrix(cfg.matrix)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := loadChain(cfg.schemas)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := prunable(chain, pins)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stale {
		t.Errorf("schema %s adds nothing to the one before it and no image pins it; run go generate ./internal/backend/kodi/kodirpc", s.Tag)
	}
	out, err := renderAll(chain, pins, cfg)
	if err != nil {
		t.Fatalf("%v (run go generate ./internal/backend/kodi/kodirpc)", err)
	}
	return cfg, out
}

// TestPinnedOutputUpToDate fails when the checked-in generated files, SOURCES.md and the contract tests' releases do not match what the generator produces from the pinned schemas.
func TestPinnedOutputUpToDate(t *testing.T) {
	cfg, out := renderPinned(t)
	for name, want := range out.pkg {
		got, err := os.ReadFile(filepath.Join(cfg.out, name))
		if err != nil {
			t.Fatalf("%s: %v (run go generate ./internal/backend/kodi/kodirpc)", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is stale; run go generate ./internal/backend/kodi/kodirpc", name)
		}
	}
	stale, _ := filepath.Glob(filepath.Join(cfg.out, "*.gen.go"))
	for _, path := range stale {
		if _, ok := out.pkg[filepath.Base(path)]; !ok {
			t.Errorf("%s is not produced by the generator", path)
		}
	}
	for path, want := range map[string][]byte{filepath.Join(cfg.schemas, "SOURCES.md"): out.sources, cfg.releases: out.releases} {
		if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s is stale; run go generate ./internal/backend/kodi/kodirpc", path)
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
	cfg, out := renderPinned(t)
	for name, data := range out.pkg {
		write(name, data)
	}

	handWritten, err := filepath.Glob(filepath.Join(cfg.out, "*.go"))
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
