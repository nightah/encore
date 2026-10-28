//go:build phoneticeval

// The stress tests below only log, and take a minute under the race detector, so they run on demand: go test -tags phoneticeval -run 'TestPhonetic(Collisions|Distractors)' -v ./internal/core/match/

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

	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
)

// vocabulary is every distinct normalised word of at least three letters in Encore's own translations for a language (i18n/lang and the web UI locales): real words of the language that nobody chose to suit an encoder.
func vocabulary(t *testing.T, l *language) []string {
	t.Helper()

	var paths []string

	for _, pattern := range []string{"../../../i18n/lang/" + l.code + "/*.yaml", "../../../web/src/i18n/locales/" + l.code + "/*.json"} {
		m, err := filepath.Glob(pattern)
		require.NoError(t, err)

		paths = append(paths, m...)
	}

	require.NotEmpty(t, paths, "no translations for %s", l.code)

	seen := map[string]bool{}

	for _, p := range paths {
		raw, err := os.ReadFile(p)
		require.NoError(t, err)

		for _, tok := range l.normalize(string(raw), false) {
			if len(tok) >= 3 && !strings.ContainsAny(tok, "0123456789") {
				seen[tok] = true
			}
		}
	}

	return slices.Sorted(maps.Keys(seen))
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

// TestPhoneticDistractors stresses precision with vocabulary nobody chose: a library of 2000 synthetic titles of one to four words from the language's translations, queried with 400 other such titles that are not in it. Any play is a wrong play; asks cost a question. It logs the counts per encoder and retriever.
func TestPhoneticDistractors(t *testing.T) {
	var table strings.Builder

	for _, code := range Languages() {
		l := languages[code]
		words := vocabulary(t, l)
		r := rand.New(rand.NewPCG(11, 11))

		title := func() string {
			w := make([]string, 1+r.IntN(4))
			for i := range w {
				w[i] = words[r.IntN(len(words))]
			}

			return strings.Join(w, " ")
		}

		docs := make([]Doc, 2000)
		inLibrary := map[string]bool{}

		for i := range docs {
			docs[i] = Doc{ID: fmt.Sprintf("d%d", i), Kind: media.KindMovie, Ref: media.Ref{Kind: media.KindMovie, ID: i + 1}, Title: title()}
			inLibrary[Normalize(code, docs[i].Title)] = true
		}

		queries := make([]string, 0, 400)
		for len(queries) < cap(queries) {
			if q := title(); !inLibrary[Normalize(code, q)] {
				queries = append(queries, q)
			}
		}

		for _, retr := range []string{RetrieverInverted, RetrieverTrigram} {
			for _, enc := range evalEncoders {
				if enc.name == "none" {
					continue
				}

				t.Run(retr+"/"+code+"/"+enc.name, func(t *testing.T) {
					if fn := enc.keys(code); fn != nil {
						withPhonetic(t, code, fn)
					}

					ix, err := New(code, Config{Retriever: retr})
					require.NoError(t, err)
					require.NoError(t, ix.Upsert(docs...))

					var plays, asks int

					for _, q := range queries {
						res, err := ix.Search(t.Context(), Query{Text: q})
						require.NoError(t, err)

						switch res.Decision {
						case DecisionPlay:
							plays++
						case DecisionAsk:
							asks++
						}
					}

					require.NoError(t, ix.Close())
					fmt.Fprintf(&table, "%-8s %-3s %-8s wrong plays %3d, asks %3d of %d absent titles\n", retr, code, enc.name, plays, asks, len(queries))
				})
			}
		}
	}

	t.Logf("absent-title distractors:\n%s", table.String())
}
