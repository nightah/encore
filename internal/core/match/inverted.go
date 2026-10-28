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
		lang:       l,
		postingSet: newPostingSet(),
		kinds:      make(map[media.Kind]int),
		maxSlots:   slotLimit,
	}
	r.background = r.wg.Go

	return r
}

func newPostingSet() postingSet {
	p := postingSet{
		byID:    make(map[string]uint32),
		buckets: make(map[wordBucket][]uint32),
		freqs:   make(map[freqKey]uint32),
	}

	for i := range p.fields {
		p.fields[i].ids = make(map[string]uint32)
	}

	return p
}

// docTerms lists an entry's terms per field, repeated as often as they occur: the distinct words of every alias, the phonetic keys of each word and of each alias with spaces removed, and the trigrams of those compact aliases.
func (l *language) docTerms(e *entry) (terms termSet) {
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

// upsert writes entries writeChunk at a time, working out their terms before taking the write lock, so searches wait for no more than one chunk's postings to change.
func (r *invertedRetriever) upsert(entries []*entry) error {
	for chunk := range slices.Chunk(entries, writeChunk) {
		if err := r.upsertChunk(chunk); err != nil {
			return err
		}
	}

	return nil
}

func (r *invertedRetriever) upsertChunk(entries []*entry) error {
	terms := make([]termSet, len(entries))
	ids := make([]string, len(entries))

	for i, e := range entries {
		terms[i] = r.lang.docTerms(e)
		ids[i] = e.doc.ID
	}

	old := r.currentTerms(ids)

	r.mu.Lock()
	defer r.mu.Unlock()

	for i, e := range entries {
		if s, ok := r.byID[e.doc.ID]; ok {
			r.kill(s, old.of(r.lang, r.slots[s].entry))
		}

		for len(r.slots) >= r.maxSlots {
			if r.dead == 0 {
				return errIndexFull
			}

			r.waitCompaction()
		}

		r.add(e, &terms[i])
		r.kinds[e.doc.Kind]++
		r.live++
	}

	r.maybeCompact()

	return nil
}

// currentTerms returns the terms of the Docs that ids name now, which a write about to replace or delete them needs, working them out under the read lock only.
func (r *invertedRetriever) currentTerms(ids []string) termCache {
	r.mu.RLock()

	var entries []*entry

	for _, id := range ids {
		if s, ok := r.byID[id]; ok {
			entries = append(entries, r.slots[s].entry)
		}
	}

	r.mu.RUnlock()

	c := make(termCache, len(entries))

	for _, e := range entries {
		t := r.lang.docTerms(e)
		c[e] = &t
	}

	return c
}

// of returns e's terms, working them out if a write raced currentTerms.
func (c termCache) of(l *language, e *entry) *termSet {
	if t, ok := c[e]; ok {
		return t
	}

	t := l.docTerms(e)

	return &t
}

// add indexes e, whose terms are given, in a new slot.
func (p *postingSet) add(e *entry, terms *termSet) uint32 {
	slot := uint32(len(p.slots)) //nolint:gosec // upsert keeps the slot count below slotLimit.
	p.byID[e.doc.ID] = slot

	var norms [termFields]float32

	for f := range termFields {
		norms[f] = lengthNorm(len(terms[f]))

		for _, t := range terms[f] {
			p.addPosting(f, t, slot)
		}
	}

	p.slots = append(p.slots, invertedSlot{entry: e, alive: true, norms: norms})

	return slot
}

// addPosting counts one occurrence of term in slot, which is the newest slot, so its posting, if any, is the last one.
func (p *postingSet) addPosting(f termField, term string, slot uint32) {
	pf := &p.fields[f]

	id, ok := pf.ids[term]
	if !ok {
		id = uint32(len(pf.terms)) //nolint:gosec // a field has far fewer than 2^32 distinct terms.
		pf.ids[term] = id
		pf.terms = append(pf.terms, term)
		pf.post = append(pf.post, nil)
		pf.live = append(pf.live, 0)

		if f == termWords {
			b := wordBucketOf(term)
			p.buckets[b] = append(p.buckets[b], id)
		}
	}

	post := pf.post[id]
	if n := len(post); n == 0 || post[n-1]&slotMask != slot {
		pf.post[id] = append(post, slot|1<<slotBits)
		pf.live[id]++

		return
	}

	switch tf := post[len(post)-1] >> slotBits; {
	case tf < tfOverflow-1:
		post[len(post)-1] += 1 << slotBits
	case tf == tfOverflow-1:
		post[len(post)-1] += 1 << slotBits
		p.freqs[freqKey{f, id, slot}] = tfOverflow
	default:
		p.freqs[freqKey{f, id, slot}]++
	}
}

func wordBucketOf(w string) wordBucket {
	c, _ := utf8.DecodeRuneInString(w)

	return wordBucket{first: c, runes: utf8.RuneCountInString(w)}
}

// remove deletes ids writeChunk at a time, like upsert.
func (r *invertedRetriever) remove(ids []string) error {
	for chunk := range slices.Chunk(ids, writeChunk) {
		r.removeChunk(chunk)
	}

	return nil
}

func (r *invertedRetriever) removeChunk(ids []string) {
	old := r.currentTerms(ids)

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, id := range ids {
		if s, ok := r.byID[id]; ok {
			r.kill(s, old.of(r.lang, r.slots[s].entry))
			delete(r.byID, id)
		}
	}

	r.maybeCompact()
}

// kill tombstones a slot, whose entry has terms, and takes it out of the document counts, so scores are as if it had never been indexed. Its postings stay until a compaction drops them. Callers hold mu.
func (r *invertedRetriever) kill(s uint32, terms *termSet) {
	kind := r.slots[s].entry.doc.Kind

	r.tombstone(s, terms)
	r.live--
	r.kinds[kind]--

	if r.kinds[kind] == 0 {
		delete(r.kinds, kind)
	}

	if r.compacting != nil {
		r.compacting.killed = append(r.compacting.killed, s)
	}
}

// tombstone marks slot s, whose entry has terms, dead and takes it out of its terms' live counts.
func (p *postingSet) tombstone(s uint32, terms *termSet) {
	p.slots[s].alive = false
	p.dead++

	for f := range termFields {
		pf := &p.fields[f]

		for i, t := range terms[f] {
			if !slices.Contains(terms[f][:i], t) {
				pf.live[pf.ids[t]]--
			}
		}
	}
}

// maybeCompact starts a compaction once tombstones are a quarter of the slots, unless one is running. Callers hold mu.
func (r *invertedRetriever) maybeCompact() {
	if r.compacting == nil && r.dead >= compactMinDead && r.dead*compactRatio >= len(r.slots) {
		r.startCompaction()
	}
}

// startCompaction compacts in the background. Callers hold mu.
func (r *invertedRetriever) startCompaction() *compaction {
	c := r.beginCompaction()
	r.background(func() { r.compactInto(c) })

	return c
}

// waitCompaction waits for a compaction, starting one if none is running. Callers hold mu, which is released while waiting.
func (r *invertedRetriever) waitCompaction() {
	c := r.compacting
	if c == nil {
		c = r.startCompaction()
	}

	r.mu.Unlock()
	<-c.done
	r.mu.Lock()
}

// beginCompaction starts logging kills for a compaction of the slots there are now. Callers hold mu.
func (r *invertedRetriever) beginCompaction() *compaction {
	c := &compaction{base: uint32(len(r.slots)), set: newPostingSet(), done: make(chan struct{})} //nolint:gosec // bounded by slotLimit.
	r.compacting = c

	return c
}

// compactInto builds postings without tombstones while searches and writes go on, taking the read lock a chunk at a time, catches up with the writes made meanwhile, and swaps the result in under a short write lock.
func (r *invertedRetriever) compactInto(c *compaction) {
	r.collectSlots(c)

	for f := range termFields {
		r.collectPostings(c, f)
	}

	for range catchUpRounds {
		if r.catchUp(c) <= catchUpOps {
			break
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.swap(c)
}

// collectSlots keeps the slots that were there when c began and are live when read, numbering them anew.
func (r *invertedRetriever) collectSlots(c *compaction) {
	for start := uint32(0); start < c.base; start += compactChunk {
		r.mu.RLock()

		for s := start; s < min(start+compactChunk, c.base); s++ {
			slot := r.slots[s]
			if !slot.alive {
				c.remap = append(c.remap, droppedSlot)

				continue
			}

			ns := uint32(len(c.set.slots)) //nolint:gosec // bounded by c.base.
			c.remap = append(c.remap, ns)
			c.set.byID[slot.entry.doc.ID] = ns
			c.set.slots = append(c.set.slots, slot)
		}

		r.mu.RUnlock()
	}
}

// collectPostings copies one field's postings of the slots collectSlots kept, dropping terms none of them has. Postings of later slots are left to catchUp: they come last, as postings are in slot order.
func (r *invertedRetriever) collectPostings(c *compaction, f termField) {
	dst := &c.set.fields[f]

	for start, done := 0, false; !done; start += compactChunk {
		r.mu.RLock()

		src := &r.fields[f]
		end := min(start+compactChunk, len(src.terms))
		done = end == len(src.terms)

		for id := start; id < end; id++ {
			nid := uint32(len(dst.terms)) //nolint:gosec // bounded by len(src.terms).
			post := make([]uint32, 0, max(src.live[id], 0))

			for _, p := range src.post[id] {
				old := p & slotMask
				if old >= c.base {
					break
				}

				s := c.remap[old]
				if s == droppedSlot {
					continue
				}

				if p>>slotBits == tfOverflow {
					c.set.freqs[freqKey{f, nid, s}] = r.freqs[freqKey{f, uint32(id), old}]
				}

				post = append(post, s|p&^slotMask)
			}

			if len(post) == 0 {
				continue
			}

			term := src.terms[id]
			dst.ids[term] = nid
			dst.terms = append(dst.terms, term)
			dst.post = append(dst.post, post)
			dst.live = append(dst.live, int32(len(post))) //nolint:gosec // bounded by slotLimit.

			if f == termWords {
				b := wordBucketOf(term)
				c.set.buckets[b] = append(c.set.buckets[b], nid)
			}
		}

		r.mu.RUnlock()
	}
}

// catchUp replays the writes made since c last caught up, under the read lock only, and returns how many there were.
func (r *invertedRetriever) catchUp(c *compaction) int {
	r.mu.RLock()
	added, killed := r.pending(c)
	r.mu.RUnlock()

	c.apply(r.lang, added, killed)

	return len(added) + len(killed)
}

// swap replays the last writes and makes c the index. Callers hold mu.
func (r *invertedRetriever) swap(c *compaction) {
	added, killed := r.pending(c)
	c.apply(r.lang, added, killed)

	r.postingSet = c.set
	r.compacting = nil
	close(c.done)

	r.maybeCompact()
}

// pending returns the writes c has yet to replay: the entries of slots added since it last caught up, and the slots killed. Callers hold mu for reading.
func (r *invertedRetriever) pending(c *compaction) (added []*entry, killed []uint32) {
	for s := len(c.remap); s < len(r.slots); s++ {
		added = append(added, r.slots[s].entry)
	}

	killed = slices.Clone(c.killed[c.replayed:])
	c.replayed = len(c.killed)

	return added, killed
}

// apply replays writes on c: slots added, which were live when written, then slots killed, which may include some of those.
func (c *compaction) apply(l *language, added []*entry, killed []uint32) {
	for _, e := range added {
		t := l.docTerms(e)
		c.remap = append(c.remap, c.set.add(e, &t))
	}

	for _, s := range killed {
		ns := c.remap[s]
		if ns == droppedSlot {
			continue
		}

		e := c.set.slots[ns].entry
		t := l.docTerms(e)
		c.set.tombstone(ns, &t)

		if c.set.byID[e.doc.ID] == ns {
			delete(c.set.byID, e.doc.ID)
		}
	}
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

// close waits for a running compaction.
func (r *invertedRetriever) close() error {
	r.wg.Wait()

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
