package layout

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"
)

// skipDirs are trees with no hand-written Go of ours.
var skipDirs = []string{".git", "node_modules", "web", "docs", "dist", "bin", "testdata", ".scratch"}

// TestLayout checks every hand-written, non-test Go file in the root, tools and benchmarks modules.
func TestLayout(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	packages := map[string][]string{}

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if path != root && (slices.Contains(skipDirs, d.Name()) || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}

			return nil
		}

		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			packages[filepath.Dir(path)] = append(packages[filepath.Dir(path)], path)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()

	for dir, files := range packages {
		rel, _ := filepath.Rel(root, dir)
		hasDoc := false

		for _, path := range files {
			f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}

			if ast.IsGenerated(f) {
				continue
			}

			name := filepath.Base(path)

			if f.Doc != nil {
				if name == "doc.go" {
					hasDoc = true
				} else {
					t.Errorf("%s: the package comment belongs in doc.go", filepath.Join(rel, name))
				}
			}

			for _, decl := range f.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok {
					continue
				}

				for _, spec := range gen.Specs {
					if want, what := home(gen.Tok, spec); want != "" && want != name {
						t.Errorf("%s:%d: %s belongs in %s", filepath.Join(rel, name), fset.Position(spec.Pos()).Line, what, want)
					}
				}
			}
		}

		if !hasDoc && !allGenerated(t, fset, files) {
			t.Errorf("%s: needs a doc.go with the package comment", rel)
		}
	}
}

// home returns the file a top-level declaration belongs in, and how to describe it, or "" when it may live anywhere.
func home(tok token.Token, spec ast.Spec) (file, what string) {
	switch tok {
	case token.CONST:
		return "consts.go", "const " + specName(spec)
	case token.TYPE:
		name := spec.(*ast.TypeSpec).Name.Name
		if strings.HasSuffix(name, "Error") {
			return "errors.go", "error type " + name
		}

		return "types.go", "type " + name
	case token.VAR:
		for _, n := range spec.(*ast.ValueSpec).Names {
			if isSentinel(n.Name) {
				return "errors.go", "error " + n.Name
			}
		}
	}

	return "", ""
}

// isSentinel reports whether name is an error variable, ErrX or errX.
func isSentinel(name string) bool {
	rest, ok := strings.CutPrefix(name, "Err")
	if !ok {
		rest, ok = strings.CutPrefix(name, "err")
	}

	return ok && rest != "" && unicode.IsUpper(rune(rest[0]))
}

func specName(spec ast.Spec) string {
	if v, ok := spec.(*ast.ValueSpec); ok && len(v.Names) > 0 {
		return v.Names[0].Name
	}

	return "?"
}

func allGenerated(t *testing.T, fset *token.FileSet, files []string) bool {
	t.Helper()

	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly|parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}

		if !ast.IsGenerated(f) {
			return false
		}
	}

	return true
}
