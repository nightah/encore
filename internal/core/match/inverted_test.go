package match

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/nightah/encore/internal/core/media"
)

// scoredID is a retrieval hit with its retrieval score.
type scoredID struct {
	id    string
	score float64
}

// invertedRanking runs the inverted index's query and returns its top k with scores.
func invertedRanking(r *invertedRetriever, q *form, kinds []media.Kind, k int) []scoredID {
	r.mu.RLock()
	defer r.mu.RUnlock()

	hits := r.search(q, kinds, k)
	out := make([]scoredID, len(hits))

	for i, h := range hits {
		out[i] = scoredID{r.slots[h.slot].entry.doc.ID, h.score}
	}

	return out
}

// refDoc is one Doc as the reference scorer sees it: term frequencies and length norms per field.
type refDoc struct {
	id   string
	kind media.Kind
	tf   [termFields]map[string]int
	norm [termFields]float64
}

func refDocs(l *language, entries []*entry) []refDoc {
	out := make([]refDoc, len(entries))

	for i, e := range entries {
		terms := l.docTerms(e)
		d := refDoc{id: e.doc.ID, kind: e.doc.Kind}

		for f := range termFields {
			d.tf[f] = make(map[string]int)
			for _, t := range terms[f] {
				d.tf[f][t]++
			}

			if n := len(terms[f]); n > 0 {
				d.norm[f] = float64(float32(1 / math.Sqrt(float64(n))))
			}
		}

		out[i] = d
	}

	return out
}

type refAlt struct {
	term   string
	weight float64
	count  int
}

type refClause struct {
	field termField
	boost float64
	alts  []refAlt
	total int
}

// refRanking is the retrieval query scored Doc by Doc, straight from its definition and without postings: the fixed reference the inverted index is checked against. It returns every match, best first.
func refRanking(l *language, docs []refDoc, q *form, kinds []media.Kind, k int) []scoredID {
	df := [termFields]map[string]int{}
	for f := range termFields {
		df[f] = make(map[string]int)
	}

	kindDocs := make(map[media.Kind]int)

	for _, d := range docs {
		for f := range termFields {
			for t := range d.tf[f] {
				df[f][t]++
			}
		}

		kindDocs[d.kind]++
	}

	vocab := make([]string, 0, len(df[termWords]))
	for w := range df[termWords] {
		vocab = append(vocab, w)
	}

	slices.Sort(vocab)

	terms := l.queryTokens(q)

	var clauses []refClause

	for _, t := range terms {
		c := refClause{field: termWords, boost: boostNorm, total: 1}

		if fz := fuzziness(t); fz > 0 {
			first, _ := utf8.DecodeRuneInString(t)
			for _, w := range vocab {
				if r, _ := utf8.DecodeRuneInString(w); r == first {
					if d := osaDistance(t, w); d <= fz {
						c.alts = append(c.alts, refAlt{w, 1 / float64(1+d), 1})
					}
				}
			}

			c.total = max(len(c.alts), 1)
		} else {
			c.alts = []refAlt{{t, 1, 1}}
		}

		clauses = append(clauses, c)
	}

	for _, key := range queryKeys(q, terms) {
		clauses = append(clauses, refClause{field: termPhonetic, boost: boostPhon, alts: []refAlt{{key, 1, 1}}, total: 1})
	}

	n := float64(len(docs))
	run := func(clauses []refClause) []scoredID {
		var out []scoredID

		for _, d := range docs {
			if len(kinds) > 0 && !slices.Contains(kinds, d.kind) {
				continue
			}

			sum, matched := 0.0, 0

			for _, c := range clauses {
				cs, cm := 0.0, 0

				for _, a := range c.alts {
					if tf := d.tf[c.field][a.term]; tf > 0 {
						idf := 1 + math.Log(n/float64(df[c.field][a.term]+1))
						cs += float64(a.count) * c.boost * a.weight * idf * idf * math.Sqrt(float64(tf)) * d.norm[c.field]
						cm += a.count
					}
				}

				if cm > 0 {
					sum += cs * float64(cm) / float64(c.total)
					matched++
				}
			}

			if matched == 0 {
				continue
			}

			score := sum * float64(matched) / float64(len(clauses))

			if len(kinds) > 0 {
				same := 0
				for _, k := range kinds {
					if k == d.kind {
						same++
					}
				}

				idf := 1 + math.Log(n/float64(kindDocs[d.kind]+1))
				score += float64(same) * idf * idf * float64(same) / float64(len(kinds))
			}

			out = append(out, scoredID{d.id, score})
		}

		slices.SortStableFunc(out, func(a, b scoredID) int { return compareFloat(b.score, a.score) })

		return out
	}

	hits := run(clauses)
	if len(hits) >= min(k, ngramFallbackHits) {
		return hits
	}

	grams := appendTrigrams(nil, q.compact)
	c := refClause{field: termTrigrams, boost: boostNgram, total: len(grams)}

	for _, g := range grams {
		c.alts = append(c.alts, refAlt{g, 1, 1})
	}

	return run(append(clauses, c))
}

