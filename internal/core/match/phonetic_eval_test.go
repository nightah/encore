package match

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// evalEncoder is a candidate phonetic function for TestPhoneticEval. keys returns nil for a language it does not cover, which keeps the production encoder.
type evalEncoder struct {
	name string
	keys func(code string) phoneticFunc
}

// evalOutcome is one case's result under one encoder and retriever.
type evalOutcome struct {
	suite, heard string
	ok, wrong    bool
	why          string
}

// evalEncoders are compared against "current", the production encoders. "none" makes every word its own key, which measures how much the phonetic layer contributes at all.
var evalEncoders = []evalEncoder{
	{name: "current", keys: func(string) phoneticFunc { return nil }},
	{name: "none", keys: func(string) phoneticFunc { return identityKeys }},
}

func identityKeys(tok string) []string { return []string{tok} }

// withPhonetic swaps a language's phonetic function until the test ends. The bleve filter and the trigram retriever read it when an index is built, so it must be set before New. Tests in this package do not run in parallel, so swapping the shared language is safe.
func withPhonetic(t testing.TB, code string, fn phoneticFunc) {
	t.Helper()

	l := languages[code]
	old := l.phonetic
	l.phonetic = fn

	t.Cleanup(func() { l.phonetic = old })
}

// evalSuites gathers the English and German suites from asr_corpus.yaml and the per-language suites in testdata/phonetic, grouped by language.
func evalSuites(t *testing.T) map[string][]corpusSuite {
	t.Helper()

	out := map[string][]corpusSuite{}

	for _, s := range loadCorpus(t).Suites {
		out[s.Lang] = append(out[s.Lang], s)
	}

	paths, err := filepath.Glob("testdata/phonetic/*.yaml")
	require.NoError(t, err)

	for _, p := range paths {
		raw, err := os.ReadFile(p)
		require.NoError(t, err)

		var c corpus
		require.NoError(t, yaml.Unmarshal(raw, &c), p)

		for _, s := range c.Suites {
			out[s.Lang] = append(out[s.Lang], s)
		}
	}

	return out
}

// runEval scores every case of the suites with the given retriever and returns one outcome per case, in corpus order.
func runEval(t *testing.T, suites []corpusSuite, retr string) []evalOutcome {
	t.Helper()

	var out []evalOutcome

	for _, suite := range suites {
		ix, err := New(suite.Lang, Config{Retriever: retr})
		require.NoError(t, err)
		require.NoError(t, ix.Upsert(suite.docs()...))

		for _, tc := range suite.Cases {
			res, err := ix.Search(t.Context(), tc.query())
			require.NoError(t, err)

			best, _ := res.Best()
			why := tc.check(res)
			out = append(out, evalOutcome{
				suite: suite.Name,
				heard: tc.Heard,
				ok:    why == "",
				wrong: res.Decision == DecisionPlay && (tc.Want != "play" || best.Doc.ID != tc.ID),
				why:   why,
			})
		}

		require.NoError(t, ix.Close())
	}

	return out
}

// evalScore is a language's tally under one encoder and retriever.
type evalScore struct {
	correct, total, wrong int
}

func tally(out []evalOutcome) evalScore {
	s := evalScore{total: len(out)}

	for _, o := range out {
		if o.ok {
			s.correct++
		}

		if o.wrong {
			s.wrong++
		}
	}

	return s
}

func (s evalScore) String() string {
	return fmt.Sprintf("%3d/%3d (%5.1f%%) wrong plays %d", s.correct, s.total, 100*float64(s.correct)/float64(s.total), s.wrong)
}

// TestPhoneticEval measures match accuracy per language through the real pipeline (normalise, alias, retrieve, score, decide) with each candidate phonetic encoder swapped in. It logs a table, the production encoder's misses and every case another encoder gains or loses against it; run it with -v to read them. It asserts nothing about the candidates, so it never blocks on one.
func TestPhoneticEval(t *testing.T) {
	suites := evalSuites(t)
	codes := slices.Sorted(func(yield func(string) bool) {
		for c := range suites {
			if !yield(c) {
				return
			}
		}
	})

	var table strings.Builder

	for _, retr := range []string{RetrieverBleve, RetrieverTrigram} {
		for _, code := range codes {
			var base []evalOutcome

			for _, enc := range evalEncoders {
				fn := enc.keys(code)
				if fn == nil && enc.name != "current" {
					continue
				}

				var out []evalOutcome

				t.Run(retr+"/"+code+"/"+enc.name, func(t *testing.T) {
					if fn != nil {
						withPhonetic(t, code, fn)
					}

					out = runEval(t, suites[code], retr)
				})

				fmt.Fprintf(&table, "%-8s %-3s %-10s %s\n", retr, code, enc.name, tally(out))

				if enc.name == "current" {
					base = out

					for _, o := range out {
						if !o.ok {
							t.Logf("%s %s current misses [%s] %q: %s", retr, code, o.suite, o.heard, o.why)
						}
					}

					continue
				}

				for i, o := range out {
					switch {
					case o.ok && !base[i].ok:
						t.Logf("%s %s %s gains [%s] %q (current: %s)", retr, code, enc.name, o.suite, o.heard, base[i].why)
					case !o.ok && base[i].ok:
						t.Logf("%s %s %s loses [%s] %q: %s", retr, code, enc.name, o.suite, o.heard, o.why)
					}
				}
			}

			assert.NotEmpty(t, base, "no current run for %s", code)
		}
	}

	t.Logf("phonetic encoder evaluation:\n%s", table.String())
}
