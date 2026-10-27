package docsgen

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/nightah/encore/internal/config"
)

// ConfigSections walks config.Config and returns its keys grouped by top-level key, in declaration order. dir is package config's source directory, read for doc comments. Every key must have a doc comment.
func ConfigSections(dir string) ([]ConfigSection, error) {
	docs, err := parseDocs(dir)
	if err != nil {
		return nil, err
	}

	w := &walker{docs: docs, aliases: map[string][]string{}}
	for name, key := range config.EnvAliases() {
		w.aliases[key] = append(w.aliases[key], config.EnvPrefix+name)
	}

	root := reflect.TypeFor[config.Config]()
	defaults := reflect.ValueOf(config.Defaults())

	var sections []ConfigSection

	for i := range root.NumField() {
		f := root.Field(i)

		tag := f.Tag.Get("koanf")
		if tag == "" {
			continue
		}

		ft := f.Type

		switch {
		case ft.Kind() == reflect.Struct:
			sec := ConfigSection{Key: tag, Doc: w.typeDoc(ft, tag)}
			w.walk(&sec, ft, defaults.Field(i), tag, false)
			sections = append(sections, sec)
		case ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.Struct:
			sec := ConfigSection{Key: tag, Doc: w.fieldDoc(root, f.Name, tag) + " " + w.typeDoc(ft.Elem(), tag+"[]")}
			w.walk(&sec, ft.Elem(), reflect.Value{}, tag+"[]", true)
			sections = append(sections, sec)
		default:
			sections = append(sections, ConfigSection{Key: tag, Keys: []ConfigKey{w.key(tag, f, defaults.Field(i), w.fieldDoc(root, f.Name, tag), false)}})
		}
	}

	return sections, errors.Join(w.errs...)
}

// ConfigReference renders the configuration reference page.
func ConfigReference(dir string) ([]byte, error) {
	sections, err := ConfigSections(dir)
	if err != nil {
		return nil, err
	}

	return render("configuration.md.tmpl", map[string]any{"Sections": sections, "Prefix": config.EnvPrefix, "File": config.DefaultFileName})
}

// v holds the defaults, or is invalid inside a list, where there are none.
func (w *walker) walk(sec *ConfigSection, t reflect.Type, v reflect.Value, prefix string, inList bool) {
	for i := range t.NumField() {
		f := t.Field(i)

		tag := f.Tag.Get("koanf")
		if tag == "" {
			continue
		}

		key := prefix + "." + tag

		var fv reflect.Value
		if v.IsValid() {
			fv = v.Field(i)
		}

		if f.Type.Kind() == reflect.Struct {
			w.walk(sec, f.Type, fv, key, inList)

			continue
		}

		doc := w.fieldDoc(t, f.Name, key)
		sec.Keys = append(sec.Keys, w.key(key, f, fv, doc, inList))
	}
}

func (w *walker) key(key string, f reflect.StructField, v reflect.Value, doc string, inList bool) ConfigKey {
	k := ConfigKey{Key: key, Type: typeName(f.Type), Doc: doc}

	if v.IsValid() {
		k.Default = formatDefault(v)
	}

	// The data directory's default depends on the user and platform, so describe it instead of showing this machine's.
	if key == "data_dir" {
		k.Default, k.DefaultDoc = "", config.DataDirDefaultDoc
	}

	// A variable ending in _FILE reads the key without the suffix from a file, so a key whose name ends in _file has no variable of its own.
	if (!inList && f.Type.Kind() != reflect.Slice) || config.IsEnvList(key) {
		if !strings.HasSuffix(key, "_file") {
			k.Env = config.EnvPrefix + strings.ToUpper(config.EnvName(key))
		}
	}

	k.EnvAliases = w.aliases[key]

	return k
}

func (w *walker) typeDoc(t reflect.Type, key string) string {
	d := w.docs.types[t.Name()]
	if d == "" {
		w.errs = append(w.errs, fmt.Errorf("config.%s (%s) has no doc comment", t.Name(), key))

		return ""
	}

	return w.rewrite(d, t.Name(), key, key+".", tags(t))
}

