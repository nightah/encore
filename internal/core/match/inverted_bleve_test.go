package match

import (
	"fmt"
	"slices"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/nightah/encore/internal/core/media"
)

// The differential tests below check that the inverted index retrieves what bleve did: the same candidates in the same order, up to ties, which bleve breaks by its internal document order.

// diffT is what the differential checks need from *testing.T and *rapid.T.
type diffT interface {
	require.TestingT
	Helper()
	Logf(format string, args ...any)
}

// bleveRanking runs the bleve retriever's query, including the trigram fallback for a top k, and returns every hit with its score.
func bleveRanking(tb diffT, r *bleveRetriever, q *form, kinds []media.Kind, k int) ([]scoredID, error) {
	tb.Helper()

	n, err := r.idx.DocCount()
	require.NoError(tb, err)

	clauses := r.wordClauses(q)

	hits, err := bleveScored(tb, r, clauses, kinds, int(n))
	if err != nil || len(hits) >= min(k, ngramFallbackHits) {
		return hits, err
	}

	nq := bleve.NewMatchQuery(q.compact)
	nq.SetField(fieldNgram)
	nq.SetBoost(boostNgram)

	return bleveScored(tb, r, append(clauses, nq), kinds, int(n))
}

// bleveScored is bleveRetriever.search keeping scores.
func bleveScored(tb diffT, r *bleveRetriever, clauses []query.Query, kinds []media.Kind, n int) ([]scoredID, error) {
	tb.Helper()

	var root query.Query = bleve.NewDisjunctionQuery(clauses...)

	if len(kinds) > 0 {
		kq := make([]query.Query, len(kinds))
		for i, kind := range kinds {
			tq := bleve.NewTermQuery(string(kind))
			tq.SetField(fieldKind)
			kq[i] = tq
		}

		root = bleve.NewConjunctionQuery(root, bleve.NewDisjunctionQuery(kq...))
	}

	res, err := r.idx.Search(bleve.NewSearchRequestOptions(root, max(n, 1), 0, false))
	if err != nil {
		return nil, err
	}

	out := make([]scoredID, len(res.Hits))
	for i, h := range res.Hits {
		out[i] = scoredID{h.ID, h.Score}
	}

	return out, nil
}

// bleveTokens says whether bleve's tokenizer keeps s as one token. It splits words in scripts written without spaces into characters, or drops them, which the inverted index deliberately does not do, so libraries and queries with such words are compared in TestInvertedKeepsUnsegmentedScripts instead.
func bleveTokens(tb testing.TB, l *language) func(string) bool {
	tb.Helper()

	m, err := bleveMapping(l)
	require.NoError(tb, err)

	a := m.AnalyzerNamed("encore_norm")
	require.NotNil(tb, a)

	return func(s string) bool {
		toks := a.Analyze([]byte(s))

		return len(toks) == 1 && string(toks[0].Term) == s && toks[0].Type != analysis.Ideographic
	}
}

func formWhole(whole func(string) bool, f *form) bool {
	return !slices.ContainsFunc(f.toks, func(t string) bool { return !whole(t) }) && (f.compact == "" || whole(f.compact))
}

func entriesWhole(whole func(string) bool, entries []*entry) bool {
	for _, e := range entries {
		for i := range e.aliases {
			if !formWhole(whole, &e.aliases[i].form) {
				return false
			}
		}
	}

	return true
}

// diffQuery compares bleve and the inverted index on one query. Queries bleve rejects (a fuzzy word with too many expansions, or one too long for its automaton) return false.
func diffQuery(tb diffT, b *bleveRetriever, inv *invertedRetriever, q *form, kinds []media.Kind, k int, msgAndArgs ...any) bool {
	tb.Helper()

	want, err := bleveRanking(tb, b, q, kinds, k)
	if err != nil {
		tb.Logf("bleve rejects %q: %v", q.norm, err)

		return false
	}

	assertSameRanking(tb, want, invertedRanking(inv, q, kinds, k), k, msgAndArgs...)

	return true
}

