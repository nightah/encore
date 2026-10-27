package match

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

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

	"github.com/nightah/encore/internal/core/media"
)

func phoneticFilterName(code string) string { return "encore_phonetic_" + code }

func init() {
	for code, l := range languages {
		err := registry.RegisterTokenFilter(phoneticFilterName(code), func(map[string]any, *registry.Cache) (analysis.TokenFilter, error) {
			return phoneticFilter{fn: l.phonetic}, nil
		})
		if err != nil {
			panic(fmt.Sprintf("match: registering phonetic filter: %v", err))
		}
	}
}

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

func newBleveRetriever(l *language) (*bleveRetriever, error) {
	m, err := bleveMapping(l)
	if err != nil {
		return nil, err
	}

	// An empty path with scorch keeps segments in memory at a fraction of NewMemOnly's footprint.
	idx, err := bleve.NewUsing("", m, scorch.Name, scorch.Name, nil)
	if err != nil {
		return nil, fmt.Errorf("match: creating bleve index: %w", err)
	}

	return &bleveRetriever{lang: l, idx: idx}, nil
}

func bleveMapping(l *language) (*mapping.IndexMappingImpl, error) {
	m := bleve.NewIndexMapping()
	m.StoreDynamic = false
	m.IndexDynamic = false
	m.DocValuesDynamic = false

	if err := m.AddCustomTokenFilter("encore_ngram3", map[string]any{"type": ngram.Name, "min": 3.0, "max": 3.0}); err != nil {
		return nil, fmt.Errorf("match: ngram filter: %w", err)
	}

	analyzers := map[string][]string{
		"encore_norm":  nil,
		"encore_ngram": {"encore_ngram3"},
		"encore_phon":  {phoneticFilterName(l.code)},
	}

	for name, filters := range analyzers {
		cfg := map[string]any{"type": custom.Name, "tokenizer": unicode.Name}
		if filters != nil {
			cfg["token_filters"] = filters
		}

		if err := m.AddCustomAnalyzer(name, cfg); err != nil {
			return nil, fmt.Errorf("match: analyzer %s: %w", name, err)
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

func (r *bleveRetriever) upsert(entries []*entry) error {
	b := r.idx.NewBatch()

	for _, e := range entries {
		if err := b.Index(e.doc.ID, bleveDoc(e)); err != nil {
			return fmt.Errorf("match: indexing %q: %w", e.doc.ID, err)
		}

		if b.Size() >= batchSize {
			if err := r.idx.Batch(b); err != nil {
				return fmt.Errorf("match: indexing batch: %w", err)
			}

			b.Reset()
		}
	}

	if b.Size() > 0 {
		if err := r.idx.Batch(b); err != nil {
			return fmt.Errorf("match: indexing batch: %w", err)
		}
	}

	return nil
}

// bleveDoc flattens an entry's aliases: distinct words for the word and phonetic fields, plus each alias with spaces removed for trigrams and whole-string phonetics. Term frequencies don't matter because hits are re-scored.
func bleveDoc(e *entry) map[string]any {
	var words, compacts []string

	for i := range e.aliases {
		a := &e.aliases[i]
		for _, t := range a.toks {
			if !slices.Contains(words, t) {
				words = append(words, t)
			}
		}

		if !slices.Contains(compacts, a.compact) {
			compacts = append(compacts, a.compact)
		}
	}

	return map[string]any{
		fieldNorm:  strings.Join(words, " "),
		fieldNgram: compacts,
		fieldPhon:  append(words, compacts...),
		fieldKind:  string(e.doc.Kind),
	}
}

func (r *bleveRetriever) remove(ids []string) error {
	b := r.idx.NewBatch()
	for _, id := range ids {
		b.Delete(id)
	}

	if err := r.idx.Batch(b); err != nil {
		return fmt.Errorf("match: deleting: %w", err)
	}

	return nil
}

// candidates runs a word and phonetic query first. Trigram postings are long, so the trigram clause is only added when that finds too little.
func (r *bleveRetriever) candidates(ctx context.Context, q *form, kinds []media.Kind, k int) ([]string, error) {
	clauses := r.wordClauses(q)

	ids, err := r.search(ctx, clauses, kinds, k)
	if err != nil || len(ids) >= min(k, ngramFallbackHits) {
		return ids, err
	}

	nq := bleve.NewMatchQuery(q.compact)
	nq.SetField(fieldNgram)
	nq.SetBoost(boostNgram)

	return r.search(ctx, append(clauses, nq), kinds, k)
}

// wordClauses matches words fuzzily and phonetic keys exactly.
func (r *bleveRetriever) wordClauses(q *form) []query.Query {
	terms := r.lang.queryTokens(q)
	clauses := make([]query.Query, 0, 2*len(terms)+1)
	keys := make([]string, 0, len(terms)+1)

	for i, t := range q.toks {
		if !slices.Contains(terms, t) {
			continue
		}

		for _, k := range q.keys[i] {
			if !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
	}

	for _, t := range terms {
		if fz := fuzziness(t); fz > 0 {
			fq := bleve.NewFuzzyQuery(t)
			fq.SetField(fieldNorm)
			fq.SetFuzziness(fz)
			fq.SetPrefix(fuzzyPrefix)
			fq.SetBoost(boostNorm)
			clauses = append(clauses, fq)

			continue
		}

		tq := bleve.NewTermQuery(t)
		tq.SetField(fieldNorm)
		tq.SetBoost(boostNorm)
		clauses = append(clauses, tq)
	}

	for _, k := range q.compactKeys {
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}

	for _, k := range keys {
		tq := bleve.NewTermQuery(k)
		tq.SetField(fieldPhon)
		tq.SetBoost(boostPhon)
		clauses = append(clauses, tq)
	}

	return clauses
}

func (r *bleveRetriever) search(ctx context.Context, clauses []query.Query, kinds []media.Kind, k int) ([]string, error) {
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

		return nil, fmt.Errorf("match: searching: %w", err)
	}

	ids := make([]string, len(res.Hits))
	for i, h := range res.Hits {
		ids[i] = h.ID
	}

	return ids, nil
}

func (r *bleveRetriever) close() error {
	if err := r.idx.Close(); err != nil {
		return fmt.Errorf("match: closing bleve index: %w", err)
	}

	return nil
}

// fuzziness allows more edits in longer words; short words and numbers must match exactly.
func fuzziness(t string) int {
	switch n := utf8.RuneCountInString(t); {
	case isDigits(t) || n <= 3:
		return 0
	case n <= 6:
		return 1
	default:
		return 2
	}
}
