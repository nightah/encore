package main

import (
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	schemaDir := flag.String("schema", "", "directory with the newest pinned schema (methods.json, types.json, notifications.json, version.txt)")
	compatDir := flag.String("compat", "", "optional directory with an older schema used for availability data")
	outDir := flag.String("out", "", "output directory")
	pkg := flag.String("package", "kodirpc", "Go package name")
	flag.Parse()

	if *schemaDir == "" || *outDir == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*schemaDir, *compatDir, *outDir, *pkg); err != nil {
		fmt.Fprintln(os.Stderr, "kodigen:", err)
		os.Exit(1)
	}
}

func run(schemaDir, compatDir, outDir, pkg string) error {
	spec, err := loadSpec(schemaDir)
	if err != nil {
		return err
	}
	var compat *Spec
	if compatDir != "" {
		if compat, err = loadSpec(compatDir); err != nil {
			return err
		}
	}

	files, err := generate(spec, compat, pkg)
	if err != nil {
		return err
	}
	return writeFiles(outDir, files)
}

func generate(spec, compat *Spec, pkg string) (map[string][]byte, error) {
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
