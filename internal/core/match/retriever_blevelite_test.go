//go:build retrieverbench

package match

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/blevesearch/bleve/v2/analysis"
	"github.com/blevesearch/bleve/v2/document"
	"github.com/blevesearch/bleve/v2/index/scorch"
	"github.com/blevesearch/bleve/v2/search"
	"github.com/blevesearch/bleve/v2/search/collector"
	"github.com/blevesearch/bleve/v2/search/searcher"
	index "github.com/blevesearch/bleve_index_api"

	"github.com/nightah/encore/internal/core/media"
)

// bleveLiteRetriever runs the bleve retriever's query on scorch directly, without the top-level bleve package, its mapping, query parser or analyzers. Tokens come from the entry's forms, so no analysis packages are linked. It exists to measure how much of bleve's binary cost is avoidable without forking.
type bleveLiteRetriever struct {
	lang *language
	idx  index.Index
}

// liteTokens is an analyzer that emits fixed tokens: the field value is ignored.
type liteTokens []string

func (t liteTokens) Analyze([]byte) analysis.TokenStream {
	out := make(analysis.TokenStream, len(t))
	for i, s := range t {
		out[i] = &analysis.Token{Term: []byte(s), Position: i + 1, Type: analysis.AlphaNumeric}
	}

	return out
}

func init() {
	scaleRetrievers["bleve-lite"] = func(l *language) (retriever, error) { return newBleveLiteRetriever(l) }
}

func newBleveLiteRetriever(l *language) (*bleveLiteRetriever, error) {
	idx, err := scorch.NewScorch(scorch.Name, map[string]any{"path": ""}, index.NewAnalysisQueue(4))
	if err != nil {
		return nil, err
	}

	if err := idx.Open(); err != nil {
		return nil, err
	}

	return &bleveLiteRetriever{lang: l, idx: idx}, nil
}

func liteField(name string, toks []string) *document.TextField {
	return document.NewTextFieldCustom(name, nil, []byte(strings.Join(toks, " ")), index.IndexField, liteTokens(toks))
}

func (r *bleveLiteRetriever) upsert(entries []*entry) error {
	b := index.NewBatch()

	for i, e := range entries {
		var words, compacts []string

		for j := range e.aliases {
			a := &e.aliases[j]
			for _, t := range a.toks {
				if !slices.Contains(words, t) {
					words = append(words, t)
				}
			}

			if !slices.Contains(compacts, a.compact) {
				compacts = append(compacts, a.compact)
			}
		}

		var phon []string
		for _, s := range append(slices.Clone(words), compacts...) {
			phon = append(phon, r.lang.phonetic(s)...)
		}

		var grams []string
		for _, c := range compacts {
			grams = trigramsOf(c, grams)
		}

		doc := document.NewDocument(e.doc.ID)
		doc.AddField(liteField(fieldNorm, words))
		doc.AddField(liteField(fieldNgram, grams))
		doc.AddField(liteField(fieldPhon, phon))
		doc.AddField(liteField(fieldKind, []string{string(e.doc.Kind)}))
		b.Update(doc)

		if (i+1)%batchSize == 0 {
			if err := r.idx.Batch(b); err != nil {
				return err
			}

			b = index.NewBatch()
		}
	}

	return r.idx.Batch(b)
}

func (r *bleveLiteRetriever) remove(ids []string) error {
	b := index.NewBatch()
	for _, id := range ids {
		b.Delete(id)
	}

	return r.idx.Batch(b)
}

func (r *bleveLiteRetriever) candidates(ctx context.Context, q *form, kinds []media.Kind, k int) ([]string, error) {
	rd, err := r.idx.Reader()
	if err != nil {
		return nil, err
	}
	defer rd.Close()

	clauses, err := r.wordClauses(ctx, rd, q)
	if err != nil {
		return nil, err
	}

	ids, err := r.search(ctx, rd, clauses, kinds, k)
	if err != nil || len(ids) >= min(k, ngramFallbackHits) {
		return ids, err
	}

	// Searchers are spent once collected, so the word clauses are rebuilt for the second pass.
	clauses, err = r.wordClauses(ctx, rd, q)
	if err != nil {
		return nil, err
	}

	var grams []search.Searcher

	for _, g := range trigramsOf(q.compact, nil) {
		s, err := searcher.NewTermSearcher(ctx, rd, g, fieldNgram, boostNgram, search.SearcherOptions{})
		if err != nil {
			return nil, err
		}

		grams = append(grams, s)
	}

	if len(grams) > 0 {
		d, err := searcher.NewDisjunctionSearcher(ctx, rd, grams, 0, search.SearcherOptions{})
		if err != nil {
			return nil, err
		}

		clauses = append(clauses, d)
	}

	return r.search(ctx, rd, clauses, kinds, k)
}

func (r *bleveLiteRetriever) wordClauses(ctx context.Context, rd index.IndexReader, q *form) ([]search.Searcher, error) {
	terms := r.lang.queryTokens(q)
	keys := make([]string, 0, len(terms)+1)

	for i, t := range q.toks {
		if slices.Contains(terms, t) {
			for _, k := range q.keys[i] {
				if !slices.Contains(keys, k) {
					keys = append(keys, k)
				}
			}
		}
	}

	for _, k := range q.compactKeys {
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}

	opts := search.SearcherOptions{}
	out := make([]search.Searcher, 0, len(terms)+len(keys))

	for _, t := range terms {
		var (
			s   search.Searcher
			err error
		)

		if fz := fuzziness(t); fz > 0 {
			s, err = searcher.NewFuzzySearcher(ctx, rd, t, fuzzyPrefix, fz, fieldNorm, boostNorm, opts)
		} else {
			s, err = searcher.NewTermSearcher(ctx, rd, t, fieldNorm, boostNorm, opts)
		}

		if err != nil {
			return nil, err
		}

		out = append(out, s)
	}

	for _, k := range keys {
		s, err := searcher.NewTermSearcher(ctx, rd, k, fieldPhon, boostPhon, opts)
		if err != nil {
			return nil, err
		}

		out = append(out, s)
	}

	return out, nil
}

func (r *bleveLiteRetriever) search(ctx context.Context, rd index.IndexReader, clauses []search.Searcher, kinds []media.Kind, k int) ([]string, error) {
	opts := search.SearcherOptions{}

	root, err := searcher.NewDisjunctionSearcher(ctx, rd, clauses, 0, opts)
	if err != nil {
		return nil, err
	}

	if len(kinds) > 0 {
		kq := make([]search.Searcher, len(kinds))
		for i, kind := range kinds {
			if kq[i], err = searcher.NewTermSearcher(ctx, rd, string(kind), fieldKind, 1, opts); err != nil {
				return nil, err
			}
		}

		kd, err := searcher.NewDisjunctionSearcher(ctx, rd, kq, 0, opts)
		if err != nil {
			return nil, err
		}

		if root, err = searcher.NewConjunctionSearcher(ctx, rd, []search.Searcher{root, kd}, opts); err != nil {
			return nil, err
		}
	}

	defer root.Close()

	coll := collector.NewTopNCollector(k, 0, search.SortOrder{&search.SortScore{Desc: true}})
	if err := coll.Collect(ctx, root, rd); err != nil {
		return nil, fmt.Errorf("collect: %w", err)
	}

	hits := coll.Results()
	ids := make([]string, len(hits))

	for i, h := range hits {
		if h.ID == "" {
			if h.ID, err = rd.ExternalID(h.IndexInternalID); err != nil {
				return nil, err
			}
		}

		ids[i] = h.ID
	}

	return ids, nil
}

func (r *bleveLiteRetriever) close() error { return r.idx.Close() }
