package skillmodel

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

	"github.com/nightah/encore/internal/core/media"
)

// IntentContent maps each intent that belongs to a content group to that group.
func (m *Model) IntentContent() map[string]media.Content {
	out := map[string]media.Content{}

	for _, in := range m.Intents {
		if in.Content != "" {
			out[in.Name] = in.Content
		}
	}

	return out
}

// Type returns the named custom slot type.
func (m Model) Type(name string) (SlotType, bool) {
	for _, t := range m.Types {
		if t.Name == name {
			return t, true
		}
	}

	return SlotType{}, false
}

// IsBuiltin reports whether name is an Amazon built-in intent or slot type.
func IsBuiltin(name string) bool {
	return strings.HasPrefix(name, "AMAZON.")
}

// missingBuiltins lists, per locale, the built-in slot types Amazon doesn't offer there. Every other built-in type the model uses exists in every Alexa locale.
var missingBuiltins = map[string][]string{
	"nl-NL": {"AMAZON.SearchQuery"},
}

// LacksBuiltin reports whether locale is one where Amazon doesn't offer the built-in slot type typ.
func LacksBuiltin(locale, typ string) bool {
	return slices.Contains(missingBuiltins[locale], typ)
}

// TypeIn returns the slot's type in locale: its Fallback where the locale lacks its built-in type, otherwise its Type.
func (s Slot) TypeIn(locale string) string {
	if s.Fallback != "" && LacksBuiltin(locale, s.Type) {
		return s.Fallback
	}

	return s.Type
}

// fallbackOnly returns the custom types that slots take only as their Fallback, which a locale needs only when one of its slots falls back to them.
func (m Model) fallbackOnly() map[string]bool {
	out := map[string]bool{}

	for _, in := range m.Intents {
		for _, s := range in.Slots {
			if s.Fallback != "" {
				out[s.Fallback] = true
			}
		}
	}

	for _, in := range m.Intents {
		for _, s := range in.Slots {
			delete(out, s.Type)
		}
	}

	return out
}

// Intent returns the named intent.
func (m Model) Intent(name string) (Intent, bool) {
	for _, in := range m.Intents {
		if in.Name == name {
			return in, true
		}
	}

	return Intent{}, false
}

