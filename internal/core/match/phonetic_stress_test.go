//go:build phoneticeval

// TestPhoneticCollisions only logs, and TestPhoneticDistractors holds production to wrong-play ceilings. TestPhoneticDistractors takes about two minutes, and more than the ten minutes go test allows by default under the race detector, so both sit behind a build tag, which also runs TestPhoneticEval in full (evalFull): CI runs TestPhoneticDistractors and TestPhoneticEval on every push and pull request in a job of its own without -race (.github/workflows/ci-go.yml). By hand: go test -tags phoneticeval -run 'TestPhonetic(Eval|Collisions|Distractors)' -v ./internal/core/match/

package match

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
)

// vocabulary is the words a language's synthetic titles are made of: every distinct normalised word of at least three letters in Encore's own translations (i18n/lang and the web UI locales), real words of the language that nobody chose to suit an encoder. They are kept in testdata/distractors, so a translation update never moves the wrong-play ceilings TestPhoneticDistractors holds production to.
func vocabulary(t *testing.T, l *language) []string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "distractors", l.code+".txt"))
	require.NoError(t, err)

	words := strings.Fields(string(raw))
	require.NotEmpty(t, words, "no vocabulary for %s", l.code)

	return words
}

// collisions counts the pairs of distinct words that share a phonetic key, the over-merging that can make different titles sound alike.
func collisions(words []string, fn phoneticFunc) (pairs, total int) {
	byKey := map[string][]int{}

	for i, w := range words {
		for _, k := range fn(w) {
			byKey[k] = append(byKey[k], i)
		}
	}

	shared := map[[2]int]bool{}

	for _, ids := range byKey {
		for a := range ids {
			for b := a + 1; b < len(ids); b++ {
				shared[[2]int{ids[a], ids[b]}] = true
			}
		}
	}

	return len(shared), len(words) * (len(words) - 1) / 2
}

// TestPhoneticCollisions logs, per language, how many pairs of real words each encoder gives a shared key. A candidate that wins on the corpus by merging everything shows up here as a jump in collisions.
func TestPhoneticCollisions(t *testing.T) {
	var table strings.Builder

	for _, code := range Languages() {
		l := languages[code]
		words := vocabulary(t, l)

		for _, enc := range evalEncoders {
			fn := enc.keys(code)
			if fn == nil {
				fn = l.phonetic
			}

			pairs, total := collisions(words, fn)
			fmt.Fprintf(&table, "%-3s %-8s %5d words %6d colliding pairs (%.3f%% of %d)\n", code, enc.name, len(words), pairs, 100*float64(pairs)/float64(total), total)
		}
	}

	t.Logf("phonetic key collisions over translation vocabulary:\n%s", table.String())
}

// distractorSeeds each build a library of 2000 titles and 400 absent queries, so a run asks 2000 absent titles per language and retriever.
var distractorSeeds = []uint64{11, 12, 13, 14, 15}

// distractorSet is one seed's library and the absent titles queried against it.
type distractorSet struct {
	docs    []Doc
	queries []string
}

// distractors builds a library of 2000 synthetic titles of one to four words from vocabulary, and 400 other such titles that are not in it.
func distractors(code string, words []string, seed uint64) distractorSet {
	r := rand.New(rand.NewPCG(seed, seed))

	title := func() string {
		w := make([]string, 1+r.IntN(4))
		for i := range w {
			w[i] = words[r.IntN(len(words))]
		}

		return strings.Join(w, " ")
	}

	set := distractorSet{docs: make([]Doc, 2000), queries: make([]string, 0, 400)}
	inLibrary := map[string]bool{}

	for i := range set.docs {
		set.docs[i] = Doc{ID: fmt.Sprintf("d%d", i), Kind: media.KindMovie, Ref: media.Ref{Kind: media.KindMovie, ID: i + 1}, Title: title()}
		inLibrary[Normalize(code, set.docs[i].Title)] = true
	}

	for len(set.queries) < cap(set.queries) {
		if q := title(); !inLibrary[Normalize(code, q)] {
			set.queries = append(set.queries, q)
		}
	}

	return set
}

