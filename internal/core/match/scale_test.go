package match

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
)

// The scale harness compares candidate retrievers on large synthetic libraries that embed the English ASR corpus. It is opt-in because a full run takes minutes:
//
//	ENCORE_MATCH_SCALE=10000,100000,500000 go test ./internal/core/match -run TestRetrieverScale -v -timeout 1h
//
// ENCORE_MATCH_RETRIEVERS limits which retrievers run (comma-separated names from scaleRetrievers), and ENCORE_MATCH_POOLS lists candidate pool sizes to compare (default Config.CandidatePool).

// scaleRetrievers builds each retriever the harness compares against exhaustive scoring.
var scaleRetrievers = map[string]func(*language) (retriever, error){
	RetrieverInverted: func(l *language) (retriever, error) { return newInvertedRetriever(l), nil },
	RetrieverTrigram:  func(*language) (retriever, error) { return newTrigramRetriever(), nil },
}

// exhaustiveRetriever hands every Doc to the scorer, so its results are the best the scorer can do: the reference the real retrievers are measured against.
type exhaustiveRetriever struct {
	mu    sync.RWMutex
	kinds map[string]media.Kind
}

func (r *exhaustiveRetriever) upsert(entries []*entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, e := range entries {
		r.kinds[e.doc.ID] = e.doc.Kind
	}

	return nil
}

func (r *exhaustiveRetriever) remove(ids []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, id := range ids {
		delete(r.kinds, id)
	}

	return nil
}

func (r *exhaustiveRetriever) candidates(_ context.Context, _ *form, kinds []media.Kind, _ int) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.kinds))
	for id, k := range r.kinds {
		if len(kinds) == 0 || slices.Contains(kinds, k) {
			ids = append(ids, id)
		}
	}

	return ids, nil
}

func (r *exhaustiveRetriever) close() error { return nil }

// scaleWords are common title words, drawn with a Zipf distribution so that some postings are very long, as in real libraries.
var scaleWords = strings.Fields(`the of a and in to love night man day life world girl time my you me i is it on your house dark last king blue home little heart city
dead black red one two three story new war lost all with from for at by be we no star part live big good dream fire light sun moon water death
blood shadow ghost game boy song back way out up down long road world queen lady lord mister doctor return rise fall secret return land sea sky
river mountain summer winter spring autumn christmas american great best end beginning first second final edge kill killer hunter run stay
under over beyond after before inside outside without forever never always again tonight tomorrow yesterday golden silver iron steel stone
wild free lonely happy sad crazy wonderful beautiful sweet bitter hot cold fast slow high low deep hidden open broken perfect true false real
angel devil heaven hell paradise kingdom empire legend hero monster dragon wolf tiger bird horse dog cat baby child mother father brother sister
family friends lovers stranger soldier rebel prince princess warrior spirit soul mind body eyes face hand voice words music dance party rock
blues soul jazz radio movie show live tour volume chapter episode season edition remastered deluxe acoustic version remix mix original theme`)

// scaleSyllables build name-like words, so the vocabulary also has a long tail of rare terms.
var scaleSyllables = []string{
	"ka", "lo", "ver", "sein", "feld", "ro", "cky", "star", "war", "night", "ma", "tri", "x", "ban", "dit", "to", "ry", "gel", "dark", "ness", "lord",
	"ring", "sil", "ence", "lamb", "god", "fa", "thur", "mo", "nster", "in", "ter", "stel", "lar", "be", "ach", "ton", "son", "ber", "gen", "man",
	"el", "ley", "zo", "qui", "phi", "chel", "ny", "ash", "ford", "wick", "ham", "dra", "ken", "jo", "vi", "an", "ca", "mi", "la", "ri", "us", "ia",
}

var scaleKinds = []struct {
	kind   media.Kind
	weight int
}{
	{media.KindSong, 50}, {media.KindAlbum, 14}, {media.KindArtist, 8}, {media.KindEpisode, 12}, {media.KindMovie, 10}, {media.KindTVShow, 4}, {media.KindMusicVideo, 2},
}

