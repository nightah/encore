package retrieval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/custom"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/keyword"
	"github.com/blevesearch/bleve/v2/analysis/token/ngram"
	"github.com/blevesearch/bleve/v2/analysis/tokenizer/unicode"
	"github.com/blevesearch/bleve/v2/index/scorch"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/registry"
	"github.com/blevesearch/bleve/v2/search/query"

	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/core/media"
)

// The bleve retriever below has the mapping, analyzers, batching and query Encore used with bleve, behind match.Retriever. bleveDoc indexes each match.RetrievalDoc, and wordClauses works out the query's terms, fuzziness and phonetic keys.

var (
	filtersMu sync.Mutex
	filters   = map[string]bool{}
)

// NewBleve returns a factory for match.NewWithRetriever that retrieves candidates with bleve, set up as Encore used it. lang names the phonetic token filter registered for the language, since bleve's registry is global.
func NewBleve(lang string) func(phonetic func(string) []string) (match.Retriever, error) {
	return func(phonetic func(string) []string) (match.Retriever, error) {
		name := "encore_phonetic_" + lang

		filtersMu.Lock()
		if !filters[name] {
			err := registry.RegisterTokenFilter(name, func(map[string]any, *registry.Cache) (analysis.TokenFilter, error) {
				return phoneticFilter{fn: phonetic}, nil
			})
			if err != nil {
				filtersMu.Unlock()

				return nil, fmt.Errorf("registering phonetic filter: %w", err)
			}

			filters[name] = true
		}
		filtersMu.Unlock()

		m, err := bleveMapping(name)
		if err != nil {
			return nil, err
		}

		// An empty path with scorch keeps segments in memory at a fraction of NewMemOnly's footprint.
		idx, err := bleve.NewUsing("", m, scorch.Name, scorch.Name, nil)
		if err != nil {
			return nil, fmt.Errorf("creating bleve index: %w", err)
		}

		return &Bleve{idx: idx}, nil
	}
}

// Filter replaces each token with its phonetic keys at the same position.
func (f phoneticFilter) Filter(in analysis.TokenStream) analysis.TokenStream {
	out := make(analysis.TokenStream, 0, len(in))

	for _, t := range in {
		for _, k := range f.fn(string(t.Term)) {
			tok := *t
			tok.Term = []byte(k)
			out = append(out, &tok)
		}
	}

	return out
}

func bleveMapping(phoneticFilter string) (*mapping.IndexMappingImpl, error) {
	m := bleve.NewIndexMapping()
	m.StoreDynamic = false
	m.IndexDynamic = false
	m.DocValuesDynamic = false

	if err := m.AddCustomTokenFilter("encore_ngram3", map[string]any{"type": ngram.Name, "min": 3.0, "max": 3.0}); err != nil {
		return nil, fmt.Errorf("ngram filter: %w", err)
	}

	analyzers := map[string][]string{
		"encore_norm":  nil,
		"encore_ngram": {"encore_ngram3"},
		"encore_phon":  {phoneticFilter},
	}

	for name, filters := range analyzers {
		cfg := map[string]any{"type": custom.Name, "tokenizer": unicode.Name}
		if filters != nil {
			cfg["token_filters"] = filters
		}

		if err := m.AddCustomAnalyzer(name, cfg); err != nil {
			return nil, fmt.Errorf("analyzer %s: %w", name, err)
		}
	}

	text := func(analyzer string) *mapping.FieldMapping {
		f := bleve.NewTextFieldMapping()
		f.Analyzer = analyzer
		f.Store = false
		f.IncludeTermVectors = false
		f.IncludeInAll = false
		f.DocValues = false

		return f
	}

	kind := bleve.NewKeywordFieldMapping()
	kind.Analyzer = keyword.Name
	kind.Store = false
	kind.IncludeTermVectors = false
	kind.IncludeInAll = false
	kind.DocValues = false

	dm := bleve.NewDocumentStaticMapping()
	dm.AddFieldMappingsAt(fieldNorm, text("encore_norm"))
	dm.AddFieldMappingsAt(fieldNgram, text("encore_ngram"))
	dm.AddFieldMappingsAt(fieldPhon, text("encore_phon"))
	dm.AddFieldMappingsAt(fieldKind, kind)

	m.DefaultMapping = dm

	return m, nil
}

