package match

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
)

// New returns an empty Index for a language or locale code ("en", "de-DE"). Unknown languages use English rules.
func New(lang string, cfg Config) (*Index, error) {
	cfg = cfg.withDefaults()
	l := languageFor(lang)

	var (
		ret retriever
		err error
	)

	switch cfg.Retriever {
	case RetrieverInverted:
		ret = newInvertedRetriever(l)
	case RetrieverTrigram:
		ret = newTrigramRetriever()
	default:
		err = fmt.Errorf("match: unknown retriever %q", cfg.Retriever)
	}

	if err != nil {
		return nil, err
	}

	return &Index{
		lang:  l,
		cfg:   cfg,
		ret:   ret,
		docs:  make(map[string]*entry),
		exact: make(map[string]map[string]struct{}),
	}, nil
}

// Normalize returns the query form of s for this index's language.
func (ix *Index) Normalize(s string) string {
	return Normalize(ix.lang.code, s)
}

// Upsert adds docs, replacing any with the same ID.
func (ix *Index) Upsert(docs ...Doc) error {
	entries := make([]*entry, 0, len(docs))

	for _, d := range docs {
		if d.ID == "" {
			return errors.New("match: doc without ID")
		}

		entries = append(entries, ix.lang.newEntry(d))
	}

	ix.writeMu.Lock()
	defer ix.writeMu.Unlock()

	if ix.isClosed() {
		return ErrClosed
	}

	if err := ix.ret.upsert(entries); err != nil {
		return err
	}

	ix.mu.Lock()
	defer ix.mu.Unlock()

	for _, e := range entries {
		ix.unlink(e.doc.ID)
		ix.docs[e.doc.ID] = e

		for i := range e.aliases {
			a := &e.aliases[i]
			if a.derived {
				continue
			}

			ids := ix.exact[a.norm]
			if ids == nil {
				ids = make(map[string]struct{}, 1)
				ix.exact[a.norm] = ids
			}

			ids[e.doc.ID] = struct{}{}
		}
	}

	return nil
}

// Delete removes docs by ID. Unknown IDs are ignored.
func (ix *Index) Delete(ids ...string) error {
	ix.writeMu.Lock()
	defer ix.writeMu.Unlock()

	if ix.isClosed() {
		return ErrClosed
	}

	if err := ix.ret.remove(ids); err != nil {
		return err
	}

	ix.mu.Lock()
	defer ix.mu.Unlock()

	for _, id := range ids {
		ix.unlink(id)
		delete(ix.docs, id)
	}

	return nil
}

// Callers hold mu.
func (ix *Index) unlink(id string) {
	old, ok := ix.docs[id]
	if !ok {
		return
	}

	for i := range old.aliases {
		a := &old.aliases[i]
		if ids := ix.exact[a.norm]; ids != nil {
			delete(ids, id)

			if len(ids) == 0 {
				delete(ix.exact, a.norm)
			}
		}
	}
}

// Len returns the number of Docs.
func (ix *Index) Len() int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	return len(ix.docs)
}

// Close releases the index. Further calls fail with ErrClosed.
func (ix *Index) Close() error {
	ix.writeMu.Lock()
	defer ix.writeMu.Unlock()

	ix.mu.Lock()
	if ix.closed {
		ix.mu.Unlock()

		return nil
	}

	ix.closed = true
	ix.mu.Unlock()

	return ix.ret.close()
}

func (ix *Index) isClosed() bool {
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	return ix.closed
}

// Search ranks the Docs against q.
func (ix *Index) Search(ctx context.Context, q Query) (Result, error) {
	qf := ix.lang.newForm(ix.lang.normalizeSpelled(q.Text, false))
	res := Result{Normalized: qf.norm}

	if len(qf.toks) == 0 {
		return res, nil
	}

	if ix.isClosed() {
		return res, ErrClosed
	}

	yq := yearQuery(ix.lang, &qf)

	ids, err := ix.ret.candidates(ctx, &qf, q.Kinds, ix.cfg.CandidatePool)
	if err != nil {
		return res, err
	}

	entries := ix.lookup(ids, qf.norm, yq)

	if err := ctx.Err(); err != nil {
		return res, err
	}

	cands := make([]Candidate, 0, len(entries))
	exact := make(map[string]bool)

	for _, e := range entries {
		if len(q.Kinds) > 0 && !slices.Contains(q.Kinds, e.doc.Kind) {
			continue
		}

		best := ix.scoreEntry(&qf, e)
		if yq != nil && e.doc.Year == yq.year {
			if s := ix.scoreEntry(&yq.rest, e); s.score > best.score {
				best = s
			}
		}

		if best.exact {
			exact[e.doc.ID] = true
		}

		boost := min(max(e.doc.Boost+q.Hints[e.doc.ID], 0), ix.cfg.MaxBoost)
		best.bd.Boost = boost

		cands = append(cands, Candidate{Doc: e.doc, Score: min(best.score+boost, 1), Breakdown: best.bd})
	}

	slices.SortFunc(cands, func(a, b Candidate) int {
		return cmp.Or(
			cmp.Compare(b.Score, a.Score),
			compareBool(exact[b.Doc.ID], exact[a.Doc.ID]),
			cmp.Compare(a.Doc.Title, b.Doc.Title),
			cmp.Compare(a.Doc.ID, b.Doc.ID),
		)
	})

	res.Decision, res.Margin = ix.cfg.decide(cands, len(exact))

	limit := q.Limit
	if limit <= 0 {
		limit = ix.cfg.Limit
	}

	res.Candidates = cands[:min(limit, len(cands))]

	return res, nil
}

func (ix *Index) scoreEntry(q *form, e *entry) scored {
	best := scored{score: -1}

	for i := range e.aliases {
		if s := ix.scoreAlias(q, &e.aliases[i], best.score); s.score > best.score {
			best = s
		}
	}

	return best
}

func yearQuery(l *language, q *form) *yearQ {
	n := len(q.toks)
	if n < 2 || len(q.toks[n-1]) != 4 {
		return nil
	}

	y, err := strconv.Atoi(q.toks[n-1])
	if err != nil || y < 1880 || y > 2100 {
		return nil
	}

	return &yearQ{year: y, rest: l.newForm(q.toks[:n-1], "")}
}

// lookup resolves candidate IDs plus every exact match of norm (or of the year-less query for Docs from that year), deduplicated. Entries are immutable, so they stay valid after the lock is released.
func (ix *Index) lookup(ids []string, norm string, yq *yearQ) []*entry {
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	seen := make(map[string]bool, len(ids))
	out := make([]*entry, 0, len(ids)+1)

	add := func(id string, year int) {
		if seen[id] {
			return
		}

		if e, ok := ix.docs[id]; ok && (year == 0 || e.doc.Year == year) {
			seen[id] = true
			out = append(out, e)
		}
	}

	for id := range ix.exact[norm] {
		add(id, 0)
	}

	if yq != nil {
		for id := range ix.exact[yq.rest.norm] {
			add(id, yq.year)
		}
	}

	for _, id := range ids {
		add(id, 0)
	}

	return out
}

func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	default:
		return -1
	}
}