// scaleDocs returns n Docs: the English ASR corpus library plus synthetic titles, deterministic for a seed.
func scaleDocs(n int, lib []Doc, seed uint64) []Doc {
	r := rand.New(rand.NewPCG(seed, seed^0x5ca1e))

	vocab := make([]string, 40_000)
	for i := range vocab {
		var b strings.Builder
		for range 2 + r.IntN(3) {
			b.WriteString(scaleSyllables[r.IntN(len(scaleSyllables))])
		}

		vocab[i] = b.String()
	}

	common := rand.NewZipf(r, 1.1, 2, uint64(len(scaleWords)-1))
	rare := rand.NewZipf(r, 1.05, 4, uint64(len(vocab)-1))

	word := func() string {
		if r.IntN(100) < 45 {
			return scaleWords[common.Uint64()]
		}

		return vocab[rare.Uint64()]
	}

	title := func() string {
		w := make([]string, 1+r.IntN(r.IntN(6)+1))
		for i := range w {
			w[i] = word()
		}

		return strings.Join(w, " ")
	}

	total := 0
	for _, k := range scaleKinds {
		total += k.weight
	}

	docs := slices.Clone(lib)
	for i := len(docs); i < n; i++ {
		pick, kind := r.IntN(total), scaleKinds[0].kind
		for _, k := range scaleKinds {
			if pick < k.weight {
				kind = k.kind

				break
			}

			pick -= k.weight
		}

		d := Doc{ID: fmt.Sprintf("scale/%s/%d", kind, i), Kind: kind, Ref: media.Ref{Kind: kind, ID: i}, Title: title(), Year: 1950 + r.IntN(75)}
		if r.IntN(20) == 0 {
			d.Aliases = []string{title()}
		}

		docs = append(docs, d)
	}

	return docs
}

// scaleQuery is a query derived from a known Doc by one kind of mishearing.
type scaleQuery struct {
	text   string
	kind   string
	target string
}

// scaleMishearings rewrite one word the way speech recognition tends to get it wrong.
var scaleMishearings = [][2]string{
	{"ph", "f"}, {"ck", "k"}, {"c", "k"}, {"ee", "ea"}, {"s", "z"}, {"ou", "ow"}, {"y", "ie"}, {"er", "a"}, {"th", "f"}, {"ll", "l"}, {"x", "ks"}, {"qu", "kw"}, {"i", "ee"},
}

func scaleQueries(docs []Doc, n int, seed uint64) []scaleQuery {
	r := rand.New(rand.NewPCG(seed, seed^0xa5))
	kinds := []string{"exact", "typo", "first-letter", "sound-alike", "merged", "dropped-word", "first-word"}
	out := make([]scaleQuery, 0, n)

	for len(out) < n {
		d := docs[r.IntN(len(docs))]
		w := strings.Fields(strings.ToLower(d.Title))
		kind := kinds[len(out)%len(kinds)]
		i := r.IntN(len(w))

		switch kind {
		case "typo", "first-letter":
			rs := []rune(w[i])
			if len(rs) < 4 {
				continue
			}

			at := 1 + r.IntN(len(rs)-1)
			if kind == "first-letter" {
				at = 0
			}

			rs[at] = 'a' + rune(r.IntN(26))
			w[i] = string(rs)
		case "sound-alike":
			hit := false

			for _, j := range r.Perm(len(scaleMishearings)) {
				if m := scaleMishearings[j]; strings.Contains(w[i], m[0]) {
					w[i], hit = strings.Replace(w[i], m[0], m[1], 1), true

					break
				}
			}

			if !hit {
				continue
			}
		case "merged":
			if len(w) < 2 {
				continue
			}

			i = r.IntN(len(w) - 1)
			w = slices.Concat(w[:i], []string{w[i] + w[i+1]}, w[i+2:])
		case "dropped-word":
			if len(w) < 3 {
				continue
			}

			i = 1 + r.IntN(len(w)-1)
			w = slices.Delete(w, i, i+1)
		case "first-word":
			if len(w) < 2 {
				continue
			}

			w = w[:1]
		}

		out = append(out, scaleQuery{text: strings.Join(w, " "), kind: kind, target: d.ID})
	}

	return out
}

// newScaleIndex builds an English Index over docs with the named retriever, or with exhaustive scoring when name is empty.
func newScaleIndex(tb testing.TB, name string, docs []Doc) (*Index, time.Duration) {
	tb.Helper()

	ix, err := New("en", Config{Retriever: RetrieverTrigram})
	require.NoError(tb, err)

	ix.ret = &exhaustiveRetriever{kinds: make(map[string]media.Kind)}

	if name != "" {
		build, ok := scaleRetrievers[name]
		require.True(tb, ok, "unknown retriever %q", name)

		ix.ret, err = build(ix.lang)
		require.NoError(tb, err)
	}

	start := time.Now()
	require.NoError(tb, ix.Upsert(docs...))

	return ix, time.Since(start)
}

