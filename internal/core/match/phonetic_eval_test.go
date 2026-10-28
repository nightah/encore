package match

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
	"pgregory.net/rapid"

	"github.com/nightah/encore/internal/fuzzy/phonetic"
	_ "github.com/nightah/encore/internal/fuzzy/phonetic/de"
	_ "github.com/nightah/encore/internal/fuzzy/phonetic/en"
	_ "github.com/nightah/encore/internal/fuzzy/phonetic/es"
	_ "github.com/nightah/encore/internal/fuzzy/phonetic/fr"
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

// evalEncoders are compared against "current", the production encoders. "none" makes every word its own key, which measures how much the phonetic layer contributes at all. "fuzzy" is internal/fuzzy/phonetic, which falls back to English for languages without an encoder of their own.
var evalEncoders = []evalEncoder{
	{name: "current", keys: func(string) phoneticFunc { return nil }},
	{name: "none", keys: func(string) phoneticFunc { return identityKeys }},
	{name: "fuzzy", keys: fuzzyKeys},
}

func identityKeys(tok string) []string { return []string{tok} }

// fuzzyKeys adapts an internal/fuzzy encoder to the matcher's phoneticFunc.
func fuzzyKeys(code string) phoneticFunc {
	enc := phonetic.For(code)

	return func(tok string) []string { return enc.Keys(make([]string, 0, 3), tok) }
}

// corpusTokens returns every normalised token and compact form the matcher would encode for a language's suites: titles, aliases and what was heard.
func corpusTokens(l *language, suites []corpusSuite) []string {
	seen := map[string]bool{}

	add := func(toks []string) {
		for _, tok := range toks {
			seen[tok] = true
		}

		seen[strings.Join(toks, "")] = true
	}

	for _, s := range suites {
		for _, d := range s.Library {
			for _, a := range append([]string{d.Title}, d.Aliases...) {
				add(l.normalize(a, true))
				add(l.normalize(a, false))
			}
		}

		for _, c := range s.Cases {
			add(l.normalize(c.Heard, false))
		}
	}

	return slices.Sorted(maps.Keys(seen))
}

// TestFuzzyPortsMatchProduction checks that internal/fuzzy's English and German encoders give the production keys for every token and compact form in the corpora and for random normalised text, so swapping them in cannot change a match.
func TestFuzzyPortsMatchProduction(t *testing.T) {
	suites := evalSuites(t)

	for _, code := range []string{"en", "de"} {
		l, enc := languages[code], fuzzyKeys(code)

		toks := corpusTokens(l, suites[code])
		for _, tok := range toks {
			require.Equal(t, l.phonetic(tok), enc(tok), "%s %q", code, tok)
		}

		t.Logf("%s: %d corpus tokens agree", code, len(toks))

		rapid.Check(t, func(t *rapid.T) {
			toks := l.normalize(genText().Draw(t, "s"), true)
			for _, tok := range append(toks, strings.Join(toks, "")) {
				if got, want := enc(tok), l.phonetic(tok); !slices.Equal(got, want) {
					t.Fatalf("%s %q: fuzzy %q, production %q", code, tok, got, want)
				}
			}
		})
	}
}

// withPhonetic swaps a language's phonetic function until the test ends. The inverted index and the trigram retriever read it when an index is built, so it must be set before New. Tests in this package do not run in parallel, so swapping the shared language is safe.
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

// fuzzyGate is how many more correct decisions than production internal/fuzzy must make per language: its own French and Spanish encoders earn their place only with a measurable gain, and the ports of the production encoders must not lose any.
var fuzzyGate = map[string]int{"fr": 3, "es": 3}

// TestPhoneticEval measures match accuracy per language through the real pipeline (normalise, alias, retrieve, score, decide) with each candidate phonetic encoder swapped in. It logs a table, the production encoder's misses and every case another encoder gains or loses against it; run it with -v to read them. Only internal/fuzzy is held to fuzzyGate, with no wrong plays added.
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

	for _, retr := range []string{RetrieverInverted, RetrieverTrigram} {
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

				if enc.name == "fuzzy" {
					got, was := tally(out), tally(base)
					assert.GreaterOrEqual(t, got.correct, was.correct+fuzzyGate[code], "%s %s: fuzzy must gain at least %d correct decisions", retr, code, fuzzyGate[code])
					assert.LessOrEqual(t, got.wrong, was.wrong, "%s %s: fuzzy must not add wrong plays", retr, code)
				}
			}

			assert.NotEmpty(t, base, "no current run for %s", code)
		}
	}

	t.Logf("phonetic encoder evaluation:\n%s", table.String())
}

// BenchmarkPhoneticUpsert measures building an index of 2000 synthetic titles with each encoder swapped in, which is where encoders run most: once per word and compact form of every alias.
func BenchmarkPhoneticUpsert(b *testing.B) {
	docs := syntheticDocs(2000, 3)

	for _, code := range []string{"en", "de", "fr", "es", "it"} {
		for _, enc := range evalEncoders {
			if enc.name == "none" {
				continue
			}

			b.Run("lang="+code+"/enc="+enc.name, func(b *testing.B) {
				if fn := enc.keys(code); fn != nil {
					withPhonetic(b, code, fn)
				}

				b.ReportAllocs()

				for range b.N {
					ix, err := New(code, Config{})
					require.NoError(b, err)
					require.NoError(b, ix.Upsert(docs...))
					require.NoError(b, ix.Close())
				}
			})
		}
	}
}
