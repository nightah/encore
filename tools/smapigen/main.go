package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	specPath := flag.String("spec", "smapigen/spec/spec.json", "pinned Swagger 2.0 spec")
	sumPath := flag.String("sum", "smapigen/spec/spec.json.sha256", "sha256sum-format checksum file for the spec")
	outDir := flag.String("out", "", "output directory")
	pkg := flag.String("package", "smapiclient", "Go package name")
	flag.Parse()

	if *outDir == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*specPath, *sumPath, *outDir, *pkg); err != nil {
		fmt.Fprintln(os.Stderr, "smapigen:", err)
		os.Exit(1)
	}
}

func run(specPath, sumPath, outDir, pkg string) error {
	raw, err := os.ReadFile(specPath) //nolint:gosec // G304: path comes from the -spec flag of a dev tool.
	if err != nil {
		return err
	}
	if err := verifyChecksum(raw, sumPath); err != nil {
		return err
	}
	src, err := generate(raw, pkg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return err
	}
	//nolint:gosec // G306: the generated Go source is checked in and must be world-readable.
	return os.WriteFile(filepath.Join(outDir, outputFile), src, 0o644)
}