// memMB returns the live heap and the resident set, after returning freed memory to the OS. RSS also counts memory outside the Go heap, such as SQLite's.
func memMB() (heap, rss float64) {
	runtime.GC()
	debug.FreeOSMemory()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	if raw, err := os.ReadFile("/proc/self/statm"); err == nil {
		if f := strings.Fields(string(raw)); len(f) > 1 {
			pages, _ := strconv.Atoi(f[1])
			rss = float64(pages*os.Getpagesize()) / 1e6
		}
	}

	return float64(m.HeapAlloc) / 1e6, rss
}

func percentile(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}

	s := slices.Clone(d)
	slices.Sort(s)

	return s[int(p*float64(len(s)-1))]
}

// outcome is what a Search decided and which Docs it ranked first.
type outcome struct {
	decision Decision
	top      []string
}

func outcomes(results []Result) []outcome {
	out := make([]outcome, len(results))
	for i, res := range results {
		out[i].decision = res.Decision
		for _, c := range res.Candidates {
			out[i].top = append(out[i].top, c.Doc.ID)
		}
	}

	return out
}

// searchAll runs up to par queries at a time and returns their results in order.
func searchAll(tb testing.TB, ix *Index, queries []Query, par int) []Result {
	tb.Helper()

	out := make([]Result, len(queries))

	var wg sync.WaitGroup

	sem := make(chan struct{}, par)

	for i := range queries {
		wg.Add(1)
		sem <- struct{}{}

		go func() {
			defer func() { <-sem; wg.Done() }()

			res, err := ix.Search(context.Background(), queries[i])
			if err != nil {
				tb.Error(err)

				return
			}

			// Candidates shares its array with every scored Doc; exhaustive results would otherwise keep each query's whole library alive.
			res.Candidates = slices.Clone(res.Candidates)
			out[i] = res
		}()
	}

	wg.Wait()

	return out
}

// referenceResults scores queries exhaustively. Exhaustive scoring takes minutes at 500k Docs, so results are cached in ENCORE_MATCH_SCALE_CACHE when it is set; the key covers the harness inputs but not the scorer, so clear the cache after changing scoring.
func referenceResults(tb testing.TB, docs []Doc, queries []Query) []Result {
	tb.Helper()

	var path string

	if dir := os.Getenv("ENCORE_MATCH_SCALE_CACHE"); dir != "" {
		h := fnv.New64a()
		for _, d := range docs {
			fmt.Fprintln(h, d.ID, d.Title, d.Aliases, d.Year)
		}

		for _, q := range queries {
			fmt.Fprintln(h, q.Text, q.Kinds)
		}

		path = filepath.Join(dir, fmt.Sprintf("reference-%d-%x.gob", len(docs), h.Sum64()))

		if raw, err := os.ReadFile(path); err == nil {
			var out []Result
			if err := gob.NewDecoder(bytes.NewReader(raw)).Decode(&out); err == nil {
				return out
			}
		}
	}

	ref, build := newScaleIndex(tb, "", docs)
	start := time.Now()
	// Each exhaustive Search holds a Candidate per Doc, so large libraries get fewer at a time.
	out := searchAll(tb, ref, queries, max(1, min(runtime.GOMAXPROCS(0), 2_000_000/len(docs))))
	require.NoError(tb, ref.Close())
	tb.Logf("n=%d: exhaustive reference built in %v, scored %d queries in %v", len(docs), build.Round(time.Millisecond), len(queries), time.Since(start).Round(time.Millisecond))

	if path != "" {
		var b bytes.Buffer
		require.NoError(tb, gob.NewEncoder(&b).Encode(out))
		require.NoError(tb, os.WriteFile(path, b.Bytes(), 0o600))
	}

	return out
}

// asrScore counts the corpus cases decided correctly and the wrong plays among results, which line up with cases.
func asrScore(cases []corpusCase, results []Result) (correct, wrongPlays int) {
	for i, c := range cases {
		res := results[i]
		if c.check(res) == "" {
			correct++
		}

		if best, _ := res.Best(); res.Decision == DecisionPlay && (c.Want != "play" || best.Doc.ID != c.ID) {
			wrongPlays++
		}
	}

	return correct, wrongPlays
}

