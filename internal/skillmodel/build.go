package skillmodel

import (
	"fmt"
	"maps"
	"slices"
	"sort"
)

// Build produces the interaction model for one locale.
func Build(src *Sources, locale string, opts BuildOptions) (*InteractionModel, error) {
	loc, ok := src.Locales[locale]
	if !ok {
		return nil, fmt.Errorf("unknown locale %q", locale)
	}

	utterances, err := src.Utterances(locale)
	if err != nil {
		return nil, err
	}

	lm := LanguageModel{InvocationName: loc.InvocationName(opts.Invocation)}
	used := map[string]bool{}
	excluded := map[string]bool{}

	for _, in := range src.Model.Intents {
		if in.Content != "" && opts.Content != nil && !slices.Contains(opts.Content, in.Content) {
			for _, s := range in.Slots {
				excluded[s.Type] = true
			}

			continue
		}

		mi := ModelIntent{Name: in.Name, Samples: []string{}}

		for _, s := range in.Slots {
			mi.Slots = append(mi.Slots, ModelSlot(s))
			used[s.Type] = true
		}

		for _, pattern := range utterances[in.Name] {
			samples, err := Expand(pattern)
			if err != nil {
				return nil, fmt.Errorf("locale %s, intent %s: %w", locale, in.Name, err)
			}

			mi.Samples = append(mi.Samples, samples...)
		}

		mi.Samples = dedupe(mi.Samples)
		lm.Intents = append(lm.Intents, mi)
	}

	static, err := src.Types(locale)
	if err != nil {
		return nil, err
	}

	// Types only left-out intents used are left out too: Amazon has no use for them, and a dynamic one may have nothing to fill it.
	dropped := map[string]bool{}

	for name := range excluded {
		if !used[name] {
			dropped[name] = true
		}
	}

	types, err := buildTypes(src.Model.Types, static, used, dropped, opts.SlotValues)
	if err != nil {
		return nil, fmt.Errorf("locale %s: %w", locale, err)
	}

	lm.Types = types

	var m InteractionModel
	m.InteractionModel.LanguageModel = lm

	return &m, nil
}

// buildTypes emits each custom type, plus any built-in type that was given extra values, sorted by name for stable output.
func buildTypes(custom []SlotType, static map[string][]SlotValue, used, dropped map[string]bool, extra map[string][]SlotValue) ([]ModelType, error) {
	values := map[string][]SlotValue{}

	for _, t := range custom {
		if t.Dynamic && len(extra[t.Name]) > 0 {
			values[t.Name] = nil

			continue
		}

		values[t.Name] = append(values[t.Name], static[t.Name]...)
	}

	for name, vs := range extra {
		if !IsBuiltin(name) {
			if _, ok := values[name]; !ok {
				return nil, fmt.Errorf("values supplied for undeclared slot type %s", name)
			}
		}

		values[name] = append(values[name], vs...)
	}

	names := slices.Sorted(maps.Keys(values))
	out := make([]ModelType, 0, len(names))

	for _, name := range names {
		if (IsBuiltin(name) && !used[name]) || dropped[name] {
			continue
		}

		if len(values[name]) == 0 {
			return nil, fmt.Errorf("slot type %s has no values; Amazon rejects empty custom types", name)
		}

		mt := ModelType{Name: name}

		for _, v := range values[name] {
			var tv ModelTypeValue

			tv.ID = v.ID
			tv.Name.Value = v.Value
			tv.Name.Synonyms = v.Synonyms
			mt.Values = append(mt.Values, tv)
		}

		out = append(out, mt)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out, nil
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0]

	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}

	return out
}