func (w *walker) fieldDoc(t reflect.Type, field, key string) string {
	typ := t.Name()
	d := w.docs.fields[typ][field]
	if d == "" {
		w.errs = append(w.errs, fmt.Errorf("config.%s.%s (%s) has no doc comment", typ, field, key))

		return ""
	}

	return w.rewrite(d, field, key, key[:strings.LastIndex(key, ".")+1], tags(t))
}

var (
	// goOnly matches asides meant for Go readers: a pointer to a function or a constant's name.
	goOnly = regexp.MustCompile(`; see [A-Z]\w+| \([A-Z][a-z]+(?:[A-Z][a-z]*)+\)`)
	// camel matches multi-word Go identifiers such as GroupsClaim or SSEKMSKeyID.
	camel = regexp.MustCompile(`\b[A-Z][A-Za-z]*[a-z][A-Z][A-Za-z]*\b|\b[A-Z]{2,}[a-z]+[A-Za-z]*\b`)
)

// rewrite turns a Go doc comment into reference prose: the leading identifier becomes the key, the names of sibling fields (siblings maps them to their keys) become parent plus their keys, and Go-only asides are dropped.
func (w *walker) rewrite(doc, name, key, parent string, siblings map[string]string) string {
	doc = goOnly.ReplaceAllString(doc, "")

	if rest, ok := strings.CutPrefix(doc, name+" "); ok {
		doc = "`" + key + "` " + rest
	} else if rest, ok := strings.CutPrefix(doc, name+","); ok {
		doc = "`" + key + "`," + rest
	}

	return camel.ReplaceAllStringFunc(doc, func(id string) string {
		tag, ok := siblings[id]
		if !ok {
			return id
		}

		return "`" + parent + tag + "`"
	})
}

func tags(t reflect.Type) map[string]string {
	out := map[string]string{}

	for i := range t.NumField() {
		if tag := t.Field(i).Tag.Get("koanf"); tag != "" {
			out[t.Field(i).Name] = tag
		}
	}

	return out
}

func typeName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Pointer:
		return typeName(t.Elem())
	case reflect.Slice:
		return "list of " + typeName(t.Elem()) + "s"
	case reflect.Int:
		return "integer"
	default:
		return t.Kind().String()
	}
}

func formatDefault(v reflect.Value) string {
	switch v.Kind() {
	case reflect.String:
		return strconv.Quote(v.String())
	case reflect.Slice:
		items := make([]string, v.Len())
		for i := range v.Len() {
			items[i] = v.Index(i).String()
		}

		return "[" + strings.Join(items, ", ") + "]"
	case reflect.Pointer:
		if v.IsNil() {
			return "unset"
		}

		return formatDefault(v.Elem())
	default:
		return fmt.Sprint(v.Interface())
	}
}

func parseDocs(dir string) (goDocs, error) {
	docs := goDocs{types: map[string]string{}, fields: map[string]map[string]string{}}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return docs, err
	}

	fset := token.NewFileSet()

	var files []*ast.File

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}

		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.ParseComments)
		if err != nil {
			return docs, err
		}

		files = append(files, f)
	}

	if len(files) == 0 {
		return docs, fmt.Errorf("no Go files in %s", dir)
	}

	for _, file := range files {
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}

			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}

				doc := ts.Doc
				if doc == nil && len(gd.Specs) == 1 {
					doc = gd.Doc
				}

				docs.types[ts.Name.Name] = commentText(doc)

				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}

				fields := map[string]string{}

				for _, f := range st.Fields.List {
					text := commentText(f.Doc)
					if text == "" {
						text = commentText(f.Comment)
					}

					for _, n := range f.Names {
						fields[n.Name] = text
					}
				}

				docs.fields[ts.Name.Name] = fields
			}
		}
	}

	return docs, nil
}

func commentText(g *ast.CommentGroup) string {
	if g == nil {
		return ""
	}

	return strings.Join(strings.Fields(g.Text()), " ")
}
