package match

import (
	"context"
	"slices"

	"github.com/nightah/encore/internal/core/media"
)

// This file is a seam for the benchmarks module (benchmarks/ at the repository root), which compares the in-tree code with external libraries. Encore itself never calls anything here. It adds no behaviour: each function wraps what the package already does, so code outside the package can measure and check it.

// NewWithRetriever is New with candidate retrieval supplied by newRetriever, which is given the language's phonetic encoder. Everything else, from normalising to scoring and deciding, is the Index's own, so two retrievers can be compared through the same Search.
func NewWithRetriever(lang string, cfg Config, newRetriever func(phonetic func(word string) []string) (Retriever, error)) (*Index, error) {
	ix, err := New(lang, cfg)
	if err != nil {
		return nil, err
	}

	r, err := newRetriever(ix.lang.phonetic)
	if err != nil {
		return nil, err
	}

	ix.ret = &externalRetriever{lang: ix.lang, r: r}

	return ix, nil
}

// Candidates returns the Doc IDs retrieval hands the scorer for q, best first, without scoring them. Exact matches that Search adds itself are not included.
func (ix *Index) Candidates(ctx context.Context, q Query) ([]string, error) {
	qf := ix.lang.newForm(ix.lang.normalizeSpelled(q.Text, false))
	if len(qf.toks) == 0 {
		return nil, nil
	}

	if ix.isClosed() {
		return nil, ErrClosed
	}

	return ix.ret.candidates(ctx, &qf, q.Kinds, ix.cfg.CandidatePool)
}

// JaroWinkler is the Jaro-Winkler similarity the scorer uses, in [0, 1].
func JaroWinkler(a, b string) float64 { return jaroWinkler(a, b) }

// OSADistance is the optimal string alignment distance the scorer uses.
func OSADistance(a, b string) int { return osaDistance(a, b) }

func (r *externalRetriever) upsert(entries []*entry) error {
	docs := make([]RetrievalDoc, len(entries))

	for i, e := range entries {
		d := RetrievalDoc{ID: e.doc.ID, Kind: e.doc.Kind}

		for j := range e.aliases {
			a := &e.aliases[j]
			for _, t := range a.toks {
				if !slices.Contains(d.Words, t) {
					d.Words = append(d.Words, t)
				}
			}

			if !slices.Contains(d.Compacts, a.compact) {
				d.Compacts = append(d.Compacts, a.compact)
			}
		}

		docs[i] = d
	}

	return r.r.Upsert(docs)
}

func (r *externalRetriever) remove(ids []string) error { return r.r.Remove(ids) }

func (r *externalRetriever) candidates(ctx context.Context, q *form, kinds []media.Kind, k int) ([]string, error) {
	terms := r.lang.queryTokens(q)
	rq := RetrievalQuery{Keys: queryKeys(q, terms), Compact: q.compact}

	for _, t := range terms {
		rq.Terms = append(rq.Terms, RetrievalTerm{Text: t, Fuzziness: fuzziness(t)})
	}

	return r.r.Candidates(ctx, rq, kinds, k)
}

func (r *externalRetriever) close() error { return r.r.Close() }
