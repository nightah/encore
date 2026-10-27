package docsgen

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/nightah/encore/internal/skillmodel"
)

// A language missing from languageNames is shown by its code.
var languageNames = map[string]string{
	"en": "English",
	"de": "German (Deutsch)",
	"fr": "French (Français)",
	"es": "Spanish (Español)",
	"it": "Italian (Italiano)",
}

// VoiceCommands renders one voice command page per language, keyed by language code. modelFS is the i18n directory, read again for the section comments in model.yaml.
func VoiceCommands(src *skillmodel.Sources, modelFS fs.FS) (map[string][]byte, error) {
	titles, err := sectionTitles(modelFS)
	if err != nil {
		return nil, err
	}

	langs := make([]string, 0, len(src.Languages))
	for lang := range src.Languages {
		langs = append(langs, lang)
	}

	slices.Sort(langs)

	out := map[string][]byte{}

	for i, lang := range langs {
		data := voiceData(src, lang, titles)
		data["Order"] = i + 1
		data["Lang"] = lang
		data["EditURL"] = editURL(lang)

		if lang == "en" {
			data["Order"] = 0
		}

		page, err := render("voice.md.tmpl", data)
		if err != nil {
			return nil, err
		}

		out[lang] = page
	}

	return out, nil
}

func voiceData(src *skillmodel.Sources, lang string, titles map[string]string) map[string]any {
	name := languageNames[lang]
	if name == "" {
		name = lang
	}

	l := src.Languages[lang]

	var locales []VoiceLocale

	for _, code := range src.LocaleNames() {
		if loc := src.Locales[code]; loc.Language == lang {
			locales = append(locales, VoiceLocale{Locale: code, SkillName: loc.SkillName, Invocation: loc.Invocation})
		}
	}

	var (
		sections []VoiceSection
		builtins []string
		used     = map[string]bool{}
	)

	for _, in := range src.Model.Intents {
		if t, ok := titles[in.Name]; ok || len(sections) == 0 {
			sections = append(sections, VoiceSection{Title: t})
		}

		vi := VoiceIntent{Name: in.Name, Slots: in.Slots, Patterns: l.Utterances[in.Name]}

		for _, loc := range locales {
			if p := src.Locales[loc.Locale].Utterances[in.Name]; len(p) > 0 {
				vi.Extra = append(vi.Extra, LocalePatterns{Locale: loc.Locale, Patterns: p})
			}
		}

		if len(vi.Patterns) == 0 && len(vi.Extra) == 0 {
			if skillmodel.IsBuiltin(in.Name) {
				builtins = append(builtins, in.Name)
			}

			continue
		}

		for _, s := range in.Slots {
			used[s.Type] = true
		}

		last := &sections[len(sections)-1]
		last.Intents = append(last.Intents, vi)
	}

	sections = slices.DeleteFunc(sections, func(s VoiceSection) bool { return len(s.Intents) == 0 })

	var types []VoiceType

	for _, t := range src.Model.Types {
		if !used[t.Name] {
			continue
		}

		types = append(types, VoiceType{Name: t.Name, Dynamic: t.Dynamic, Values: append(slices.Clone(t.Values), l.Types[t.Name]...)})
	}

	return map[string]any{
		"Lang": lang, "Name": name, "Locales": locales, "Sections": sections, "Builtins": builtins, "Types": types,
	}
}

// sectionTitles reads the comments that head groups of intents in model.yaml, keyed by the first intent of each group.
func sectionTitles(fsys fs.FS) (map[string]string, error) {
	b, err := fs.ReadFile(fsys, "model.yaml")
	if err != nil {
		return nil, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}

	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("model.yaml is not a mapping")
	}

	root := doc.Content[0]

	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "intents" {
			continue
		}

		titles := map[string]string{}

		for _, item := range root.Content[i+1].Content {
			if item.HeadComment == "" {
				continue
			}

			var in skillmodel.Intent
			if err := item.Decode(&in); err != nil {
				return nil, err
			}

			titles[in.Name] = sectionTitle(item.HeadComment)
		}

		return titles, nil
	}

	return nil, fmt.Errorf("model.yaml has no intents")
}

// sectionTitle turns a "# Movies." comment into "Movies". Only the comment's last paragraph counts, since a blank line separates it from any comment above.
func sectionTitle(comment string) string {
	paras := strings.Split(strings.TrimSpace(comment), "\n\n")

	var lines []string
	for line := range strings.SplitSeq(paras[len(paras)-1], "\n") {
		lines = append(lines, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#")))
	}

	return strings.TrimSuffix(strings.Join(lines, " "), ".")
}

// editURL is where a language's voice commands are changed: English in the repository, every translation on Crowdin, whose pull requests overwrite edits made to the translated files directly.
func editURL(lang string) string {
	if lang == "en" {
		return SourceEditURL
	}

	return CrowdinProject + "/" + crowdinLanguage(lang)
}

// crowdinLanguage maps a language directory under i18n/lang to the language's code on Crowdin, which differs where Crowdin names a regional variant.
func crowdinLanguage(lang string) string {
	if lang == "es" {
		return "es-ES"
	}

	return lang
}
