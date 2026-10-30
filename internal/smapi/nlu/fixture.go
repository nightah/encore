package nlu

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/nightah/encore/internal/skillmodel"
)

// Load reads every <locale>.yaml in fsys, sorted by locale.
func Load(fsys fs.FS) ([]Fixture, error) {
	names, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return nil, err
	}

	out := make([]Fixture, 0, len(names))

	for _, name := range names {
		f, err := LoadFile(fsys, name)
		if err != nil {
			return nil, err
		}

		out = append(out, f)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Locale < out[j].Locale })

	return out, nil
}

// LoadFile reads one fixture file, which must be named after its locale.
func LoadFile(fsys fs.FS, name string) (Fixture, error) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return Fixture{}, err
	}

	var f Fixture

	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)

	if err := dec.Decode(&f); err != nil {
		return Fixture{}, fmt.Errorf("nlu: %s: %w", name, err)
	}

	if want := strings.TrimSuffix(path.Base(name), ".yaml"); f.Locale != want {
		return Fixture{}, fmt.Errorf("nlu: %s: locale is %q, but the file is named for %q", name, f.Locale, want)
	}

	return f, nil
}

// Check reports rows that can't match the skill src builds: an unknown locale or intent, a slot the intent doesn't have, an empty or repeated utterance.
func (f Fixture) Check(src *skillmodel.Sources) error {
	var errs []error

	if _, ok := src.Locales[f.Locale]; !ok {
		errs = append(errs, fmt.Errorf("%s: no such locale in the sources", f.Locale))
	}

	seen := map[string]bool{}

	for i, c := range f.Cases {
		where := fmt.Sprintf("%s row %d (%q)", f.Locale, i+1, c.Utterance)

		key := strings.ToLower(strings.TrimSpace(c.Utterance))
		if key == "" {
			errs = append(errs, fmt.Errorf("%s: no utterance", where))
		} else if seen[key] {
			errs = append(errs, fmt.Errorf("%s: utterance repeated", where))
		}

		seen[key] = true

		in, ok := src.Model.Intent(c.Intent)
		if !ok {
			errs = append(errs, fmt.Errorf("%s: no intent %q in the model", where, c.Intent))

			continue
		}

		for _, name := range sortedKeys(c.Slots) {
			if !slices.ContainsFunc(in.Slots, func(s skillmodel.Slot) bool { return s.Name == name }) {
				errs = append(errs, fmt.Errorf("%s: %s has no slot %s", where, c.Intent, name))
			}
		}
	}

	return errors.Join(errs...)
}

// Missing lists the model's intents no row expects, in model order.
func (f Fixture) Missing(src *skillmodel.Sources) []string {
	var out []string

	for _, in := range src.Model.Intents {
		if !slices.ContainsFunc(f.Cases, func(c Case) bool { return c.Intent == in.Name }) {
			out = append(out, in.Name)
		}
	}

	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}
