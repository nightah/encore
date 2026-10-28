package match

import (
	"fmt"
	"maps"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// invModel is what an inverted index should hold: the live entries by ID, and the order they were last written in.
type invModel struct {
	byID  map[string]*entry
	order []string
}

func newInvModel() *invModel { return &invModel{byID: make(map[string]*entry)} }

func (m *invModel) upsert(entries []*entry) {
	for _, e := range entries {
		m.order = append(slices.DeleteFunc(m.order, func(id string) bool { return id == e.doc.ID }), e.doc.ID)
		m.byID[e.doc.ID] = e
	}
}

func (m *invModel) remove(ids []string) {
	for _, id := range ids {
		delete(m.byID, id)
		m.order = slices.DeleteFunc(m.order, func(o string) bool { return o == id })
	}
}

func (m *invModel) entries() []*entry {
	out := make([]*entry, len(m.order))
	for i, id := range m.order {
		out[i] = m.byID[id]
	}

	return out
}

// checkInverted checks everything retrieval scores depend on against the model: the live slots and their IDs, document counts per term, and postings in slot order.
func checkInverted(t require.TestingT, inv *invertedRetriever, m *invModel) {
	if h, ok := t.(interface{ Helper() }); ok {
		h.Helper()
	}

	inv.mu.RLock()
	defer inv.mu.RUnlock()

	require.Equal(t, len(m.byID), inv.live, "live Docs")
	require.Len(t, inv.byID, len(m.byID), "IDs")

	alive := 0

	for _, s := range inv.slots {
		if s.alive {
			alive++
		}
	}

	require.Equal(t, inv.live, alive, "live slots")
	require.Equal(t, len(inv.slots)-alive, inv.dead, "tombstones")

	want := [termFields]map[string]int32{}

	for f := range termFields {
		want[f] = make(map[string]int32)
	}

	for id, e := range m.byID {
		s, ok := inv.byID[id]
		require.True(t, ok, "%s missing", id)
		require.True(t, inv.slots[s].alive, "%s is a tombstone", id)
		require.Same(t, e, inv.slots[s].entry, "%s", id)

		terms := inv.lang.docTerms(e)
		for f := range termFields {
			for i, term := range terms[f] {
				if !slices.Contains(terms[f][:i], term) {
					want[f][term]++
				}
			}
		}
	}

	for f := range termFields {
		pf := &inv.fields[f]
		got := make(map[string]int32)

		for id, n := range pf.live {
			if n != 0 {
				got[pf.terms[id]] = n
			}

			require.True(t, slices.IsSortedFunc(pf.post[id], func(a, b uint32) int { return int(a&slotMask) - int(b&slotMask) }), "field %d term %q postings out of order", f, pf.terms[id])
		}

		require.Equal(t, want[f], got, "field %d live counts", f)
	}
}

// checkRankings compares the index's candidates for queries against the reference scorer over the model.
func checkRankings(t require.TestingT, inv *invertedRetriever, m *invModel, queries []string) {
	if h, ok := t.(interface{ Helper() }); ok {
		h.Helper()
	}

	ref := refDocs(inv.lang, m.entries())

	for _, text := range queries {
		qf := inv.lang.newForm(inv.lang.normalizeSpelled(text, false))
		if len(qf.toks) == 0 {
			continue
		}

		assertSameRanking(t, refRanking(inv.lang, ref, &qf, nil, 50), invertedRanking(inv, &qf, nil, 50), 50, text)
	}
}

func entriesOf(l *language, docs []Doc) []*entry {
	out := make([]*entry, len(docs))
	for i, d := range docs {
		out[i] = l.newEntry(d)
	}

	return out
}

func idsOf(docs []Doc) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.ID
	}

	return out
}

