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

	"github.com/nightah/encore/internal/fuzzy/phonetic"
	"github.com/nightah/encore/internal/fuzzy/phonetic/de"
	"github.com/nightah/encore/internal/fuzzy/phonetic/en"
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
	ask          bool
	why          string
}

// evalEncoders are compared against "current", the production encoders. "none" makes every word its own key, which measures how much the phonetic layer contributes at all. "english" keys every language with English Double Metaphone, the baseline the gates compare with and what a language without an encoder of its own falls back to. "fuzzy" is internal/fuzzy/phonetic, which falls back to English for languages without an encoder of their own.
var evalEncoders = []evalEncoder{
	{name: "current", keys: func(string) phoneticFunc { return nil }},
	{name: "none", keys: func(string) phoneticFunc { return identityKeys }},
	{name: "english", keys: func(string) phoneticFunc { return encoderKeys(en.Encoder{}) }},
	{name: "fuzzy", keys: fuzzyKeys},
}

func identityKeys(tok string) []string { return []string{tok} }

// fuzzyKeys is the internal/fuzzy encoder registered for a language.
func fuzzyKeys(code string) phoneticFunc { return encoderKeys(phonetic.For(code)) }

// encoderKeys adapts an internal/fuzzy encoder to the matcher's phoneticFunc.
func encoderKeys(enc phonetic.Encoder) phoneticFunc {
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

// libraryKeysFile pins the keys PhonoGrams/double_metaphone and f1monkey/phonetic give each token in goldenTokens: one line per language and token, tab-separated, with the keys after the token.
const libraryKeysFile = "testdata/phonetic/library_keys.tsv"

// goldenTokens are a language's corpus tokens and compact forms (corpusTokens) plus those of 300 synthetic titles.
func goldenTokens(t *testing.T, code string, suites []corpusSuite) []string {
	t.Helper()

	l := languages[code]
	seen := map[string]bool{}

	for _, tok := range corpusTokens(l, suites) {
		seen[tok] = true
	}

	for _, d := range syntheticDocs(300, 7) {
		toks := l.normalize(d.Title, true)
		for _, tok := range append(toks, strings.Join(toks, "")) {
			seen[tok] = true
		}
	}

	return slices.Sorted(maps.Keys(seen))
}

// portedKeys is the in-tree encoder alone, without production's extra rule, for each language whose production keying adds one on top of it (germanKeys); TestPhoneticLibraryGolden checks it instead.
var portedKeys = map[string]phoneticFunc{"de": encoderKeys(de.Encoder{})}

// TestPhoneticLibraryGolden checks that production keys every token as PhonoGrams/double_metaphone and f1monkey/phonetic do, from the keys pinned in libraryKeysFile, in every language it lists: those whose in-tree encoders reproduce a library. Where production adds a rule of its own (portedKeys), the encoder alone is checked.
func TestPhoneticLibraryGolden(t *testing.T) {
	raw, err := os.ReadFile(libraryKeysFile)
	require.NoError(t, err)

	golden := map[string]map[string][]string{}

	for line := range strings.Lines(string(raw)) {
		f := strings.Split(strings.TrimSuffix(line, "\n"), "\t")
		require.GreaterOrEqual(t, len(f), 3, line)

		if golden[f[0]] == nil {
			golden[f[0]] = map[string][]string{}
		}

		golden[f[0]][f[1]] = f[2:]
	}

	suites := evalSuites(t, corpusOriginal)

	for _, code := range slices.Sorted(maps.Keys(golden)) {
		toks := goldenTokens(t, code, suites[code])
		require.Equal(t, slices.Sorted(maps.Keys(golden[code])), toks, "%s: %s pins goldenTokens", code, libraryKeysFile)

		keys := languages[code].phonetic
		if fn, ok := portedKeys[code]; ok {
			keys = fn
		}

		for _, tok := range toks {
			require.Equal(t, golden[code][tok], keys(tok), "%s %q", code, tok)
		}

		t.Logf("%s: %d tokens keyed as the libraries do", code, len(toks))
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

// Corpus sets TestPhoneticEval scores separately.
const (
	// corpusOriginal is asr_corpus.yaml (English and German) and the per-language suites in testdata/phonetic: the set encoders are chosen and tuned on.
	corpusOriginal = "original"
	// corpusGrown is testdata/grown: held-out suites in every language. Cases describe what a listener hears; don't change one to suit an encoder (see docs/src/content/docs/contributing/dependencies.md).
	corpusGrown = "grown"
)

// evalSuites gathers one corpus set's suites, grouped by language.
func evalSuites(t *testing.T, set string) map[string][]corpusSuite {
	t.Helper()

	var c corpus

	switch set {
	case corpusOriginal:
		paths, err := filepath.Glob("testdata/phonetic/*.yaml")
		require.NoError(t, err)

		c = loadCorpusFiles(t, append([]string{"testdata/asr_corpus.yaml"}, paths...)...)
	case corpusGrown:
		c = loadGrownCorpus(t)
	default:
		require.Failf(t, "unknown corpus set", "%q", set)
	}

	out := map[string][]corpusSuite{}

	for _, s := range c.Suites {
		out[s.Lang] = append(out[s.Lang], s)
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
				ask:   res.Decision == DecisionAsk,
				why:   why,
			})
		}

		require.NoError(t, ix.Close())
	}

	return out
}

