package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func main() {
	schemasDir := flag.String("schemas", "", "directory holding one directory per pinned schema (methods.json, types.json, notifications.json, version.txt, commit.txt), named after its Kodi tag")
	matrix := flag.String("matrix", "", "workflow whose job matrix pins the Kodi images the contract tests run")
	outDir := flag.String("out", "", "output directory")
	releases := flag.String("releases", "", "file to write the contract tests' release to API version map to")
	pkg := flag.String("package", "kodirpc", "Go package name")
	releasesPkg := flag.String("releases-package", "kodi", "Go package name of the -releases file")
	flag.Parse()

	if *schemasDir == "" || *matrix == "" || *outDir == "" || *releases == "" {
		flag.Usage()
		os.Exit(2)
	}
	cfg := config{schemas: *schemasDir, matrix: *matrix, out: *outDir, releases: *releases, pkg: *pkg, releasesPkg: *releasesPkg}
	if err := run(context.Background(), cfg); err != nil {
		fmt.Fprintln(os.Stderr, "kodigen:", err)
		os.Exit(1)
	}
}

// run downloads the schema of every Kodi release the matrix pins that has no directory yet, removes schemas that add nothing, and writes the package, SOURCES.md and the contract tests' releases.
func run(ctx context.Context, cfg config) error {
	pins, err := readMatrix(cfg.matrix)
	if err != nil {
		return err
	}
	for _, p := range pins {
		dir := filepath.Join(cfg.schemas, p.kodiTag)
		if _, err := os.Stat(dir); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := fetchSchema(ctx, p.kodiTag, dir); err != nil {
			return err
		}
	}

	chain, err := loadChain(cfg.schemas)
	if err != nil {
		return err
	}
	stale, err := prunable(chain, pins)
	if err != nil {
		return err
	}
	for _, s := range stale {
		if err := os.RemoveAll(s.Path); err != nil {
			return err
		}
	}
	chain = slices.DeleteFunc(chain, func(s *Spec) bool { return slices.Contains(stale, s) })

	out, err := renderAll(chain, pins, cfg)
	if err != nil {
		return err
	}
	if err := writeFiles(cfg.out, out.pkg); err != nil {
		return err
	}
	//nolint:gosec // G306: generated files are checked in and must be world-readable.
	if err := os.WriteFile(filepath.Join(cfg.schemas, "SOURCES.md"), out.sources, 0o644); err != nil {
		return err
	}
	//nolint:gosec // G306: as above.
	return os.WriteFile(cfg.releases, out.releases, 0o644)
}

// matrixName is the matrix workflow's path from the repository root, as SOURCES.md names it.
func (c config) matrixName() string {
	if i := strings.Index(c.matrix, ".github/"); i >= 0 {
		return c.matrix[i:]
	}
	return c.matrix
}

// renderAll produces every generated file from the pinned schemas, the newest of which the package is generated from.
func renderAll(chain []*Spec, pins []pin, cfg config) (*rendered, error) {
	if err := checkPinned(chain, pins); err != nil {
		return nil, err
	}
	last := len(chain) - 1
	pkgFiles, err := generate(chain[last], chain[:last], cfg.pkg)
	if err != nil {
		return nil, err
	}
	sources, err := renderSources(chain, pins, cfg.matrixName())
	if err != nil {
		return nil, err
	}
	releases, err := renderReleases(chain, pins, cfg.releasesPkg)
	if err != nil {
		return nil, err
	}
	return &rendered{pkg: pkgFiles, sources: sources, releases: releases}, nil
}

// generate renders the package from spec, taking availability data from the compat schemas, which must be ordered oldest to newest and none be newer than spec.
func generate(spec *Spec, compat []*Spec, pkg string) (map[string][]byte, error) {
	for i, s := range compat {
		next := spec
		if i+1 < len(compat) {
			next = compat[i+1]
		}
		if compareVersions(s.Version, next.Version) > 0 {
			return nil, fmt.Errorf("compat schema %s is newer than %s: list compat schemas oldest first, none newer than the schema", s.Version, next.Version)
		}
	}

	g := newGenerator(spec)

	// Reserve every schema type name first so inline types never take a name that a schema id maps to.
	for _, id := range sortedKeys(spec.Types) {
		g.claim(typeName(id), id)
	}
	g.file = "types"
	for _, id := range sortedKeys(spec.Types) {
		g.ensureType(id)
	}
	g.genMethods()
	g.genNotifications()

	extra := map[string]string{
		"methods":       g.renderMethodTable(compat),
		"client":        g.renderClient(),
		"notifications": g.renderNotificationTable(),
	}
	if err := g.err(); err != nil {
		return nil, err
	}

	imports := map[string][]string{
		"client": {"context"},
	}

	files := map[string][]byte{}
	for _, file := range []string{"types", "methods", "client", "notifications"} {
		var body strings.Builder
		for _, name := range sortedKeys(g.decls) {
			if d := g.decls[name]; d.file == file {
				body.WriteString(d.code)
				body.WriteString("\n")
			}
		}
		body.WriteString(extra[file])

		src, err := render(pkg, imports[file], body.String())
		if err != nil {
			return nil, fmt.Errorf("%s.gen.go: %w", file, err)
		}
		files[file+".gen.go"] = src
	}
	return files, nil
}

func render(pkg string, imports []string, body string) ([]byte, error) {
	var b strings.Builder
	b.WriteString(header)
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	if len(imports) > 0 {
		b.WriteString("import (\n")
		for _, imp := range imports {
			fmt.Fprintf(&b, "\t%q\n", imp)
		}
		b.WriteString(")\n\n")
	}
	b.WriteString(body)

	src, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("gofmt: %w\n%s", err, b.String())
	}
	return src, nil
}

// writeFiles removes stale generated files and leaves hand-written ones alone.
func writeFiles(dir string, files map[string][]byte) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	existing, err := filepath.Glob(filepath.Join(dir, "*.gen.go"))
	if err != nil {
		return err
	}
	for _, path := range existing {
		if _, keep := files[filepath.Base(path)]; !keep {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}
	for _, name := range sortedKeys(files) {
		//nolint:gosec // G306: generated Go sources are checked in and must be world-readable.
		if err := os.WriteFile(filepath.Join(dir, name), files[name], 0o644); err != nil {
			return err
		}
	}
	return nil
}
