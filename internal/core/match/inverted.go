package match

import (
	"context"
	"math"
	"slices"
	"unicode/utf8"

	"github.com/nightah/encore/internal/core/media"
)

func newInvertedRetriever(l *language) *invertedRetriever {
	r := &invertedRetriever{
		lang:    l,
		byID:    make(map[string]uint32),
		kinds:   make(map[media.Kind]int),
		buckets: make(map[wordBucket][]uint32),
		freqs:   make(map[freqKey]uint32),
	}

	for i := range r.fields {
		r.fields[i].ids = make(map[string]uint32)
	}

	return r
}

// docTerms lists an entry's terms per field, repeated as often as they occur: the distinct words of every alias, the phonetic keys of each word and of each alias with spaces removed, and the trigrams of those compact aliases.
func (l *language) docTerms(e *entry) (terms [termFields][]string) {
	var compacts []string

	for i := range e.aliases {
		a := &e.aliases[i]
		for _, t := range a.toks {
			if !slices.Contains(terms[termWords], t) {
				terms[termWords] = append(terms[termWords], t)
			}
		}

		if !slices.Contains(compacts, a.compact) {
			compacts = append(compacts, a.compact)
		}
	}

	for _, s := range terms[termWords] {
		terms[termPhonetic] = append(terms[termPhonetic], l.phonetic(s)...)
	}

	for _, s := range compacts {
		terms[termPhonetic] = append(terms[termPhonetic], l.phonetic(s)...)
		terms[termTrigrams] = appendTrigrams(terms[termTrigrams], s)
	}

	return terms
}

// appendTrigrams appends every run of three runes in s.
func appendTrigrams(dst []string, s string) []string {
	var buf [stackRunes]rune

	r := appendRunes(buf[:0], s)
	for i := 0; i+3 <= len(r); i++ {
		dst = append(dst, string(r[i:i+3]))
	}

	return dst
}

// lengthNorm is 1/sqrt(n) for a field of n terms, rounded to float32 the way Lucene-style indexes store it.
func lengthNorm(n int) float32 {
	if n == 0 {
		return 0
	}

	return float32(1 / math.Sqrt(float64(n)))
}

func (r *invertedRetriever) upsert(entries []*entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, e := range entries {
		if old, ok := r.byID[e.doc.ID]; ok {
			r.kill(old)
		}

		if len(r.slots) >= slotLimit {
			r.compact()
		}

		slot := uint32(len(r.slots)) //nolint:gosec // compact keeps the slot count below slotLimit.
		r.byID[e.doc.ID] = slot

		terms := r.lang.docTerms(e)

		var norms [termFields]float32

		for f := range termFields {
			norms[f] = lengthNorm(len(terms[f]))

			for _, t := range terms[f] {
				r.addPosting(f, t, slot)
			}
		}

		r.slots = append(r.slots, invertedSlot{entry: e, alive: true, norms: norms})
		r.kinds[e.doc.Kind]++
		r.live++
	}

	if r.dead >= compactMinDead && r.dead*compactRatio >= len(r.slots) {
		r.compact()
	}

	return nil
}

// addPosting counts one occurrence of term in slot, which is the newest slot, so its posting, if any, is the last one.
func (r *invertedRetriever) addPosting(f termField, term string, slot uint32) {
	pf := &r.fields[f]

	id, ok := pf.ids[term]
	if !ok {
		id = uint32(len(pf.terms)) //nolint:gosec // a field has far fewer than 2^32 distinct terms.
		pf.ids[term] = id
		pf.terms = append(pf.terms, term)
		pf.post = append(pf.post, nil)
		pf.live = append(pf.live, 0)

		if f == termWords {
			b := wordBucketOf(term)
			r.buckets[b] = append(r.buckets[b], id)
		}
	}

	p := pf.post[id]
	if n := len(p); n == 0 || p[n-1]&slotMask != slot {
		pf.post[id] = append(p, slot|1<<slotBits)
		pf.live[id]++

		return
	}

	switch tf := p[len(p)-1] >> slotBits; {
	case tf < tfOverflow-1:
		p[len(p)-1] += 1 << slotBits
	case tf == tfOverflow-1:
		p[len(p)-1] += 1 << slotBits
		r.freqs[freqKey{f, id, slot}] = tfOverflow
	default:
		r.freqs[freqKey{f, id, slot}]++
	}
}