// TestInvertedCompactsInBackground deletes a quarter of the Docs, enough to compact the index. Compacting under the write lock before the delete returned would block every search meanwhile; the delete returns with the compaction still to run, searches answer from the tombstoned index, writes made before the compaction finishes are kept, and the result is what a fresh index holds.
func TestInvertedCompactsInBackground(t *testing.T) {
	l := languageFor("en")
	inv := newInvertedRetriever(l)

	var pending []func()

	inv.background = func(f func()) { pending = append(pending, f) }

	docs := syntheticDocs(4000, 21)
	m := newInvModel()
	all := entriesOf(l, docs)
	require.NoError(t, inv.upsert(all))
	m.upsert(all)

	gone := idsOf(docs[:1100])
	require.NoError(t, inv.remove(gone))
	m.remove(gone)

	require.Len(t, pending, 1, "the delete did not leave the compaction to run in the background")
	require.Equal(t, 1100, inv.dead, "tombstones before the compaction ran")

	queries := []string{docs[2000].Title, docs[3999].Title, "starnight", docs[10].Title}
	checkInverted(t, inv, m)
	checkRankings(t, inv, m, queries)

	// Writes racing the compaction: Docs replaced, deleted, and added, some deleted again before it finishes.
	retitled := syntheticDocs(200, 22)
	replaced := make([]Doc, 0, 200)

	for i, d := range docs[1100:1300] {
		d.Title = retitled[i].Title
		replaced = append(replaced, d)
	}

	added := []Doc{movie("new1", "Inception", 2010), movie("new2", "Interstellar", 2014), movie("new3", "Tenet", 2020)}

	for _, ds := range [][]Doc{replaced, added} {
		es := entriesOf(l, ds)
		require.NoError(t, inv.upsert(es))
		m.upsert(es)
	}

	require.NoError(t, inv.remove([]string{"new2", docs[1500].ID, docs[1150].ID}))
	m.remove([]string{"new2", docs[1500].ID, docs[1150].ID})

	pending[0]()

	// The compaction left out every slot dead when it read them and replayed the 203 slots added since it began; two of those were deleted.
	assert.Equal(t, 2, inv.dead, "tombstones left by the writes that raced the compaction")
	assert.Len(t, inv.slots, 4000-1100-200-1+203, "slots after the compaction")
	assert.Nil(t, inv.compacting)

	checkInverted(t, inv, m)
	checkRankings(t, inv, m, append(queries, "inception", "interstellar", "tenet", replaced[0].Title, replaced[50].Title))
}

// TestPropInvertedCompactionModel runs upserts and deletes interleaved with every step of a compaction, catching up and swapping at any point, and checks the index against a model of the live Docs after each step.
func TestPropInvertedCompactionModel(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		l := languageFor(rapid.SampledFrom(langs).Draw(t, "lang"))
		inv := newInvertedRetriever(l)
		inv.background = func(func()) { t.Fatal("compacted on its own") }

		m := newInvModel()

		var (
			c      *compaction
			phase  int
			titles []string
		)

		t.Repeat(map[string]func(*rapid.T){
			"upsert": func(t *rapid.T) {
				docs := rapid.SliceOfN(genDiffDoc(), 1, 12).Draw(t, "docs")
				es := entriesOf(l, docs)
				require.NoError(t, inv.upsert(es))
				m.upsert(es)

				for _, d := range docs {
					titles = append(titles, d.Title)
				}
			},
			"remove": func(t *rapid.T) {
				ids := rapid.SliceOfN(rapid.Map(rapid.IntRange(0, 85), func(i int) string { return fmt.Sprintf("d%d", i) }), 1, 12).Draw(t, "ids")
				require.NoError(t, inv.remove(ids))
				m.remove(ids)
			},
			"begin": func(t *rapid.T) {
				if c != nil {
					t.Skip("a compaction is running")
				}

				inv.mu.Lock()
				c = inv.beginCompaction()
				inv.mu.Unlock()

				phase = 0
			},
			"step": func(t *rapid.T) {
				if c == nil {
					t.Skip("no compaction")
				}

				switch phase {
				case 0:
					inv.collectSlots(c)
				case 1, 2, 3:
					inv.collectPostings(c, termField(phase-1))
				default:
					inv.catchUp(c)
				}

				phase++
			},
			"swap": func(t *rapid.T) {
				if c == nil || phase < 4 {
					t.Skip("nothing to swap in")
				}

				inv.mu.Lock()
				inv.swap(c)
				inv.mu.Unlock()

				c = nil
			},
			"run": func(t *rapid.T) {
				if c == nil || phase > 0 {
					t.Skip("no fresh compaction")
				}

				inv.compactInto(c)

				c = nil
			},
			"search": func(t *rapid.T) {
				checkRankings(t, inv, m, []string{genDiffQuery(titles).Draw(t, "query")})
			},
			"": func(t *rapid.T) {
				checkInverted(t, inv, m)
			},
		})
	})
}