func scaleSizes(tb testing.TB, env string, def []int) []int {
	tb.Helper()

	v := os.Getenv(env)
	if v == "" {
		return def
	}

	var sizes []int

	for f := range strings.SplitSeq(v, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		require.NoError(tb, err)

		sizes = append(sizes, n)
	}

	return sizes
}

func scaleRetrieverNames() []string {
	if v := os.Getenv("ENCORE_MATCH_RETRIEVERS"); v != "" {
		return strings.Split(v, ",")
	}

	names := make([]string, 0, len(scaleRetrievers))
	for n := range scaleRetrievers {
		names = append(names, n)
	}

	slices.Sort(names)

	return names
}

func englishSuite(tb testing.TB) corpusSuite {
	tb.Helper()

	for _, s := range loadCorpus(tb).Suites {
		if s.Lang == "en" {
			return s
		}
	}

	tb.Fatal("no English suite")

	return corpusSuite{}
}

// TestRetrieverScale reports, per retriever, library size and candidate pool size, build time and memory, candidate and Search latency, and how often Search agrees with exhaustive scoring. Agreement is the share of queries with the same decision and top Doc; recall@10 is the share of exhaustive scoring's top ten that the retriever's Search also returns.
func TestRetrieverScale(t *testing.T) {
	if os.Getenv("ENCORE_MATCH_SCALE") == "" {
		t.Skip("set ENCORE_MATCH_SCALE (e.g. 10000,100000) to run the retriever scale harness")
	}

	suite := englishSuite(t)
	pools := scaleSizes(t, "ENCORE_MATCH_POOLS", []int{DefaultConfig().CandidatePool})

	for _, n := range scaleSizes(t, "ENCORE_MATCH_SCALE", nil) {
		docs := scaleDocs(n, suite.docs(), 1)
		synth := scaleQueries(docs, 700, 2)

		queries := make([]Query, 0, len(synth)+len(suite.Cases))
		for _, q := range synth {
			queries = append(queries, Query{Text: q.text})
		}

		for _, c := range suite.Cases {
			queries = append(queries, c.query())
		}

		refResults := referenceResults(t, docs, queries)
		want := outcomes(refResults)
		refASR, refWrong := asrScore(suite.Cases, refResults[len(synth):])
		t.Logf("n=%d: exhaustive scoring decides %d/%d ASR corpus cases correctly with %d wrong plays", n, refASR, len(suite.Cases), refWrong)

		ix, _ := newScaleIndex(t, "", docs)
		refHeap, refRSS := memMB()
		require.NoError(t, ix.Close())

		for _, name := range scaleRetrieverNames() {
			t.Run(fmt.Sprintf("%s/n=%d", name, n), func(t *testing.T) {
				ix, build := newScaleIndex(t, name, docs)
				defer func() { require.NoError(t, ix.Close()) }()

				heap, rss := memMB()

				for _, pool := range pools {
					ix.cfg.CandidatePool = pool

					var candLat, searchLat []time.Duration

					ctx := t.Context()

					for pass := range 4 {
						for _, q := range queries {
							qf := ix.lang.newForm(ix.lang.normalizeSpelled(q.Text, false))

							t0 := time.Now()
							_, err := ix.ret.candidates(ctx, &qf, q.Kinds, ix.cfg.CandidatePool)
							require.NoError(t, err)

							t1 := time.Now()
							_, err = ix.Search(ctx, q)
							require.NoError(t, err)

							if pass > 0 {
								candLat = append(candLat, t1.Sub(t0))
								searchLat = append(searchLat, time.Since(t1))
							}
						}
					}

					results := searchAll(t, ix, queries, runtime.GOMAXPROCS(0))
					got := outcomes(results)

					agree, recall, targetTop := 0, 0.0, 0
					byKind := map[string][2]int{}

					for i := range queries {
						same := got[i].decision == want[i].decision && slices.Equal(first(got[i].top), first(want[i].top))
						if same {
							agree++
						}

						ref := want[i].top[:min(10, len(want[i].top))]
						hit := 0

						for _, id := range ref {
							if slices.Contains(got[i].top, id) {
								hit++
							}
						}

						if len(ref) > 0 {
							recall += float64(hit) / float64(len(ref))
						} else {
							recall++
						}

						kind := "asr-corpus"
						if i < len(synth) {
							kind = synth[i].kind
							if len(got[i].top) > 0 && got[i].top[0] == synth[i].target {
								targetTop++
							}
						}

						c := byKind[kind]
						c[1]++

						if same {
							c[0]++
						}

						byKind[kind] = c
					}

					asrCorrect, wrongPlays := asrScore(suite.Cases, results[len(synth):])

					kinds := make([]string, 0, len(byKind))
					for k, c := range byKind {
						kinds = append(kinds, fmt.Sprintf("%s %d/%d", k, c[0], c[1]))
					}

					slices.Sort(kinds)

					t.Logf("RESULT retriever=%s n=%d pool=%d build=%v heap=%.0fMB rss=%.0fMB cand_p50=%v cand_p99=%v search_p50=%v search_p99=%v agree=%.1f%% recall10=%.1f%% target_top=%.1f%% asr=%d/%d wrong_plays=%d",
						name, n, pool, build.Round(time.Millisecond), heap-refHeap, rss-refRSS,
						percentile(candLat, 0.5).Round(time.Microsecond), percentile(candLat, 0.99).Round(time.Microsecond),
						percentile(searchLat, 0.5).Round(time.Microsecond), percentile(searchLat, 0.99).Round(time.Microsecond),
						100*float64(agree)/float64(len(queries)), 100*recall/float64(len(queries)), 100*float64(targetTop)/float64(len(synth)),
						asrCorrect, len(suite.Cases), wrongPlays)
					t.Logf("agreement by query kind: %s", strings.Join(kinds, ", "))
				}

				// Library changes arrive a few Docs at a time, so single-Doc updates are timed last, once the index no longer needs to match the reference.
				upd := make([]time.Duration, 0, 500)

				for i := range cap(upd) {
					d := docs[(i*7919)%len(docs)]
					d.Title += " live"

					t0 := time.Now()
					require.NoError(t, ix.Upsert(d))

					upd = append(upd, time.Since(t0))
				}

				t.Logf("UPDATE retriever=%s n=%d upsert1_p50=%v upsert1_p99=%v", name, n, percentile(upd, 0.5).Round(time.Microsecond), percentile(upd, 0.99).Round(time.Microsecond))
			})
		}
	}
}