func wordBucketOf(w string) wordBucket {
	c, _ := utf8.DecodeRuneInString(w)

	return wordBucket{first: c, runes: utf8.RuneCountInString(w)}
}

func (r *invertedRetriever) remove(ids []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, id := range ids {
		if s, ok := r.byID[id]; ok {
			r.kill(s)
			delete(r.byID, id)
		}
	}

	if r.dead >= compactMinDead && r.dead*compactRatio >= len(r.slots) {
		r.compact()
	}

	return nil
}

// kill tombstones a slot and takes it out of the document counts, so scores are as if it had never been indexed. Its postings stay until compact. Callers hold mu.
func (r *invertedRetriever) kill(s uint32) {
	slot := &r.slots[s]
	slot.alive = false
	r.dead++
	r.live--
	r.kinds[slot.entry.doc.Kind]--

	if r.kinds[slot.entry.doc.Kind] == 0 {
		delete(r.kinds, slot.entry.doc.Kind)
	}

	terms := r.lang.docTerms(slot.entry)
	for f := range termFields {
		pf := &r.fields[f]

		for i, t := range terms[f] {
			if !slices.Contains(terms[f][:i], t) {
				pf.live[pf.ids[t]]--
			}
		}
	}
}

// compact drops tombstoned slots from the postings and terms no live Doc has from the vocabulary. Callers hold mu.
func (r *invertedRetriever) compact() {
	remap := make([]uint32, len(r.slots))
	slots := make([]invertedSlot, 0, r.live)

	for i, s := range r.slots {
		if !s.alive {
			remap[i] = math.MaxUint32

			continue
		}

		remap[i] = uint32(len(slots)) //nolint:gosec // bounded by len(r.slots).
		r.byID[s.entry.doc.ID] = remap[i]
		slots = append(slots, s)
	}

	freqs := make(map[freqKey]uint32)
	buckets := make(map[wordBucket][]uint32)

	for f := range termFields {
		old := r.fields[f]
		pf := postingField{ids: make(map[string]uint32)}

		for id, term := range old.terms {
			if old.live[id] == 0 {
				continue
			}

			nid := uint32(len(pf.terms)) //nolint:gosec // bounded by len(old.terms).
			post := make([]uint32, 0, old.live[id])

			for _, p := range old.post[id] {
				s := remap[p&slotMask]
				if s == math.MaxUint32 {
					continue
				}

				if p>>slotBits == tfOverflow {
					freqs[freqKey{f, nid, s}] = r.freqs[freqKey{f, uint32(id), p & slotMask}]
				}

				post = append(post, s|p&^slotMask)
			}

			pf.ids[term] = nid
			pf.terms = append(pf.terms, term)
			pf.post = append(pf.post, post)
			pf.live = append(pf.live, old.live[id])

			if f == termWords {
				b := wordBucketOf(term)
				buckets[b] = append(buckets[b], nid)
			}
		}

		r.fields[f] = pf
	}

	r.slots, r.freqs, r.buckets, r.dead = slots, freqs, buckets, 0
}

// candidates runs a disjunction of fuzzy words and exact phonetic keys, with a trigram clause added when that finds too little; trigram postings are long, so they are only read when needed. Scores are TF-IDF with coordination factors, so the more clauses a Doc matches the better.
func (r *invertedRetriever) candidates(ctx context.Context, q *form, kinds []media.Kind, k int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	hits := r.search(q, kinds, k)

	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = r.slots[h.slot].entry.doc.ID
	}

	return ids, nil
}

// search returns the top k slots, best first. Callers hold mu for reading.
func (r *invertedRetriever) search(q *form, kinds []media.Kind, k int) []slotHit {
	clauses := r.wordClauses(q)

	hits := r.run(clauses, kinds, k)
	if len(hits) >= min(k, ngramFallbackHits) {
		return hits
	}

	return r.run(append(clauses, r.trigramClause(q)), kinds, k)
}