// assertSameRanking checks that got, a top k, is a valid top k of want, a full ranking: the same scores rank by rank, up to a constant factor, and every Doc in got has its score in want. Docs with equal scores may come in any order, and either side may break a tie at the cut-off differently.
func assertSameRanking(t require.TestingT, want, got []scoredID, k int, msgAndArgs ...any) {
	if h, ok := t.(interface{ Helper() }); ok {
		h.Helper()
	}

	require.Len(t, got, min(k, len(want)), msgAndArgs...)

	if len(got) == 0 {
		return
	}

	scale := want[0].score / got[0].score
	wantScore := make(map[string]float64, len(want))

	for _, w := range want {
		wantScore[w.id] = w.score
	}

	seen := make(map[string]bool, len(got))

	for i, g := range got {
		require.False(t, seen[g.id], "%s returned twice %v", g.id, msgAndArgs)
		seen[g.id] = true

		require.True(t, closeScores(g.score*scale, want[i].score), "rank %d: score %v (%s), want %v (%s) %v", i, g.score*scale, g.id, want[i].score, want[i].id, msgAndArgs)

		ws, ok := wantScore[g.id]
		require.True(t, ok, "rank %d: %s is not a match at all %v", i, g.id, msgAndArgs)
		require.True(t, closeScores(g.score*scale, ws), "rank %d: %s scores %v, want %v %v", i, g.id, g.score*scale, ws, msgAndArgs)
	}
}

func closeScores(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9*math.Max(math.Abs(a), math.Abs(b))
}

// diffWords feeds generated libraries: common words, stop words, numbers, words that sound alike, repeated syllables, accented, Cyrillic, Greek, Hebrew and Arabic words, very short and very long words. Scripts written without spaces are covered by TestInvertedKeepsUnsegmentedScripts.
var diffWords = []string{
	"the", "a", "of", "and", "in", "to", "love", "lover", "lovers", "loves", "night", "knight", "nite", "star", "stars", "start", "wars", "war",
	"dune", "dunes", "done", "rocky", "rock", "seinfeld", "seinfield", "bluey", "blue", "y", "x", "ab", "ii", "iv", "2", "21", "1999", "2049",
	"na", "la", "nanana", "lalala", "ba", "peppa", "pepper", "phone", "fone", "kat", "cat", "catt", "act", "tac",
	"café", "über", "brücke", "ñandú", "øresund", "straße", "garçon",
	"привет", "мир", "война", "москва", "αθήνα", "οδύσσεια", "שלום", "سلام",
	"supercalifragilisticexpialidocious", "pneumonoultramicroscopicsilicovolcanoconiosisandthensomemorelettersuntilitislong",
}

var diffKinds = []media.Kind{media.KindMovie, media.KindTVShow, media.KindSong, media.KindAlbum}

func genDiffTitle() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		switch rapid.IntRange(0, 9).Draw(t, "shape") {
		case 0:
			// A very long title.
			return strings.Join(rapid.SliceOfN(rapid.SampledFrom(diffWords), 20, 60).Draw(t, "long"), " ")
		case 1:
			// One syllable repeated, whose trigrams repeat many times.
			return strings.Repeat(rapid.SampledFrom([]string{"na ", "la ", "da"}).Draw(t, "syllable"), rapid.IntRange(1, 40).Draw(t, "repeats"))
		case 2:
			// Punctuation and case that normalisation removes, sometimes leaving nothing.
			return rapid.StringMatching(`[A-Za-z!?.' -]{0,6}`).Draw(t, "odd")
		default:
			return strings.Join(rapid.SliceOfN(rapid.SampledFrom(diffWords), 1, 6).Draw(t, "words"), " ")
		}
	})
}