// distractorPlays returns, for each absent title that played, the title it played, keyed by seed, position and query so a title drawn twice counts twice.
func distractorPlays(t *testing.T, code, retr string, sets []distractorSet) (plays map[string]string, asks int) {
	t.Helper()

	plays = map[string]string{}

	for i, set := range sets {
		ix, err := New(code, Config{Retriever: retr})
		require.NoError(t, err)
		require.NoError(t, ix.Upsert(set.docs...))

		for j, q := range set.queries {
			res, err := ix.Search(t.Context(), Query{Text: q})
			require.NoError(t, err)

			switch res.Decision {
			case DecisionPlay:
				best, _ := res.Best()
				plays[fmt.Sprintf("%d/%03d/%s", i, j, q)] = best.Doc.Title
			case DecisionAsk:
				asks++
			}
		}

		require.NoError(t, ix.Close())
	}

	return plays, asks
}

// distractorCeiling is the most absent titles production may play per retriever and language, of the 2000 TestPhoneticDistractors asks: the counts measured when they were set. Lower one when a change plays fewer; raise one only with the evidence that the accuracy tables in docs/src/content/docs/contributing/dependencies.md are updated with.
var distractorCeiling = map[string]int{
	"inverted/de": 102, "inverted/en": 62, "inverted/es": 120, "inverted/fr": 73, "inverted/it": 94, "inverted/ar": 84, "inverted/hi": 82, "inverted/ja": 2000, "inverted/nl": 2000, "inverted/pt": 2000,
	"trigram/de": 102, "trigram/en": 62, "trigram/es": 119, "trigram/fr": 73, "trigram/it": 94, "trigram/ar": 84, "trigram/hi": 82, "trigram/ja": 2000, "trigram/nl": 2000, "trigram/pt": 2000,
}

// TestPhoneticDistractors stresses precision with vocabulary nobody chose: for each of distractorSeeds, a library of 2000 synthetic titles of one to four words from the language's translations, queried with 400 other such titles that are not in it. Any play is a wrong play; asks cost a question. It logs the counts per encoder and retriever, and every play an encoder adds or removes against English Double Metaphone, and fails when production plays more than its distractorCeiling.
func TestPhoneticDistractors(t *testing.T) {
	var table strings.Builder

	for _, code := range Languages() {
		words := vocabulary(t, languages[code])

		sets := make([]distractorSet, len(distractorSeeds))
		for i, seed := range distractorSeeds {
			sets[i] = distractors(code, words, seed)
		}

		for _, retr := range []string{RetrieverInverted, RetrieverTrigram} {
			var english map[string]string

			// English goes first: the others are compared with it.
			for _, name := range []string{"english", "current", "fuzzy"} {
				enc := evalEncoderNamed(name)

				t.Run(retr+"/"+code+"/"+enc.name, func(t *testing.T) {
					if fn := enc.keys(code); fn != nil {
						withPhonetic(t, code, fn)
					}

					plays, asks := distractorPlays(t, code, retr, sets)
					fmt.Fprintf(&table, "%-8s %-3s %-8s wrong plays %4d, asks %4d of %d absent titles\n", retr, code, enc.name, len(plays), asks, 400*len(sets))

					if enc.name == "english" {
						english = plays

						return
					}

					if enc.name == "current" {
						ceiling, ok := distractorCeiling[retr+"/"+code]
						require.True(t, ok, "no distractor ceiling for %s/%s", retr, code)
						assert.LessOrEqual(t, len(plays), ceiling, "%s %s: production plays more absent titles than its ceiling", retr, code)
					}

					for _, q := range slices.Sorted(maps.Keys(plays)) {
						if _, ok := english[q]; !ok {
							t.Logf("%s %s %s adds %q -> %q", retr, code, enc.name, q, plays[q])
						}
					}

					for _, q := range slices.Sorted(maps.Keys(english)) {
						if _, ok := plays[q]; !ok {
							t.Logf("%s %s %s removes %q -> %q", retr, code, enc.name, q, english[q])
						}
					}
				})
			}
		}
	}

	t.Logf("absent-title distractors:\n%s", table.String())
}

func evalEncoderNamed(name string) evalEncoder {
	i := slices.IndexFunc(evalEncoders, func(e evalEncoder) bool { return e.name == name })

	return evalEncoders[i]
}