// Upsert indexes docs in batches.
func (r *Bleve) Upsert(docs []match.RetrievalDoc) error {
	b := r.idx.NewBatch()

	for _, d := range docs {
		if err := b.Index(d.ID, bleveDoc(d)); err != nil {
			return fmt.Errorf("indexing %q: %w", d.ID, err)
		}

		if b.Size() >= batchSize {
			if err := r.idx.Batch(b); err != nil {
				return fmt.Errorf("indexing batch: %w", err)
			}

			b.Reset()
		}
	}

	if b.Size() > 0 {
		if err := r.idx.Batch(b); err != nil {
			return fmt.Errorf("indexing batch: %w", err)
		}
	}

	return nil
}

// bleveDoc puts the words in the word and phonetic fields, and each alias with spaces removed in the trigram and phonetic fields. Term frequencies don't matter because hits are re-scored.
func bleveDoc(d match.RetrievalDoc) map[string]any {
	return map[string]any{
		fieldNorm:  strings.Join(d.Words, " "),
		fieldNgram: d.Compacts,
		fieldPhon:  append(append([]string(nil), d.Words...), d.Compacts...),
		fieldKind:  string(d.Kind),
	}
}

// Remove deletes Docs by ID.
func (r *Bleve) Remove(ids []string) error {
	b := r.idx.NewBatch()
	for _, id := range ids {
		b.Delete(id)
	}

	if err := r.idx.Batch(b); err != nil {
		return fmt.Errorf("deleting: %w", err)
	}

	return nil
}

// Candidates runs a word and phonetic query first. Trigram postings are long, so the trigram clause is only added when that finds too little.
func (r *Bleve) Candidates(ctx context.Context, q match.RetrievalQuery, kinds []media.Kind, k int) ([]string, error) {
	clauses := wordClauses(q)

	ids, err := r.search(ctx, clauses, kinds, k)
	if err != nil || len(ids) >= min(k, ngramFallbackHits) {
		return ids, err
	}

	nq := bleve.NewMatchQuery(q.Compact)
	nq.SetField(fieldNgram)
	nq.SetBoost(boostNgram)

	return r.search(ctx, append(clauses, nq), kinds, k)
}

// wordClauses matches words fuzzily and phonetic keys exactly.
func wordClauses(q match.RetrievalQuery) []query.Query {
	clauses := make([]query.Query, 0, len(q.Terms)+len(q.Keys))

	for _, t := range q.Terms {
		if t.Fuzziness > 0 {
			fq := bleve.NewFuzzyQuery(t.Text)
			fq.SetField(fieldNorm)
			fq.SetFuzziness(t.Fuzziness)
			fq.SetPrefix(fuzzyPrefix)
			fq.SetBoost(boostNorm)
			clauses = append(clauses, fq)

			continue
		}

		tq := bleve.NewTermQuery(t.Text)
		tq.SetField(fieldNorm)
		tq.SetBoost(boostNorm)
		clauses = append(clauses, tq)
	}

	for _, k := range q.Keys {
		tq := bleve.NewTermQuery(k)
		tq.SetField(fieldPhon)
		tq.SetBoost(boostPhon)
		clauses = append(clauses, tq)
	}

	return clauses
}

func (r *Bleve) search(ctx context.Context, clauses []query.Query, kinds []media.Kind, k int) ([]string, error) {
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

	res, err := r.idx.SearchInContext(ctx, bleve.NewSearchRequestOptions(root, k, 0, false))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
			return nil, ctxErr
		}

		return nil, fmt.Errorf("searching: %w", err)
	}

	ids := make([]string, len(res.Hits))
	for i, h := range res.Hits {
		ids[i] = h.ID
	}

	return ids, nil
}

// Close closes the bleve index.
func (r *Bleve) Close() error {
	if err := r.idx.Close(); err != nil {
		return fmt.Errorf("closing bleve index: %w", err)
	}

	return nil
}
