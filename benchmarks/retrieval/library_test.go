package retrieval

import (
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// The libraries and queries below are copies of the generators in internal/core/match's bench_test.go (syntheticDocs) and scale_test.go (scaleDocs, scaleQueries), which are test code of that package and can't be imported. They are deterministic for a seed, so the numbers here and there are measured over the same titles.

// corpusPath is the ASR corpus the matcher's own tests use: libraries of real titles and what speech recognition heard for them.
const corpusPath = "../../internal/core/match/testdata/asr_corpus.yaml"

type corpus struct {
	Suites []corpusSuite `yaml:"suites"`
}

type corpusSuite struct {
	Name    string `yaml:"name"`
	Lang    string `yaml:"lang"`
	Library []struct {
		ID      string   `yaml:"id"`
		Kind    string   `yaml:"kind"`
		Title   string   `yaml:"title"`
		Aliases []string `yaml:"aliases"`
		Year    int      `yaml:"year"`
	} `yaml:"library"`
	Cases []struct {
		Heard string   `yaml:"heard"`
		Kinds []string `yaml:"kinds"`
	} `yaml:"cases"`
}

func loadCorpus(tb testing.TB) corpus {
	tb.Helper()

	raw, err := os.ReadFile(corpusPath)
	require.NoError(tb, err)

	var c corpus
	require.NoError(tb, yaml.Unmarshal(raw, &c))
	require.NotEmpty(tb, c.Suites)

	return c
}

func englishSuite(tb testing.TB) corpusSuite {
	tb.Helper()

	for _, s := range loadCorpus(tb).Suites {
		if s.Lang == "en" {
			return s
		}
	}

	tb.Fatal("no English suite")

	return corpusSuite{}
}

func (s corpusSuite) docs() []match.Doc {
	docs := make([]match.Doc, len(s.Library))
	for i, d := range s.Library {
		docs[i] = match.Doc{ID: d.ID, Kind: media.Kind(d.Kind), Ref: media.Ref{Kind: media.Kind(d.Kind), ID: i + 1}, Title: d.Title, Aliases: d.Aliases, Year: d.Year}
	}

	return docs
}

func (s corpusSuite) queries() []match.Query {
	out := make([]match.Query, len(s.Cases))
	for i, c := range s.Cases {
		out[i] = match.Query{Text: c.Heard}
		for _, k := range c.Kinds {
			out[i].Kinds = append(out[i].Kinds, media.Kind(k))
		}
	}

	return out
}

var benchSyllables = []string{
	"ka", "lo", "ver", "sein", "feld", "ro", "cky", "star", "war", "night", "ma", "tri", "x", "ban", "dit", "to", "ry",
	"gel", "dark", "ness", "lord", "ring", "sil", "ence", "lamb", "god", "fa", "thur", "mo", "nster", "in", "ter", "stel", "lar",
}

// syntheticDocs is BenchmarkSearch100k's library: titles of one to five words from an 8000-word vocabulary of made-up words.
func syntheticDocs(n int, seed uint64) []match.Doc {
	r := rand.New(rand.NewPCG(seed, seed))

	vocab := make([]string, 8000)
	for i := range vocab {
		var b strings.Builder
		for range 1 + r.IntN(3) {
			b.WriteString(benchSyllables[r.IntN(len(benchSyllables))])
		}

		vocab[i] = b.String()
	}

	kinds := []media.Kind{media.KindMovie, media.KindTVShow, media.KindArtist, media.KindAlbum}
	docs := make([]match.Doc, n)

	for i := range docs {
		w := make([]string, 1+r.IntN(5))
		for j := range w {
			w[j] = vocab[r.IntN(len(vocab))]
		}

		if r.IntN(10) == 0 {
			w = append([]string{"the"}, w...)
		}

		k := kinds[r.IntN(len(kinds))]
		docs[i] = match.Doc{ID: fmt.Sprintf("bench/%s/%d", k, i), Kind: k, Ref: media.Ref{Kind: k, ID: i}, Title: strings.Join(w, " "), Year: 1950 + r.IntN(75)}
	}

	return docs
}

// syntheticQueries is BenchmarkSearch100k's queries: a known mishearing, then exact titles, one-letter typos and first words.
func syntheticQueries(docs []match.Doc) []match.Query {
	r := rand.New(rand.NewPCG(2, 2))
	queries := []match.Query{{Text: "seinfield"}}

	for range 200 {
		t := docs[r.IntN(len(docs))].Title
		switch r.IntN(3) {
		case 0:
			queries = append(queries, match.Query{Text: t})
		case 1:
			rs := []rune(t)
			i := r.IntN(len(rs))
			rs[i] = 'a' + rune(r.IntN(26))
			queries = append(queries, match.Query{Text: string(rs)})
		default:
			queries = append(queries, match.Query{Text: strings.Fields(t)[0]})
		}
	}

	return queries
}

// scaleWords are common title words, drawn with a Zipf distribution so that some postings are very long, as in real libraries.
var scaleWords = strings.Fields(`the of a and in to love night man day life world girl time my you me i is it on your house dark last king blue home little heart city
dead black red one two three story new war lost all with from for at by be we no star part live big good dream fire light sun moon water death
blood shadow ghost game boy song back way out up down long road world queen lady lord mister doctor return rise fall secret return land sea sky
river mountain summer winter spring autumn christmas american great best end beginning first second final edge kill killer hunter run stay
under over beyond after before inside outside without forever never always again tonight tomorrow yesterday golden silver iron steel stone
wild free lonely happy sad crazy wonderful beautiful sweet bitter hot cold fast slow high low deep hidden open broken perfect true false real
angel devil heaven hell paradise kingdom empire legend hero monster dragon wolf tiger bird horse dog cat baby child mother father brother sister
family friends lovers stranger soldier rebel prince princess warrior spirit soul mind body eyes face hand voice words music dance party rock
blues soul jazz radio movie show live tour volume chapter episode season edition remastered deluxe acoustic version remix mix original theme`)

// scaleSyllables build name-like words, so the vocabulary also has a long tail of rare terms.
var scaleSyllables = []string{
	"ka", "lo", "ver", "sein", "feld", "ro", "cky", "star", "war", "night", "ma", "tri", "x", "ban", "dit", "to", "ry", "gel", "dark", "ness", "lord",
	"ring", "sil", "ence", "lamb", "god", "fa", "thur", "mo", "nster", "in", "ter", "stel", "lar", "be", "ach", "ton", "son", "ber", "gen", "man",
	"el", "ley", "zo", "qui", "phi", "chel", "ny", "ash", "ford", "wick", "ham", "dra", "ken", "jo", "vi", "an", "ca", "mi", "la", "ri", "us", "ia",
}

var scaleKinds = []struct {
	kind   media.Kind
	weight int
}{
	{media.KindSong, 50}, {media.KindAlbum, 14}, {media.KindArtist, 8}, {media.KindEpisode, 12}, {media.KindMovie, 10}, {media.KindTVShow, 4}, {media.KindMusicVideo, 2},
}

// scaleDocs returns n Docs: the English ASR corpus library plus synthetic titles, deterministic for a seed.
func scaleDocs(n int, lib []match.Doc, seed uint64) []match.Doc {
	r := rand.New(rand.NewPCG(seed, seed^0x5ca1e))

	vocab := make([]string, 40_000)
	for i := range vocab {
		var b strings.Builder
		for range 2 + r.IntN(3) {
			b.WriteString(scaleSyllables[r.IntN(len(scaleSyllables))])
		}

		vocab[i] = b.String()
	}

	common := rand.NewZipf(r, 1.1, 2, uint64(len(scaleWords)-1))
	rare := rand.NewZipf(r, 1.05, 4, uint64(len(vocab)-1))

	word := func() string {
		if r.IntN(100) < 45 {
			return scaleWords[common.Uint64()]
		}

		return vocab[rare.Uint64()]
	}

	title := func() string {
		w := make([]string, 1+r.IntN(r.IntN(6)+1))
		for i := range w {
			w[i] = word()
		}

		return strings.Join(w, " ")
	}

	total := 0
	for _, k := range scaleKinds {
		total += k.weight
	}

	docs := slices.Clone(lib)
	for i := len(docs); i < n; i++ {
		pick, kind := r.IntN(total), scaleKinds[0].kind
		for _, k := range scaleKinds {
			if pick < k.weight {
				kind = k.kind

				break
			}

			pick -= k.weight
		}

		d := match.Doc{ID: fmt.Sprintf("scale/%s/%d", kind, i), Kind: kind, Ref: media.Ref{Kind: kind, ID: i}, Title: title(), Year: 1950 + r.IntN(75)}
		if r.IntN(20) == 0 {
			d.Aliases = []string{title()}
		}

		docs = append(docs, d)
	}

	return docs
}

// scaleMishearings rewrite one word the way speech recognition tends to get it wrong.
var scaleMishearings = [][2]string{
	{"ph", "f"}, {"ck", "k"}, {"c", "k"}, {"ee", "ea"}, {"s", "z"}, {"ou", "ow"}, {"y", "ie"}, {"er", "a"}, {"th", "f"}, {"ll", "l"}, {"x", "ks"}, {"qu", "kw"}, {"i", "ee"},
}

// scaleQueries derives n queries from known Docs, each by one kind of mishearing.
func scaleQueries(docs []match.Doc, n int, seed uint64) []match.Query {
	r := rand.New(rand.NewPCG(seed, seed^0xa5))
	kinds := []string{"exact", "typo", "first-letter", "sound-alike", "merged", "dropped-word", "first-word"}
	out := make([]match.Query, 0, n)

	for len(out) < n {
		d := docs[r.IntN(len(docs))]
		w := strings.Fields(strings.ToLower(d.Title))
		kind := kinds[len(out)%len(kinds)]
		i := r.IntN(len(w))

		switch kind {
		case "typo", "first-letter":
			rs := []rune(w[i])
			if len(rs) < 4 {
				continue
			}

			at := 1 + r.IntN(len(rs)-1)
			if kind == "first-letter" {
				at = 0
			}

			rs[at] = 'a' + rune(r.IntN(26))
			w[i] = string(rs)
		case "sound-alike":
			hit := false

			for _, j := range r.Perm(len(scaleMishearings)) {
				if m := scaleMishearings[j]; strings.Contains(w[i], m[0]) {
					w[i], hit = strings.Replace(w[i], m[0], m[1], 1), true

					break
				}
			}

			if !hit {
				continue
			}
		case "merged":
			if len(w) < 2 {
				continue
			}

			i = r.IntN(len(w) - 1)
			w = slices.Concat(w[:i], []string{w[i] + w[i+1]}, w[i+2:])
		case "dropped-word":
			if len(w) < 3 {
				continue
			}

			i = 1 + r.IntN(len(w)-1)
			w = slices.Delete(w, i, i+1)
		case "first-word":
			if len(w) < 2 {
				continue
			}

			w = w[:1]
		}

		out = append(out, match.Query{Text: strings.Join(w, " ")})
	}

	return out
}