func first(ids []string) []string { return ids[:min(1, len(ids))] }

var scaleIndexes sync.Map

// BenchmarkRetrieverCandidates times candidate retrieval alone, per retriever and library size (ENCORE_MATCH_BENCH_SCALE, default 100000). Indexes are built once and shared across -count runs.
func BenchmarkRetrieverCandidates(b *testing.B) {
	suite := englishSuite(b)

	for _, n := range scaleSizes(b, "ENCORE_MATCH_BENCH_SCALE", []int{100_000}) {
		docs := scaleDocs(n, suite.docs(), 1)
		queries := scaleQueries(docs, 700, 2)

		for _, name := range scaleRetrieverNames() {
			b.Run(fmt.Sprintf("%s/n=%d", name, n), func(b *testing.B) {
				key := fmt.Sprintf("%s/%d", name, n)

				v, ok := scaleIndexes.Load(key)
				if !ok {
					ix, _ := newScaleIndex(b, name, docs)
					v, _ = scaleIndexes.LoadOrStore(key, ix)
				}

				ix, ok := v.(*Index)
				require.True(b, ok)

				forms := make([]form, len(queries))
				for i, q := range queries {
					forms[i] = ix.lang.newForm(ix.lang.normalizeSpelled(q.text, false))
				}

				ctx := b.Context()
				lat := make([]time.Duration, 0, b.N)

				b.ReportAllocs()
				b.ResetTimer()

				for i := range b.N {
					t := time.Now()
					if _, err := ix.ret.candidates(ctx, &forms[i%len(forms)], nil, ix.cfg.CandidatePool); err != nil {
						b.Fatal(err)
					}

					lat = append(lat, time.Since(t))
				}

				b.StopTimer()
				b.ReportMetric(float64(percentile(lat, 0.5).Microseconds())/1000, "p50-ms")
				b.ReportMetric(float64(percentile(lat, 0.99).Microseconds())/1000, "p99-ms")
			})
		}
	}
}
