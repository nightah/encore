//go:build retrieverbench

package match

import (
	"container/heap"
	"context"
	"math"
	"slices"
	"sync"
	"unicode/utf8"

	"github.com/nightah/encore/internal/core/media"
)

// invertedRetriever is an experimental in-tree replacement for bleve that mirrors its query: fuzzy words with a one-rune prefix, exact phonetic keys, and a trigram fallback, ranked by boosted IDF with a coordination factor and field-length norms.
type invertedRetriever struct {
	mu    sync.RWMutex
	lang  *language
	slots []invSlot
	byID  map[string]uint32
	dead  int

	words, phon, grams invField
	// buckets groups word terms by first rune and rune length for fuzzy expansion.
	buckets map[invBucket][]uint32
	pool    sync.Pool
}

const slotMask = 1<<28 - 1

var sqrtTF = func() (t [16]float32) {
	for i := range t {
		t[i] = float32(math.Sqrt(float64(i)))
	}

	return t
}()

type invSlot struct {
	id    string
	kind  media.Kind
	alive bool
	// norms are 1/sqrt(field length) for words, phonetic keys and trigrams.
	norms [3]float32
}

type invField struct {
	ids   map[string]uint32
	terms []string
	post  [][]uint32
}

type invBucket struct {
	first rune
	n     int
}

type invAcc struct {
	score []float32
	// last is the latest clause (plus one) that matched a slot, and count how many clauses did, for the coordination factor.
	last, count []uint8
	touched     []uint32
}

func init() {
	scaleRetrievers["inverted"] = func(l *language) (retriever, error) { return newInvertedRetriever(l), nil }
}

func newInvertedRetriever(l *language) *invertedRetriever {
	f := func() invField { return invField{ids: make(map[string]uint32)} }

	return &invertedRetriever{lang: l, byID: make(map[string]uint32), words: f(), phon: f(), grams: f(), buckets: make(map[invBucket][]uint32)}
}

func (f *invField) add(term string, slot uint32) (id uint32, isNew bool) {
	id, ok := f.ids[term]
	if !ok {
		id = uint32(len(f.terms))
		f.ids[term] = id
		f.terms = append(f.terms, term)
		f.post = append(f.post, nil)
	}

	// A posting is a slot in the low 28 bits and its term frequency, capped at 15, in the high 4.
	p := f.post[id]
	switch {
	case len(p) == 0 || p[len(p)-1]&slotMask != slot:
		f.post[id] = append(p, slot|1<<28)
	case p[len(p)-1]>>28 < 15:
		p[len(p)-1] += 1 << 28
	}

	return id, !ok
}

func trigramsOf(s string, into []string) []string {
	r := []rune(s)
	for i := 0; i+3 <= len(r); i++ {
		into = append(into, string(r[i:i+3]))
	}

	return into
}

func (r *invertedRetriever) upsert(entries []*entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, e := range entries {
		if old, ok := r.byID[e.doc.ID]; ok {
			r.slots[old].alive = false
			r.dead++
		}

		slot := uint32(len(r.slots))
		r.byID[e.doc.ID] = slot

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

		nPhon, nGrams := 0, 0

		for _, w := range words {
			if id, isNew := r.words.add(w, slot); isNew {
				b := invBucket{first: firstRune(w), n: utf8.RuneCountInString(w)}
				r.buckets[b] = append(r.buckets[b], id)
			}
		}

		for _, s := range append(slices.Clone(words), compacts...) {
			for _, k := range r.lang.phonetic(s) {
				r.phon.add(k, slot)
				nPhon++
			}
		}

		var grams []string
		for _, c := range compacts {
			grams = trigramsOf(c, grams)
		}

		for _, g := range grams {
			r.grams.add(g, slot)
		}

		nGrams = len(grams)

		r.slots = append(r.slots, invSlot{id: e.doc.ID, kind: e.doc.Kind, alive: true, norms: [3]float32{lenNorm(len(words)), lenNorm(nPhon), lenNorm(nGrams)}})
	}

	r.maybeCompact()

	return nil
}

func lenNorm(n int) float32 {
	if n == 0 {
		return 0
	}

	return float32(1 / math.Sqrt(float64(n)))
}

func firstRune(s string) rune {
	c, _ := utf8.DecodeRuneInString(s)

	return c
}

func (r *invertedRetriever) remove(ids []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, id := range ids {
		if s, ok := r.byID[id]; ok {
			r.slots[s].alive = false
			r.dead++

			delete(r.byID, id)
		}
	}

	r.maybeCompact()

	return nil
}

// maybeCompact drops tombstoned slots from the postings once they are a quarter of the index. Callers hold mu.
func (r *invertedRetriever) maybeCompact() {
	if r.dead < 1024 || r.dead*4 < len(r.slots) {
		return
	}

	remap := make([]uint32, len(r.slots))
	slots := r.slots[:0:0]

	for i, s := range r.slots {
		if s.alive {
			remap[i] = uint32(len(slots))
			r.byID[s.id] = remap[i]
			slots = append(slots, s)
		} else {
			remap[i] = math.MaxUint32
		}
	}

	for _, f := range []*invField{&r.words, &r.phon, &r.grams} {
		for t, p := range f.post {
			out := p[:0]
			for _, s := range p {
				if m := remap[s&slotMask]; m != math.MaxUint32 {
					out = append(out, m|s&^slotMask)
				}
			}

			f.post[t] = slices.Clip(out)
		}
	}

	r.slots = slices.Clip(slots)
	r.dead = 0
}

// clause is one query term group: its postings are summed with one boost and count once for coordination.
type invClause struct {
	field int
	boost float32
	terms []invTerm
}

type invTerm struct {
	post   [][]uint32
	weight float32
}