func genDiffDoc() *rapid.Generator[Doc] {
	return rapid.Custom(func(t *rapid.T) Doc {
		d := Doc{
			ID:    fmt.Sprintf("d%d", rapid.IntRange(0, 79).Draw(t, "id")),
			Kind:  rapid.SampledFrom(diffKinds).Draw(t, "kind"),
			Title: genDiffTitle().Draw(t, "title"),
		}

		if rapid.IntRange(0, 4).Draw(t, "aliased") == 0 {
			d.Aliases = []string{genDiffTitle().Draw(t, "alias")}
		}

		return d
	})
}

// genDiffQuery derives a query from a title the way speech recognition gets it wrong, or makes one up.
func genDiffQuery(titles []string) *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		if len(titles) == 0 || rapid.IntRange(0, 4).Draw(t, "made-up") == 0 {
			return genDiffTitle().Draw(t, "query")
		}

		rs := []rune(strings.ToLower(rapid.SampledFrom(titles).Draw(t, "title")))
		if len(rs) == 0 {
			return "x"
		}

		i := rapid.IntRange(0, len(rs)-1).Draw(t, "at")

		switch rapid.IntRange(0, 5).Draw(t, "edit") {
		case 0:
			rs[i] = rapid.RuneFrom([]rune("abcdeiklnorstyz")).Draw(t, "rune")
		case 1:
			if i+1 < len(rs) {
				rs[i], rs[i+1] = rs[i+1], rs[i]
			}
		case 2:
			rs = slices.Delete(rs, i, i+1)
		case 3:
			rs = slices.DeleteFunc(rs, func(r rune) bool { return r == ' ' })
		case 4:
			if w := strings.Fields(string(rs)); len(w) > 1 {
				rs = []rune(strings.Join(w[:len(w)-1], " "))
			}
		}

		return string(rs)
	})
}

func genDiffKinds() *rapid.Generator[[]media.Kind] {
	return rapid.Custom(func(t *rapid.T) []media.Kind {
		if rapid.IntRange(0, 2).Draw(t, "filtered") > 0 {
			return nil
		}

		// Repeats are allowed: they weigh that kind more.
		return rapid.SliceOfN(rapid.SampledFrom(diffKinds), 1, 3).Draw(t, "kinds")
	})
}

// diffLibrary is a generated library after a run of upserts and deletes.
type diffLibrary struct {
	lang *language
	inv  *invertedRetriever
	// final is the live Docs, in the order they were last written.
	final  []*entry
	titles []string
}

func genDiffLibrary(t *rapid.T, lang string) diffLibrary {
	l := languageFor(lang)
	lib := diffLibrary{lang: l, inv: newInvertedRetriever(l)}

	var order []string

	byID := make(map[string]*entry)
	write := func(docs []Doc) {
		entries := make([]*entry, len(docs))
		for i, d := range docs {
			entries[i] = l.newEntry(d)
			byID[d.ID] = entries[i]
			order = append(slices.DeleteFunc(order, func(id string) bool { return id == d.ID }), d.ID)
			lib.titles = append(lib.titles, d.Title)
		}

		require.NoError(t, lib.inv.upsert(entries))
	}

	write(rapid.SliceOfN(genDiffDoc(), 0, 60).Draw(t, "docs"))

	for range rapid.IntRange(0, 8).Draw(t, "ops") {
		if rapid.Bool().Draw(t, "delete") {
			ids := rapid.SliceOfN(rapid.Map(rapid.IntRange(0, 85), func(i int) string { return fmt.Sprintf("d%d", i) }), 1, 6).Draw(t, "deleted")
			require.NoError(t, lib.inv.remove(ids))

			for _, id := range ids {
				delete(byID, id)
				order = slices.DeleteFunc(order, func(o string) bool { return o == id })
			}

			continue
		}

		write(rapid.SliceOfN(genDiffDoc(), 1, 6).Draw(t, "updated"))
	}

	for _, id := range order {
		lib.final = append(lib.final, byID[id])
	}

	return lib
}

// TestPropInvertedMatchesReference checks the inverted index, after any run of upserts and deletes, against the reference scorer over the Docs left.
func TestPropInvertedMatchesReference(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lib := genDiffLibrary(t, rapid.SampledFrom(langs).Draw(t, "lang"))
		docs := refDocs(lib.lang, lib.final)

		for i := range rapid.IntRange(1, 6).Draw(t, "queries") {
			text := genDiffQuery(lib.titles).Draw(t, fmt.Sprint("q", i))

			qf := lib.lang.newForm(lib.lang.normalizeSpelled(text, false))
			if len(qf.toks) == 0 {
				continue
			}

			kinds := genDiffKinds().Draw(t, fmt.Sprint("kinds", i))
			k := rapid.SampledFrom([]int{1, 3, 10, 25, 200}).Draw(t, fmt.Sprint("k", i))

			assertSameRanking(t, refRanking(lib.lang, docs, &qf, kinds, k), invertedRanking(lib.inv, &qf, kinds, k), k, text, kinds)
		}
	})
}