// evalScore is a language's tally under one encoder and retriever.
type evalScore struct {
	correct, total, wrong, asks int
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

		if o.ask {
			s.asks++
		}
	}

	return s
}

func (s evalScore) String() string {
	return fmt.Sprintf("%3d/%3d (%5.1f%%) wrong plays %2d asks %3d", s.correct, s.total, 100*float64(s.correct)/float64(s.total), s.wrong, s.asks)
}

// discordant counts the cases out decides correctly and ref does not (gains), and the reverse (losses): the pairs a paired comparison of two encoders rests on.
func discordant(out, ref []evalOutcome) (gains, losses int) {
	for i, o := range out {
		switch {
		case o.ok && !ref[i].ok:
			gains++
		case !o.ok && ref[i].ok:
			losses++
		}
	}

	return gains, losses
}

// fuzzyGate is how many more correct decisions than English Double Metaphone internal/fuzzy must make per language on the original corpora, and production on both sets where it has its own encoder (ownEncoder): the French and Spanish encoders earn their place only with a measurable gain.
var fuzzyGate = map[string]int{"fr": 3, "es": 3}

// ownEncoder lists the gated languages production keys with their own encoder. Production is held to the same gate there, on both corpus sets, so a language that lost its encoder would fail.
var ownEncoder = map[string]bool{"es": true, "fr": true}

// grownFloor is what production must still do on each language's grown corpus, under either retriever: decide at least correct cases right and make at most wrong wrong plays. They are the counts measured when they were set. Raise them when a change gains; lower one only with the evidence that the accuracy tables in docs/src/content/docs/contributing/dependencies.md are updated with.
var grownFloor = map[string]evalScore{
	"de": {correct: 535, wrong: 1},
	"en": {correct: 463, wrong: 1},
	"es": {correct: 418, wrong: 2},
	"fr": {correct: 408, wrong: 2},
	"it": {correct: 343, wrong: 1},
}