// TestInvertedMatchesBleveOnCorpus compares the two on the ASR corpus libraries, with every case's query and with the titles themselves, and on a synthetic library of 10,000 Docs.
func TestInvertedMatchesBleveOnCorpus(t *testing.T) {
	type library struct {
		name, lang string
		docs       []Doc
		queries    []Query
	}

	var libs []library

	for _, s := range loadCorpus(t).Suites {
		lib := library{name: s.Name, lang: s.Lang, docs: s.docs()}

		for _, c := range s.Cases {
			lib.queries = append(lib.queries, c.query())
		}

		for _, d := range lib.docs {
			lib.queries = append(lib.queries, Query{Text: d.Title}, Query{Text: d.Title, Kinds: []media.Kind{d.Kind}})
		}

		libs = append(libs, lib)
	}

	if !testing.Short() {
		docs := scaleDocs(10_000, englishSuite(t).docs(), 1)
		lib := library{name: "synthetic", lang: "en", docs: docs}

		for _, q := range scaleQueries(docs, 700, 2) {
			lib.queries = append(lib.queries, Query{Text: q.text})
		}

		libs = append(libs, lib)
	}

	for _, lib := range libs {
		t.Run(lib.name, func(t *testing.T) {
			l := languageFor(lib.lang)
			whole := bleveTokens(t, l)

			entries := make([]*entry, len(lib.docs))
			for i, d := range lib.docs {
				entries[i] = l.newEntry(d)
			}

			require.True(t, entriesWhole(whole, entries), "the library has words bleve splits")

			b, err := newBleveRetriever(l)
			require.NoError(t, err)
			t.Cleanup(func() { _ = b.close() })
			require.NoError(t, b.upsert(entries))

			inv := newInvertedRetriever(l)
			require.NoError(t, inv.upsert(entries))

			compared := 0

			for _, q := range lib.queries {
				qf := l.newForm(l.normalizeSpelled(q.Text, false))
				if len(qf.toks) == 0 || !formWhole(whole, &qf) {
					continue
				}

				for _, k := range []int{10, DefaultConfig().CandidatePool} {
					if diffQuery(t, b, inv, &qf, q.Kinds, k, q.Text, q.Kinds, k) {
						compared++
					}
				}
			}

			t.Logf("%s: %d Docs, %d query rankings identical", lib.name, len(lib.docs), compared)
		})
	}
}

// TestPropInvertedMatchesBleve compares the two on generated libraries after runs of upserts and deletes. bleve is built from scratch over the Docs left, because its own scores after deletes still count words only deleted Docs had until its segments merge, which happens in the background.
func TestPropInvertedMatchesBleve(t *testing.T) {
	whole := make(map[string]func(string) bool)
	for _, lang := range langs {
		whole[lang] = bleveTokens(t, languageFor(lang))
	}

	rapid.Check(t, func(t *rapid.T) {
		lang := rapid.SampledFrom(langs).Draw(t, "lang")
		lib := genDiffLibrary(t, lang)

		require.True(t, entriesWhole(whole[lang], lib.final), "the library has words bleve splits")

		b, err := newBleveRetriever(lib.lang)
		require.NoError(t, err)

		defer func() { _ = b.close() }()

		require.NoError(t, b.upsert(lib.final))

		for i := range rapid.IntRange(1, 6).Draw(t, "queries") {
			text := genDiffQuery(lib.titles).Draw(t, fmt.Sprint("q", i))

			qf := lib.lang.newForm(lib.lang.normalizeSpelled(text, false))
			if len(qf.toks) == 0 || !formWhole(whole[lang], &qf) {
				continue
			}

			kinds := genDiffKinds().Draw(t, fmt.Sprint("kinds", i))
			k := rapid.SampledFrom([]int{1, 3, 10, 25, 200}).Draw(t, fmt.Sprint("k", i))

			diffQuery(t, b, lib.inv, &qf, kinds, k, text, kinds, k)
		}
	})
}