// TestInvertedCompaction deletes and replaces enough Docs to compact the index several times, checking it against the reference scorer and against an index built from scratch.
func TestInvertedCompaction(t *testing.T) {
	l := languageFor("en")
	docs := syntheticDocs(3000, 11)
	inv := newInvertedRetriever(l)

	byID := make(map[string]*entry, len(docs))
	order := make([]string, 0, len(docs))

	upsert := func(ds ...Doc) {
		entries := make([]*entry, len(ds))
		for i, d := range ds {
			entries[i] = l.newEntry(d)
			if _, ok := byID[d.ID]; !ok {
				order = append(order, d.ID)
			}

			byID[d.ID] = entries[i]
		}

		require.NoError(t, inv.upsert(entries))
	}

	upsert(docs...)

	compactions := 0

	for round := range 6 {
		var gone []string

		for i := round; i < len(docs); i += 3 {
			gone = append(gone, docs[i].ID)
		}

		dead := inv.dead
		require.NoError(t, inv.remove(gone))
		inv.wg.Wait()

		if inv.dead < dead+len(gone) {
			compactions++
		}

		for _, id := range gone {
			delete(byID, id)
		}

		order = slices.DeleteFunc(order, func(id string) bool { _, ok := byID[id]; return !ok })

		// Bring them back retitled, so the same IDs come back with different words.
		for i := round; i < len(docs); i += 3 {
			d := docs[(i+1)%len(docs)]
			d.ID = docs[i].ID
			upsert(d)
		}
	}

	require.Positive(t, compactions, "no compaction ran")
	require.Equal(t, len(order), inv.live)

	final := make([]*entry, len(order))
	for i, id := range order {
		final[i] = byID[id]
	}

	fresh := newInvertedRetriever(l)
	require.NoError(t, fresh.upsert(final))

	for f := range termFields {
		live := 0
		for _, n := range inv.fields[f].live {
			if n > 0 {
				live++
			}
		}

		assert.Equal(t, len(fresh.fields[f].terms), live, "field %d vocabulary", f)
	}

	ref := refDocs(l, final)

	for i, d := range docs[:300] {
		qf := l.newForm(l.normalizeSpelled(d.Title, false))
		if i%2 == 1 && len(d.Title) > 3 {
			// A typo: the second letter dropped.
			qf = l.newForm(l.normalizeSpelled(d.Title[:1]+d.Title[2:], false))
		}

		want := refRanking(l, ref, &qf, nil, 200)
		assertSameRanking(t, want, invertedRanking(inv, &qf, nil, 200), 200, d.Title)
		assertSameRanking(t, want, invertedRanking(fresh, &qf, nil, 200), 200, d.Title)
	}
}

// TestInvertedKeepsUnsegmentedScripts covers titles in scripts written without spaces, which segmenting words by Unicode rules, as bleve did, splits into single characters or drops: the inverted index keeps each normalised word whole, so a near miss still retrieves them.
func TestInvertedKeepsUnsegmentedScripts(t *testing.T) {
	docs := []Doc{
		{ID: "spirited", Kind: media.KindMovie, Title: "千と千尋の神隠し"},
		{ID: "thai", Kind: media.KindSong, Title: "สวัสดีชาวโลก"},
		{ID: "kana", Kind: media.KindSong, Title: "ひらがなとカタカナ"},
		{ID: "filler", Kind: media.KindMovie, Title: "Inception"},
	}

	cases := map[string]string{
		"千と千尋の神隠":     "spirited",
		"สวัสดีชาวโล": "thai",
		"ひらがなとカタカ":    "kana",
	}

	l := languageFor("en")
	inv := newInvertedRetriever(l)

	entries := make([]*entry, len(docs))
	for i, d := range docs {
		entries[i] = l.newEntry(d)
	}

	require.NoError(t, inv.upsert(entries))

	for text, want := range cases {
		qf := l.newForm(l.normalizeSpelled(text, false))
		ids, err := inv.candidates(t.Context(), &qf, nil, 10)
		require.NoError(t, err)
		assert.Contains(t, ids, want, text)
	}
}