// wordClauses has one clause per query word, matching words within a few edits that share its first rune, and one per phonetic key.
func (r *invertedRetriever) wordClauses(q *form) []queryClause {
	terms := r.lang.queryTokens(q)
	keys := queryKeys(q, terms)
	clauses := make([]queryClause, 0, len(terms)+len(keys))

	for _, t := range terms {
		c := queryClause{field: termWords, boost: boostNorm, total: 1}

		if fz := fuzziness(t); fz > 0 {
			c.terms = r.expand(t, fz)
			c.total = max(len(c.terms), 1)
		} else if id, ok := r.fields[termWords].ids[t]; ok {
			c.terms = []clauseTerm{{id: id, weight: 1, count: 1}}
		}

		clauses = append(clauses, c)
	}

	for _, key := range keys {
		c := queryClause{field: termPhonetic, boost: boostPhon, total: 1}
		if id, ok := r.fields[termPhonetic].ids[key]; ok {
			c.terms = []clauseTerm{{id: id, weight: 1, count: 1}}
		}

		clauses = append(clauses, c)
	}

	return clauses
}

// expand finds the live words within fz edits of t, swaps of adjacent runes counting as one, that start with the same rune. Each is weighted down by its distance.
func (r *invertedRetriever) expand(t string, fz int) []clauseTerm {
	var buf [stackRunes]rune

	rt := appendRunes(buf[:0], t)
	pf := &r.fields[termWords]

	var out []clauseTerm

	for n := max(len(rt)-fz, 1); n <= len(rt)+fz; n++ {
		for _, id := range r.buckets[wordBucket{first: rt[0], runes: n}] {
			if pf.live[id] == 0 {
				continue
			}

			if d := osaWithin(rt, pf.terms[id], fz); d >= 0 {
				out = append(out, clauseTerm{id: id, weight: 1 / float64(1+d), count: 1})
			}
		}
	}

	return out
}

// trigramClause matches the query's trigrams, each occurrence counting separately.
func (r *invertedRetriever) trigramClause(q *form) queryClause {
	grams := appendTrigrams(nil, q.compact)
	c := queryClause{field: termTrigrams, boost: boostNgram, total: len(grams)}

	for i, g := range grams {
		if slices.Contains(grams[:i], g) {
			continue
		}

		id, ok := r.fields[termTrigrams].ids[g]
		if !ok {
			continue
		}

		n := 0

		for _, h := range grams[i:] {
			if h == g {
				n++
			}
		}

		c.terms = append(c.terms, clauseTerm{id: id, weight: 1, count: n})
	}

	return c
}

