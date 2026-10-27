package match

import (
	"cmp"
	"context"
	"slices"

	"github.com/nightah/encore/internal/core/media"
)

func newTrigramRetriever() *trigramRetriever {
	return &trigramRetriever{byID: make(map[string]int32), postings: make(map[string][]int32)}
}

// Trigrams include word boundaries.
func grams(f *form, into map[string]bool) {
	for _, s := range []string{f.norm, f.compact} {
		r := []rune("$" + s + "$")
		for i := 0; i+3 <= len(r); i++ {
			into[string(r[i:i+3])] = true
		}
	}

	for _, ks := range f.keys {
		for _, k := range ks {
			into["#"+k] = true
		}
	}

	for _, k := range f.compactKeys {
		into["#"+k] = true
	}
}

func (r *trigramRetriever) upsert(entries []*entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, e := range entries {
		if old, ok := r.byID[e.doc.ID]; ok {
			r.slots[old].alive = false
		}

		slot := int32(len(r.slots)) //nolint:gosec // slot counts stay far below 2^31 for the libraries this retriever serves.
		r.slots = append(r.slots, trigramSlot{id: e.doc.ID, kind: e.doc.Kind, alive: true})
		r.byID[e.doc.ID] = slot

		g := make(map[string]bool)
		for i := range e.aliases {
			grams(&e.aliases[i].form, g)
		}

		for k := range g {
			r.postings[k] = append(r.postings[k], slot)
		}
	}

	return nil
}

func (r *trigramRetriever) remove(ids []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, id := range ids {
		if s, ok := r.byID[id]; ok {
			r.slots[s].alive = false
			delete(r.byID, id)
		}
	}

	return nil
}

func (r *trigramRetriever) candidates(ctx context.Context, q *form, kinds []media.Kind, k int) ([]string, error) {
	g := make(map[string]bool)
	grams(q, g)

	r.mu.RLock()
	defer r.mu.RUnlock()

	hits := make(map[int32]int)

	for gram := range g {
		w := 1
		if gram[0] == '#' {
			w = 2
		}

		for _, s := range r.postings[gram] {
			hits[s] += w
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	type hit struct {
		slot  int32
		count int
	}

	ranked := make([]hit, 0, len(hits))

	for s, c := range hits {
		slot := r.slots[s]
		if slot.alive && (len(kinds) == 0 || slices.Contains(kinds, slot.kind)) {
			ranked = append(ranked, hit{s, c})
		}
	}

	slices.SortFunc(ranked, func(a, b hit) int {
		return cmp.Or(cmp.Compare(b.count, a.count), cmp.Compare(a.slot, b.slot))
	})

	ids := make([]string, 0, min(k, len(ranked)))
	for _, h := range ranked[:min(k, len(ranked))] {
		ids = append(ids, r.slots[h.slot].id)
	}

	return ids, nil
}

func (r *trigramRetriever) close() error { return nil }