func (r *invertedRetriever) candidates(ctx context.Context, q *form, kinds []media.Kind, k int) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	clauses := r.wordClauses(q)

	ids := r.search(clauses, kinds, k)
	if len(ids) < min(k, ngramFallbackHits) {
		var grams []string

		c := invClause{field: 2, boost: boostNgram}

		for _, g := range trigramsOf(q.compact, grams) {
			if id, ok := r.grams.ids[g]; ok {
				c.terms = append(c.terms, invTerm{post: [][]uint32{r.grams.post[id]}, weight: 1})
			}
		}

		ids = r.search(append(clauses, c), kinds, k)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return ids, nil
}

func (r *invertedRetriever) wordClauses(q *form) []invClause {
	terms := r.lang.queryTokens(q)
	clauses := make([]invClause, 0, 2*len(terms)+1)
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

	for _, t := range terms {
		c := invClause{field: 0, boost: boostNorm}

		if fz := fuzziness(t); fz > 0 {
			rt := []rune(t)
			for n := len(rt) - fz; n <= len(rt)+fz; n++ {
				for _, id := range r.buckets[invBucket{first: rt[0], n: n}] {
					if d := levenshteinWithin(rt, r.words.terms[id], fz); d >= 0 {
						c.terms = append(c.terms, invTerm{post: [][]uint32{r.words.post[id]}, weight: 1 / float32(1+d)})
					}
				}
			}

			// bleve scores a fuzzy term as a disjunction of its expansions, whose coordination factor divides by their number.
			for i := range c.terms {
				c.terms[i].weight /= float32(len(c.terms))
			}
		} else if id, ok := r.words.ids[t]; ok {
			c.terms = append(c.terms, invTerm{post: [][]uint32{r.words.post[id]}, weight: 1})
		}

		clauses = append(clauses, c)
	}

	for _, k := range keys {
		c := invClause{field: 1, boost: boostPhon}
		if id, ok := r.phon.ids[k]; ok {
			c.terms = append(c.terms, invTerm{post: [][]uint32{r.phon.post[id]}, weight: 1})
		}

		clauses = append(clauses, c)
	}

	return clauses
}

// levenshteinWithin returns the edit distance between a and b when it is at most max, else -1.
func levenshteinWithin(a []rune, b string, maxD int) int {
	var buf [2][64]int

	rb := []rune(b)
	if len(rb) >= 63 || len(a) >= 63 {
		return -1
	}

	prev, cur := buf[0][:len(rb)+1], buf[1][:len(rb)+1]
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		cur[0] = i
		best := cur[0]

		for j := 1; j <= len(rb); j++ {
			cost := 1
			if a[i-1] == rb[j-1] {
				cost = 0
			}

			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			best = min(best, cur[j])
		}

		if best > maxD {
			return -1
		}

		prev, cur = cur, prev
	}

	if d := prev[len(rb)]; d <= maxD {
		return d
	}

	return -1
}

func (r *invertedRetriever) search(clauses []invClause, kinds []media.Kind, k int) []string {
	acc, _ := r.pool.Get().(*invAcc)
	if acc == nil || len(acc.score) < len(r.slots) {
		n := len(r.slots) + 1024
		acc = &invAcc{score: make([]float32, n), last: make([]uint8, n), count: make([]uint8, n)}
	}

	defer func() {
		for _, s := range acc.touched {
			acc.score[s], acc.last[s], acc.count[s] = 0, 0, 0
		}

		acc.touched = acc.touched[:0]
		r.pool.Put(acc)
	}()

	n := float64(len(r.slots) - r.dead + 1)

	for ci, c := range clauses {
		mark := uint8(ci + 1)

		for _, t := range c.terms {
			for _, p := range t.post {
				idf := float32(1 + math.Log(n/float64(len(p)+1)))
				// bleve's TF-IDF counts IDF twice, once in the query weight and once in the field weight.
				w := c.boost * t.weight * idf * idf

				for _, e := range p {
					s := e & slotMask
					if acc.last[s] != mark {
						if acc.count[s] == 0 {
							acc.touched = append(acc.touched, s)
						}

						acc.last[s] = mark
						acc.count[s]++
					}

					acc.score[s] += w * sqrtTF[e>>28] * r.slots[s].norms[c.field]
				}
			}
		}
	}

	coord := 1 / float32(max(len(clauses), 1))

	h := make(invHeap, 0, k)

	for _, s := range acc.touched {
		slot := &r.slots[s]
		if !slot.alive || (len(kinds) > 0 && !slices.Contains(kinds, slot.kind)) {
			continue
		}

		sc := acc.score[s] * float32(acc.count[s]) * coord
		if len(h) < k {
			heap.Push(&h, invHit{s, sc})
		} else if sc > h[0].score || (sc == h[0].score && s < h[0].slot) {
			h[0] = invHit{s, sc}
			heap.Fix(&h, 0)
		}
	}

	slices.SortFunc(h, func(a, b invHit) int {
		if a.score != b.score {
			if a.score > b.score {
				return -1
			}

			return 1
		}

		return int(a.slot) - int(b.slot)
	})

	ids := make([]string, len(h))
	for i, x := range h {
		ids[i] = r.slots[x.slot].id
	}

	return ids
}

type invHit struct {
	slot  uint32
	score float32
}

type invHeap []invHit

func (h invHeap) Len() int { return len(h) }
func (h invHeap) Less(i, j int) bool {
	if h[i].score != h[j].score {
		return h[i].score < h[j].score
	}

	return h[i].slot > h[j].slot
}
func (h invHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *invHeap) Push(x any)   { *h = append(*h, x.(invHit)) }
func (h *invHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]

	return x
}

func (r *invertedRetriever) close() error { return nil }