// run scores every slot matching at least one clause. A clause scores the sum of its matching terms times the share of its terms that matched; the query scores the sum of its clauses times the share of clauses that matched. A kind filter adds the kind's IDF, so rarer kinds rank a little higher.
func (r *invertedRetriever) run(clauses []queryClause, kinds []media.Kind, k int) []slotHit {
	acc := r.accumulator()
	defer r.release(acc)

	docs := float64(r.live)

	for _, c := range clauses {
		pf := &r.fields[c.field]
		single := c.total == 1 && len(c.terms) == 1

		for _, t := range c.terms {
			idf := 1 + math.Log(docs/float64(pf.live[t.id]+1))
			w := c.boost * t.weight * idf * idf

			for _, p := range pf.post[t.id] {
				s := p & slotMask

				tf := p >> slotBits
				if tf == tfOverflow {
					tf = r.freqs[freqKey{c.field, t.id, s}]
				}

				sc := w * sqrtFreq(tf) * float64(r.slots[s].norms[c.field])

				if single {
					acc.touch(s)
					acc.score[s] += sc
					acc.matched[s]++

					continue
				}

				if acc.clauseCount[s] == 0 {
					acc.clauseTouched = append(acc.clauseTouched, s)
				}

				acc.clauseScore[s] += float64(t.count) * sc
				acc.clauseCount[s] += uint32(t.count) //nolint:gosec // count is at most the number of query trigrams.
			}
		}

		for _, s := range acc.clauseTouched {
			acc.touch(s)
			acc.score[s] += acc.clauseScore[s] * float64(acc.clauseCount[s]) / float64(c.total)
			acc.matched[s]++
			acc.clauseScore[s], acc.clauseCount[s] = 0, 0
		}

		acc.clauseTouched = acc.clauseTouched[:0]
	}

	kindScore := make(map[media.Kind]float64, len(kinds))

	for _, kind := range kinds {
		if _, ok := kindScore[kind]; ok {
			continue
		}

		n := 0
		for _, k := range kinds {
			if k == kind {
				n++
			}
		}

		idf := 1 + math.Log(docs/float64(r.kinds[kind]+1))
		kindScore[kind] = float64(n) * idf * idf * float64(n) / float64(len(kinds))
	}

	top := make(topHits, 0, k)

	for _, s := range acc.touched {
		slot := &r.slots[s]
		if !slot.alive {
			continue
		}

		sc := acc.score[s] * float64(acc.matched[s]) / float64(len(clauses))

		if len(kinds) > 0 {
			ks, ok := kindScore[slot.entry.doc.Kind]
			if !ok {
				continue
			}

			sc += ks
		}

		top.offer(slotHit{slot: s, score: sc}, k)
	}

	return top.sorted()
}

func sqrtFreq(tf uint32) float64 {
	if int(tf) < len(sqrtFreqs) {
		return sqrtFreqs[tf]
	}

	return math.Sqrt(float64(tf))
}

var sqrtFreqs = func() (t [64]float64) {
	for i := range t {
		t[i] = math.Sqrt(float64(i))
	}

	return t
}()

// accumulator returns zeroed score arrays covering every slot. Callers hold mu for reading.
func (r *invertedRetriever) accumulator() *accumulator {
	acc, _ := r.pool.Get().(*accumulator)
	if acc == nil || len(acc.score) < len(r.slots) {
		n := len(r.slots) + len(r.slots)/4 + 64
		acc = &accumulator{score: make([]float64, n), matched: make([]uint32, n), clauseScore: make([]float64, n), clauseCount: make([]uint32, n)}
	}

	return acc
}

func (r *invertedRetriever) release(acc *accumulator) {
	for _, s := range acc.touched {
		acc.score[s], acc.matched[s] = 0, 0
	}

	acc.touched = acc.touched[:0]
	r.pool.Put(acc)
}

func (a *accumulator) touch(s uint32) {
	if a.matched[s] == 0 {
		a.touched = append(a.touched, s)
	}
}

// offer keeps the k best hits seen so far in a min-heap: lower scores, then later slots, are worse.
func (h *topHits) offer(x slotHit, k int) {
	if k <= 0 {
		return
	}

	if len(*h) < k {
		*h = append(*h, x)
		h.up(len(*h) - 1)

		return
	}

	if worse((*h)[0], x) {
		(*h)[0] = x
		h.down(0)
	}
}

func worse(a, b slotHit) bool {
	if a.score != b.score {
		return a.score < b.score
	}

	return a.slot > b.slot
}

func (h topHits) up(i int) {
	for i > 0 {
		p := (i - 1) / 2
		if !worse(h[i], h[p]) {
			return
		}

		h[i], h[p] = h[p], h[i]
		i = p
	}
}

func (h topHits) down(i int) {
	for {
		c := 2*i + 1
		if c >= len(h) {
			return
		}

		if c+1 < len(h) && worse(h[c+1], h[c]) {
			c++
		}

		if !worse(h[c], h[i]) {
			return
		}

		h[i], h[c] = h[c], h[i]
		i = c
	}
}

// sorted returns the hits best first.
func (h topHits) sorted() []slotHit {
	out := []slotHit(h)
	slices.SortFunc(out, func(a, b slotHit) int {
		switch {
		case worse(b, a):
			return -1
		case worse(a, b):
			return 1
		default:
			return 0
		}
	})

	return out
}

func (r *invertedRetriever) close() error { return nil }

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
