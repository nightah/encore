package skillpkg

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nightah/encore/internal/skillmodel"
)

func (e Endpoint) validate() error {
	u, err := url.Parse(e.URI)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("endpoint %q must be an https:// URL", e.URI)
	}

	return nil
}

func (p Privacy) validate() error {
	var missing []string

	for name, v := range map[string]*bool{
		"allowsPurchases":   p.AllowsPurchases,
		"usesPersonalInfo":  p.UsesPersonalInfo,
		"isChildDirected":   p.IsChildDirected,
		"isExportCompliant": p.IsExportCompliant,
		"containsAds":       p.ContainsAds,
	} {
		if v == nil {
			missing = append(missing, name)
		}
	}

	if len(missing) == 0 {
		return nil
	}

	slices.Sort(missing)

	return fmt.Errorf("privacy and compliance questions unanswered: %s", strings.Join(missing, ", "))
}

// filterSlotValues cleans build-time slot values and synonyms for Amazon (CleanSlotText), then drops those left empty or longer than MaxSlotValueLength, which would otherwise fail the whole build over one title, and merges values that clean to the same text: the first keeps its ID and gains the others' synonyms.
func filterSlotValues(in map[string][]skillmodel.SlotValue) (map[string][]skillmodel.SlotValue, []string) {
	if in == nil {
		return nil, nil
	}

	out := make(map[string][]skillmodel.SlotValue, len(in))

	var warnings []string

	for _, typ := range slices.Sorted(maps.Keys(in)) {
		dropped, empty := 0, 0
		kept := make([]skillmodel.SlotValue, 0, len(in[typ]))
		byValue := map[string]int{}

		for _, v := range in[typ] {
			value := CleanSlotText(v.Value)

			switch {
			case value == "":
				empty++

				continue
			case tooLong(value):
				dropped++

				continue
			}

			i, seen := byValue[strings.ToLower(value)]
			if !seen {
				i = len(kept)
				byValue[strings.ToLower(value)] = i
				kept = append(kept, skillmodel.SlotValue{ID: v.ID, Value: value})
			}

			for _, s := range v.Synonyms {
				s = CleanSlotText(s)

				switch {
				case s == "":
					empty++
				case tooLong(s):
					dropped++
				case !strings.EqualFold(s, kept[i].Value) && !slices.ContainsFunc(kept[i].Synonyms, func(x string) bool { return strings.EqualFold(x, s) }):
					kept[i].Synonyms = append(kept[i].Synonyms, s)
				}
			}
		}

		if dropped > 0 {
			warnings = append(warnings, fmt.Sprintf("slot type %s: dropped %d values or synonyms longer than %d characters", typ, dropped, MaxSlotValueLength))
		}

		if empty > 0 {
			warnings = append(warnings, fmt.Sprintf("slot type %s: dropped %d values or synonyms with nothing to say once cleaned", typ, empty))
		}

		out[typ] = kept
	}

	return out, warnings
}

// kodiTags matches Kodi's label formatting tags, opening and closing, in any case.
var kodiTags = regexp.MustCompile(`(?i)\[(?:/?(?:b|i|light|uppercase|lowercase|capitalize|tabs)|color[^\]]*|/color|cr)\]`)

// CleanSlotText makes a title or name fit for a slot value: Kodi's label formatting tags are removed ([B], [I], [LIGHT], [COLOR x], [UPPERCASE], [LOWERCASE], [CAPITALIZE], [TABS] and their closing tags, with [CR] a space), as are control and format characters, symbols such as emoji and their skin tones and keycaps, which can't be said, and '*', which Amazon rejects in a slot value ("*NSYNC"). Runs of spaces collapse to one and the ends are trimmed.
func CleanSlotText(s string) string {
	s = kodiTags.ReplaceAllStringFunc(s, func(tag string) string {
		if strings.EqualFold(tag, "[cr]") {
			return " "
		}

		return ""
	})

	s = strings.Map(func(r rune) rune {
		switch {
		case r == '*':
			return -1
		case unicode.IsSpace(r):
			return ' '
		case unicode.In(r, unicode.Cc, unicode.Cf, unicode.Co, unicode.Cs, unicode.So, unicode.Me), r >= 0x1F3FB && r <= 0x1F3FF, r == utf8.RuneError:
			return -1
		}

		return r
	}, s)

	return strings.Join(strings.Fields(s), " ")
}

func tooLong(s string) bool {
	return utf8.RuneCountInString(s) > MaxSlotValueLength
}

// encodeModel also checks the model against Amazon's limits.
func encodeModel(locale string, m *skillmodel.InteractionModel) ([]byte, []string, error) {
	lm := m.InteractionModel.LanguageModel

	if len(lm.Intents) > MaxIntents {
		return nil, nil, fmt.Errorf("skillpkg: locale %s: %d intents exceeds Amazon's limit of %d", locale, len(lm.Intents), MaxIntents)
	}

	for _, t := range lm.Types {
		for _, v := range t.Values {
			if tooLong(v.Name.Value) {
				return nil, nil, fmt.Errorf("skillpkg: locale %s: slot type %s value %.40q... is longer than %d characters", locale, t.Name, v.Name.Value, MaxSlotValueLength)
			}

			for _, s := range v.Name.Synonyms {
				if tooLong(s) {
					return nil, nil, fmt.Errorf("skillpkg: locale %s: slot type %s synonym %.40q... is longer than %d characters", locale, t.Name, s, MaxSlotValueLength)
				}
			}
		}
	}

	data, err := json.Marshal(m)
	if err != nil {
		return nil, nil, fmt.Errorf("skillpkg: locale %s: encoding model: %w", locale, err)
	}

	var warnings []string

	switch size := len(data); {
	case size > MaxModelBytes:
		return nil, nil, fmt.Errorf("skillpkg: locale %s: interaction model is %d bytes, over Amazon's %d byte limit; reduce the slot values", locale, size, MaxModelBytes)
	case size > WarnModelBytes:
		warnings = append(warnings, fmt.Sprintf("locale %s: interaction model is %d bytes, close to Amazon's %d byte limit", locale, size, MaxModelBytes))
	}

	return data, warnings, nil
}
