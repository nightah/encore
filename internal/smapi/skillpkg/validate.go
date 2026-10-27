package skillpkg

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
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

// filterSlotValues drops build-time slot values and synonyms that exceed MaxSlotValueLength, which would otherwise fail the whole build over one long title.
func filterSlotValues(in map[string][]skillmodel.SlotValue) (map[string][]skillmodel.SlotValue, []string) {
	if in == nil {
		return nil, nil
	}

	out := make(map[string][]skillmodel.SlotValue, len(in))

	var warnings []string

	for _, typ := range slices.Sorted(maps.Keys(in)) {
		dropped := 0
		kept := make([]skillmodel.SlotValue, 0, len(in[typ]))

		for _, v := range in[typ] {
			if tooLong(v.Value) {
				dropped++

				continue
			}

			var syns []string

			for _, s := range v.Synonyms {
				if tooLong(s) {
					dropped++

					continue
				}

				syns = append(syns, s)
			}

			v.Synonyms = syns
			kept = append(kept, v)
		}

		if dropped > 0 {
			warnings = append(warnings, fmt.Sprintf("slot type %s: dropped %d values or synonyms longer than %d characters", typ, dropped, MaxSlotValueLength))
		}

		out[typ] = kept
	}

	return out, warnings
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
