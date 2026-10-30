package skillpkg

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/nightah/encore/internal/fuzzy/loanword"
	"github.com/nightah/encore/internal/skillmodel"
)

// nativeSynonyms adds to the slot values, as synonyms, the forms a recogniser for locale writes their Latin-script words in (loanword.Native: "スーパーフライ" for "Superfly" in ja-JP), so Alexa resolves a title it hears and writes in the locale's script. They take at most budget bytes of the encoded model, so they never push a model towards Amazon's limit or cost a title its place: each value's likeliest form goes in first, the cheapest values first, then the forms of its other names and its other forms. It returns the values, and how many forms went in out of how many there were. A locale loanword doesn't write gets the values as they are.
func nativeSynonyms(locale string, in map[string][]skillmodel.SlotValue, budget int) (out map[string][]skillmodel.SlotValue, added, wanted int) {
	if !loanword.Supports(locale) || len(in) == 0 {
		return in, 0, 0
	}

	var cands []nativeCandidate

	for _, typ := range slices.Sorted(maps.Keys(in)) {
		for i, v := range in[typ] {
			taken := append([]string{v.Value}, v.Synonyms...)
			rank := 0

			for _, name := range taken[:len(taken):len(taken)] {
				for _, f := range loanword.Native(locale, name) {
					f = CleanSlotText(f)
					if f == "" || tooLong(f) || slices.ContainsFunc(taken, func(x string) bool { return strings.EqualFold(x, f) }) {
						continue
					}

					taken = append(taken, f)
					cands = append(cands, nativeCandidate{typ: typ, index: i, form: f, rank: rank, cost: synonymCost(f)})
					rank++
				}
			}
		}
	}

	slices.SortStableFunc(cands, func(a, b nativeCandidate) int {
		return cmp.Or(cmp.Compare(a.rank, b.rank), cmp.Compare(a.cost, b.cost))
	})

	out = make(map[string][]skillmodel.SlotValue, len(in))
	for typ, vs := range in {
		out[typ] = slices.Clone(vs)
	}

	for _, c := range cands {
		v := &out[c.typ][c.index]

		cost := c.cost
		if len(v.Synonyms) == 0 {
			cost += len(`,"synonyms":[]`) - 1
		}

		if cost > budget {
			continue
		}

		budget -= cost
		v.Synonyms = append(slices.Clip(v.Synonyms), c.form)
		added++
	}

	return out, added, len(cands)
}

// synonymCost is how many bytes a synonym adds to the encoded model: its JSON string and the comma before it.
func synonymCost(s string) int {
	b, _ := json.Marshal(s)

	return len(b) + 1
}

// nativeWarning reports native-script synonyms left out of locale's model for want of room.
func nativeWarning(locale string, added, wanted int) []string {
	if added == wanted {
		return nil
	}

	return []string{fmt.Sprintf("locale %s: %d of %d native-script synonyms fit in the interaction model; titles without one are still found by Encore's matcher", locale, added, wanted)}
}