// TestPhoneticEval measures match accuracy per corpus set and language through the real pipeline (normalise, alias, retrieve, score, decide) with each candidate phonetic encoder swapped in. It logs a table with each encoder's gains and losses against English Double Metaphone, the production encoder's misses and every case another encoder gains or loses against it; run it with -v to read them. On the grown corpora production is held to grownFloor. On both sets, production in the languages listed in ownEncoder is held to fuzzyGate against English Double Metaphone with no wrong plays added. On the original corpora internal/fuzzy is held to fuzzyGate too, and must not lose a decision or add a wrong play against production; on the grown ones it is only reported, as German production adds a rule to it there (germanKeys).
func TestPhoneticEval(t *testing.T) {
	var table strings.Builder

	for _, set := range []string{corpusOriginal, corpusGrown} {
		suites := evalSuites(t, set)

		for _, retr := range []string{RetrieverInverted, RetrieverTrigram} {
			for _, code := range slices.Sorted(maps.Keys(suites)) {
				evalLanguage(t, &table, set, retr, code, suites[code])
			}
		}
	}

	t.Logf("phonetic encoder evaluation:\n%s", table.String())
}

// evalLanguage runs every encoder over one language's suites in one corpus set, writes their rows to table and applies the gates.
func evalLanguage(t *testing.T, table *strings.Builder, set, retr, code string, suites []corpusSuite) {
	t.Helper()

	outs := map[string][]evalOutcome{}

	var names []string

	for _, enc := range evalEncoders {
		fn := enc.keys(code)
		if fn == nil && enc.name != "current" {
			continue
		}

		t.Run(set+"/"+retr+"/"+code+"/"+enc.name, func(t *testing.T) {
			if fn != nil {
				withPhonetic(t, code, fn)
			}

			outs[enc.name] = runEval(t, suites, retr)
		})

		names = append(names, enc.name)
	}

	base, english := outs["current"], outs["english"]
	require.NotEmpty(t, base, "no current run for %s", code)

	for _, name := range names {
		gains, losses := discordant(outs[name], english)
		fmt.Fprintf(table, "%-8s %-8s %-3s %-8s %s  vs english +%d -%d\n", set, retr, code, name, tally(outs[name]), gains, losses)

		if name == "current" {
			for _, o := range base {
				if !o.ok {
					t.Logf("%s %s %s current misses [%s] %q: %s", set, retr, code, o.suite, o.heard, o.why)
				}
			}

			continue
		}

		for i, o := range outs[name] {
			switch {
			case o.ok && !base[i].ok:
				t.Logf("%s %s %s %s gains [%s] %q (current: %s)", set, retr, code, name, o.suite, o.heard, base[i].why)
			case !o.ok && base[i].ok:
				t.Logf("%s %s %s %s loses [%s] %q: %s", set, retr, code, name, o.suite, o.heard, o.why)
			}
		}
	}

	got, eng := tally(base), tally(english)

	if set == corpusGrown {
		floor, ok := grownFloor[code]
		require.True(t, ok, "no grown floor for %s", code)
		assert.GreaterOrEqual(t, got.correct, floor.correct, "%s %s %s: production must decide at least %d cases right", set, retr, code, floor.correct)
		assert.LessOrEqual(t, got.wrong, floor.wrong, "%s %s %s: production must make at most %d wrong plays", set, retr, code, floor.wrong)
	}

	if ownEncoder[code] {
		assert.GreaterOrEqual(t, got.correct, eng.correct+fuzzyGate[code], "%s %s %s: production must gain at least %d correct decisions over english", set, retr, code, fuzzyGate[code])
		assert.LessOrEqual(t, got.wrong, eng.wrong, "%s %s %s: production must not add wrong plays over english", set, retr, code)
	}

	if set != corpusOriginal {
		return
	}

	fz := tally(outs["fuzzy"])
	assert.GreaterOrEqual(t, fz.correct, got.correct, "%s %s: fuzzy must not lose correct decisions against production", retr, code)
	assert.LessOrEqual(t, fz.wrong, got.wrong, "%s %s: fuzzy must not add wrong plays against production", retr, code)
	assert.GreaterOrEqual(t, fz.correct, eng.correct+fuzzyGate[code], "%s %s: fuzzy must gain at least %d correct decisions over english", retr, code, fuzzyGate[code])
	assert.LessOrEqual(t, fz.wrong, eng.wrong, "%s %s: fuzzy must not add wrong plays over english", retr, code)
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