// LocaleNames returns the configured locales in sorted order.
func (s *Sources) LocaleNames() []string {
	names := make([]string, 0, len(s.Locales))
	for name := range s.Locales {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// Utterances returns a locale's utterance patterns per intent: its language's patterns followed by the locale's additions.
func (s *Sources) Utterances(locale string) (map[string][]string, error) {
	loc, lang, err := s.resolve(locale)
	if err != nil {
		return nil, err
	}

	out := make(map[string][]string, len(lang.Utterances))
	for intent, patterns := range lang.Utterances {
		out[intent] = append([]string(nil), patterns...)
	}

	for intent, patterns := range loc.Utterances {
		out[intent] = append(out[intent], patterns...)
	}

	return out, nil
}

// Types returns a locale's static values for each custom slot type: the model's values followed by its language's.
func (s *Sources) Types(locale string) (map[string][]SlotValue, error) {
	_, lang, err := s.resolve(locale)
	if err != nil {
		return nil, err
	}

	out := make(map[string][]SlotValue, len(s.Model.Types))

	for _, t := range s.Model.Types {
		out[t.Name] = append(append([]SlotValue(nil), t.Values...), lang.Types[t.Name]...)
	}

	return out, nil
}

// Responses returns a locale's response strings: its language's, with the locale's overrides applied.
func (s *Sources) Responses(locale string) (map[string]string, error) {
	loc, lang, err := s.resolve(locale)
	if err != nil {
		return nil, err
	}

	out := make(map[string]string, len(lang.Responses))
	for id, text := range lang.Responses {
		out[id] = text
	}

	for id, text := range loc.Responses {
		out[id] = text
	}

	return out, nil
}

func (s *Sources) resolve(locale string) (Locale, Language, error) {
	loc, ok := s.Locales[locale]
	if !ok {
		return Locale{}, Language{}, fmt.Errorf("unknown locale %q", locale)
	}

	lang, ok := s.Languages[loc.Language]
	if !ok {
		return Locale{}, Language{}, fmt.Errorf("locale %s: unknown language %q", locale, loc.Language)
	}

	return loc, lang, nil
}

// Load reads the sources from fsys, whose root is the i18n directory.
func Load(fsys fs.FS) (*Sources, error) {
	src := &Sources{Languages: map[string]Language{}, Locales: map[string]Locale{}}

	if err := decodeFile(fsys, "model.yaml", &src.Model); err != nil {
		return nil, err
	}

	langDirs, err := fs.ReadDir(fsys, "lang")
	if err != nil {
		return nil, fmt.Errorf("reading lang: %w", err)
	}

	for _, d := range langDirs {
		if !d.IsDir() {
			continue
		}

		lang := Language{
			Utterances:    map[string][]string{},
			Types:         map[string][]SlotValue{},
			UtteranceKeys: map[string][]string{},
			TypeKeys:      map[string][]string{},
		}

		dir := path.Join("lang", d.Name())

		utterances, err := decodeKeyed(fsys, path.Join(dir, "utterances.yaml"))
		if err != nil {
			return nil, err
		}

		for _, sec := range utterances {
			for _, e := range sec.entries {
				lang.UtteranceKeys[sec.name] = append(lang.UtteranceKeys[sec.name], e.key)
				lang.Utterances[sec.name] = append(lang.Utterances[sec.name], e.lines...)
			}
		}

		if err := decodeFile(fsys, path.Join(dir, "responses.yaml"), &lang.Responses); err != nil {
			return nil, err
		}

		if _, err := fs.Stat(fsys, path.Join(dir, "types.yaml")); err == nil {
			types, err := decodeKeyed(fsys, path.Join(dir, "types.yaml"))
			if err != nil {
				return nil, err
			}

			for _, sec := range types {
				for _, e := range sec.entries {
					lang.TypeKeys[sec.name] = append(lang.TypeKeys[sec.name], e.key)
					lang.Types[sec.name] = append(lang.Types[sec.name], SlotValue{ID: e.key, Value: e.lines[0], Synonyms: e.lines[1:]})
				}
			}
		}

		if _, err := fs.Stat(fsys, path.Join(dir, "examples.yaml")); err == nil {
			examples := map[string][]SlotValue{}
			if err := decodeFile(fsys, path.Join(dir, "examples.yaml"), &examples); err != nil {
				return nil, err
			}

			lang.Examples = examples

			for typ, values := range examples {
				lang.Types[typ] = append(lang.Types[typ], values...)
			}
		}

		src.Languages[d.Name()] = lang
	}

	locales, err := fs.Glob(fsys, "locales/*.yaml")
	if err != nil {
		return nil, err
	}

	for _, name := range locales {
		var loc Locale
		if err := decodeFile(fsys, name, &loc); err != nil {
			return nil, err
		}

		src.Locales[strings.TrimSuffix(path.Base(name), ".yaml")] = loc
	}

	if len(src.Locales) == 0 {
		return nil, errors.New("no locales found under locales/")
	}

	return src, nil
}

// decodeKeyed reads a keyed file (see the package doc), keeping the file's order, which the built samples follow.
func decodeKeyed(fsys fs.FS, name string) ([]keyedSection, error) {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", name, err)
	}

	if len(doc.Content) == 0 {
		return nil, nil
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("decoding %s: expected a mapping of names to keyed entries", name)
	}

	var out []keyedSection

	for i := 0; i+1 < len(root.Content); i += 2 {
		secName, body := root.Content[i].Value, root.Content[i+1]
		if body.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("decoding %s: line %d: %s must map keys to text", name, body.Line, secName)
		}

		sec := keyedSection{name: secName}
		seen := map[string]bool{}

		for j := 0; j+1 < len(body.Content); j += 2 {
			key, text := body.Content[j], body.Content[j+1]
			if text.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("decoding %s: line %d: %s.%s must be text, one pattern or value per line", name, text.Line, secName, key.Value)
			}

			if seen[key.Value] {
				return nil, fmt.Errorf("decoding %s: line %d: %s.%s is defined twice", name, key.Line, secName, key.Value)
			}

			seen[key.Value] = true

			var lines []string

			for line := range strings.SplitSeq(text.Value, "\n") {
				if line = strings.TrimSpace(line); line != "" {
					lines = append(lines, line)
				}
			}

			if len(lines) == 0 {
				return nil, fmt.Errorf("decoding %s: line %d: %s.%s is empty", name, text.Line, secName, key.Value)
			}

			sec.entries = append(sec.entries, keyedEntry{key: key.Value, lines: lines})
		}

		out = append(out, sec)
	}

	return out, nil
}

func decodeFile(fsys fs.FS, name string, v any) error {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return fmt.Errorf("reading %s: %w", name, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)

	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decoding %s: %w", name, err)
	}

	return nil
}