// TestInvertedConcurrentUse searches while Docs are replaced and deleted fast enough to compact the index under the readers, as library syncs do while Alexa requests arrive. A Doc nobody touches must stay the top candidate for its own title throughout.
func TestInvertedConcurrentUse(t *testing.T) {
	base := syntheticDocs(3000, 5)
	ix := newIndex(t, RetrieverInverted, append(slices.Clone(base), movie("anchor", "Inception", 2010))...)

	var (
		wg   sync.WaitGroup
		stop = make(chan struct{})
	)

	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}

				qf := ix.lang.newForm(ix.lang.normalizeSpelled("inception", false))

				ids, err := ix.ret.candidates(t.Context(), &qf, nil, ix.cfg.CandidatePool)
				if err != nil {
					t.Error(err)

					return
				}

				if !assert.NotEmpty(t, ids) || !assert.Equal(t, "anchor", ids[0]) {
					return
				}

				res, err := ix.Search(t.Context(), Query{Text: "starnight", Kinds: []media.Kind{media.KindMovie, media.KindAlbum}})
				if err != nil {
					t.Error(err)

					return
				}

				for _, c := range res.Candidates {
					assert.Contains(t, []media.Kind{media.KindMovie, media.KindAlbum}, c.Doc.Kind)
				}
			}
		})
	}

	inv, ok := ix.ret.(*invertedRetriever)
	require.True(t, ok)

	compactions := 0

	for round := range 8 {
		retitled := syntheticDocs(len(base), uint64(100+round))

		var (
			ids  []string
			back []Doc
		)

		for i := round % 2; i < len(base); i += 2 {
			ids = append(ids, base[i].ID)

			d := base[i]
			d.Title = retitled[i].Title
			back = append(back, d)
		}

		dead := inv.deadSlots()
		require.NoError(t, ix.Delete(ids...))
		inv.wg.Wait()

		if inv.deadSlots() < dead+len(ids) {
			compactions++
		}

		require.NoError(t, ix.Upsert(back...))
	}

	close(stop)
	wg.Wait()

	assert.Positive(t, compactions, "no compaction ran")
	assert.Equal(t, 3001, ix.Len())
}

func (r *invertedRetriever) deadSlots() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.dead
}

func TestPropOSAWithin(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := genSimilarityText().Draw(t, "a")
		b := genSimilarityText().Draw(t, "b")
		maxD := rapid.IntRange(0, 3).Draw(t, "max")

		want := osaDistance(a, b)
		if want > maxD {
			want = -1
		}

		assert.Equal(t, want, osaWithin([]rune(a), b, maxD), "osaWithin(%q, %q, %d)", a, b, maxD)
	})
}

// TestInvertedVeryFrequentTerms indexes titles that repeat a trigram more often than a posting can count, and more often than the square-root table covers, and checks their scores against the reference before and after a compaction moves them to new slots.
func TestInvertedVeryFrequentTerms(t *testing.T) {
	l := languageFor("en")
	inv := newInvertedRetriever(l)
	m := newInvModel()

	docs := []Doc{
		{ID: "few", Kind: media.KindSong, Title: strings.Repeat("na ", 12)},
		{ID: "many", Kind: media.KindSong, Title: strings.Repeat("na ", 40)},
		{ID: "most", Kind: media.KindSong, Title: strings.Repeat("na ", 100)},
		{ID: "gone", Kind: media.KindSong, Title: strings.Repeat("na ", 90)},
		{ID: "other", Kind: media.KindMovie, Title: "Banana Republic"},
	}

	es := entriesOf(l, docs)
	require.NoError(t, inv.upsert(es))
	m.upsert(es)

	require.NoError(t, inv.remove([]string{"gone"}))
	m.remove([]string{"gone"})

	queries := []string{"nanana", "na na na na", "banana"}
	checkRankings(t, inv, m, queries)

	inv.mu.Lock()
	c := inv.beginCompaction()
	inv.mu.Unlock()
	inv.compactInto(c)

	require.Zero(t, inv.dead)
	checkInverted(t, inv, m)
	checkRankings(t, inv, m, queries)
}

// TestInvertedNoCandidatesWanted asks for zero candidates, which the retriever contract allows.
func TestInvertedNoCandidatesWanted(t *testing.T) {
	l := languageFor("en")
	inv := newInvertedRetriever(l)
	require.NoError(t, inv.upsert(entriesOf(l, []Doc{movie("m", "Inception", 2010)})))

	qf := l.newForm(l.normalizeSpelled("inception", false))
	ids, err := inv.candidates(t.Context(), &qf, nil, 0)
	require.NoError(t, err)
	assert.Empty(t, ids)
}