// TestInvertedCompactsUnderConcurrentWrites has writers replace and delete Docs directly on the retriever, so that they race each other and the compactions they trigger, while readers search; a Doc nobody touches must stay the top candidate for its own title throughout. Afterwards the index must match a model of what each writer did.
func TestInvertedCompactsUnderConcurrentWrites(t *testing.T) {
	l := languageFor("en")
	inv := newInvertedRetriever(l)

	var compactions atomic.Int32

	inv.background = func(f func()) {
		compactions.Add(1)
		inv.wg.Go(f)
	}

	const writers, perWriter = 3, 1500

	base := syntheticDocs(writers*perWriter, 31)
	initial := entriesOf(l, append(slices.Clone(base), movie("anchor", "Inception", 2010)))
	require.NoError(t, inv.upsert(initial))

	var (
		wg      sync.WaitGroup
		stop    = make(chan struct{})
		readers sync.WaitGroup
	)

	qf := l.newForm(l.normalizeSpelled("inception", false))

	for range 4 {
		readers.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}

				ids, err := inv.candidates(t.Context(), &qf, nil, 20)
				if err != nil {
					t.Error(err)

					return
				}

				if !assert.NotEmpty(t, ids) || !assert.Equal(t, "anchor", ids[0]) {
					return
				}
			}
		})
	}

	models := make([]*invModel, writers)

	for w := range writers {
		models[w] = newInvModel()
		mine := base[w*perWriter : (w+1)*perWriter]
		models[w].upsert(initial[w*perWriter : (w+1)*perWriter])

		wg.Go(func() {
			m := models[w]

			for round := range 10 {
				retitled := syntheticDocs(len(mine), uint64(1000+10*w+round))

				var (
					gone []string
					back []Doc
				)

				for i := round % 3; i < len(mine); i += 3 {
					gone = append(gone, mine[i].ID)

					if i%2 == 0 {
						d := mine[i]
						d.Title = retitled[i].Title
						back = append(back, d)
					}
				}

				if err := inv.remove(gone); err != nil {
					t.Error(err)

					return
				}

				m.remove(gone)

				// Bring some back in small batches, as single-item library updates arrive.
				for chunk := range slices.Chunk(back, 7) {
					es := entriesOf(l, chunk)
					if err := inv.upsert(es); err != nil {
						t.Error(err)

						return
					}

					m.upsert(es)
				}
			}
		})
	}

	wg.Wait()
	close(stop)
	readers.Wait()
	require.NoError(t, inv.close())

	assert.Greater(t, compactions.Load(), int32(1), "too few compactions raced the writers")

	all := newInvModel()
	all.upsert(initial[len(base):])

	for _, m := range models {
		maps.Copy(all.byID, m.byID)
		all.order = append(all.order, m.order...)
	}

	checkInverted(t, inv, all)
	checkRankings(t, inv, all, []string{"inception", base[1].Title, base[2000].Title, base[4000].Title, "starnight"})
}

// TestInvertedReclaimsSlotsWhenFull fills every slot: an upsert then waits for a compaction to reclaim tombstones, starting one or joining the one running, and fails only when no tombstone is left.
func TestInvertedReclaimsSlotsWhenFull(t *testing.T) {
	l := languageFor("en")
	docs := syntheticDocs(40, 41)

	t.Run("starts a compaction", func(t *testing.T) {
		inv := newInvertedRetriever(l)
		inv.maxSlots = 10
		m := newInvModel()

		first := entriesOf(l, docs[:8])
		require.NoError(t, inv.upsert(first))
		m.upsert(first)

		require.NoError(t, inv.remove(idsOf(docs[:5])))
		m.remove(idsOf(docs[:5]))

		more := entriesOf(l, docs[8:15])
		require.NoError(t, inv.upsert(more))
		m.upsert(more)
		require.NoError(t, inv.close())

		checkInverted(t, inv, m)
		checkRankings(t, inv, m, []string{docs[9].Title, docs[6].Title})

		require.ErrorIs(t, inv.upsert(entriesOf(l, docs[15:17])), errIndexFull)
	})

	t.Run("joins a running compaction", func(t *testing.T) {
		inv := newInvertedRetriever(l)
		inv.maxSlots = 10
		m := newInvModel()

		var pending []func()

		inv.background = func(f func()) { pending = append(pending, f) }

		first := entriesOf(l, docs[:8])
		require.NoError(t, inv.upsert(first))
		m.upsert(first)

		require.NoError(t, inv.remove(idsOf(docs[:4])))
		m.remove(idsOf(docs[:4]))

		inv.mu.Lock()
		inv.startCompaction()
		inv.mu.Unlock()
		require.Len(t, pending, 1)

		more := entriesOf(l, docs[8:13])
		done := make(chan error)

		go func() { done <- inv.upsert(more) }()

		// Wait until the upsert has filled the slots and let go of the lock to wait for the compaction.
		for {
			if inv.mu.TryLock() {
				full := len(inv.slots) >= inv.maxSlots
				inv.mu.Unlock()

				if full {
					break
				}
			}

			runtime.Gosched()
		}

		pending[0]()
		require.NoError(t, <-done)
		m.upsert(more)

		checkInverted(t, inv, m)
		checkRankings(t, inv, m, []string{docs[9].Title, docs[12].Title})
	})
}
