package match

import (
	"fmt"
	"math/rand/v2"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
)

var benchSyllables = []string{
	"ka", "lo", "ver", "sein", "feld", "ro", "cky", "star", "war", "night", "ma", "tri", "x", "ban", "dit", "to", "ry",
	"gel", "dark", "ness", "lord", "ring", "sil", "ence", "lamb", "god", "fa", "thur", "mo", "nster", "in", "ter", "stel", "lar",
}

func syntheticDocs(n int, seed uint64) []Doc {
	r := rand.New(rand.NewPCG(seed, seed))

	vocab := make([]string, 8000)
	for i := range vocab {
		var b strings.Builder
		for range 1 + r.IntN(3) {
			b.WriteString(benchSyllables[r.IntN(len(benchSyllables))])
		}

		vocab[i] = b.String()
	}

	kinds := []media.Kind{media.KindMovie, media.KindTVShow, media.KindArtist, media.KindAlbum}
	docs := make([]Doc, n)

	for i := range docs {
		w := make([]string, 1+r.IntN(5))
		for j := range w {
			w[j] = vocab[r.IntN(len(vocab))]
		}

		if r.IntN(10) == 0 {
			w = append([]string{"the"}, w...)
		}

		k := kinds[r.IntN(len(kinds))]
		docs[i] = Doc{ID: fmt.Sprintf("bench/%s/%d", k, i), Kind: k, Ref: media.Ref{Kind: k, ID: i}, Title: strings.Join(w, " "), Year: 1950 + r.IntN(75)}
	}

	return docs
}

func heapMB() float64 {
	runtime.GC()
	runtime.GC()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return float64(m.HeapAlloc) / 1e6
}

// BenchmarkSearch100k measures Search over 100k synthetic titles with misspelled queries, reporting latency percentiles and the index's heap footprint.
func BenchmarkSearch100k(b *testing.B) {
	docs := syntheticDocs(100_000, 1)
	docs[4242].Title = "Seinfeld"

	before := heapMB()
	start := time.Now()

	ix, err := New("en", Config{})
	require.NoError(b, err)

	defer ix.Close()

	require.NoError(b, ix.Upsert(docs...))

	b.Logf("indexed %d docs in %v, heap +%.1f MB", ix.Len(), time.Since(start).Round(time.Millisecond), heapMB()-before)

	// Queries: exact titles, one-letter typos, partial titles and a known mishearing.
	r := rand.New(rand.NewPCG(2, 2))
	queries := []string{"seinfield"}

	for range 200 {
		t := docs[r.IntN(len(docs))].Title
		switch r.IntN(3) {
		case 0:
			queries = append(queries, t)
		case 1:
			rs := []rune(t)
			i := r.IntN(len(rs))
			rs[i] = 'a' + rune(r.IntN(26))
			queries = append(queries, string(rs))
		default:
			queries = append(queries, strings.Fields(t)[0])
		}
	}

	ctx := b.Context()
	lat := make([]time.Duration, 0, b.N)

	b.ReportAllocs()
	b.ResetTimer()

	for i := range b.N {
		t := time.Now()

		if _, err := ix.Search(ctx, Query{Text: queries[i%len(queries)]}); err != nil {
			b.Fatal(err)
		}

		lat = append(lat, time.Since(t))
	}

	b.StopTimer()

	slices.Sort(lat)
	pct := func(p float64) float64 { return float64(lat[int(p*float64(len(lat)-1))].Microseconds()) / 1000 }
	b.ReportMetric(pct(0.5), "p50-ms")
	b.ReportMetric(pct(0.95), "p95-ms")
	b.ReportMetric(pct(0.99), "p99-ms")
}

// BenchmarkInvertedCompact deletes a quarter of a scale library (ENCORE_MATCH_BENCH_SCALE, default 100000) and waits for the compaction that triggers, while a reader keeps retrieving candidates. pause-ms is the longest the reader waited for one retrieval meanwhile, read-ms its median retrieval on the idle index, and write-ms how long the delete took to return.
func BenchmarkInvertedCompact(b *testing.B) {
	suite := englishSuite(b)
	l := languageFor("en")
	qf := l.newForm(l.normalizeSpelled("starlight lover", false))

	for _, n := range scaleSizes(b, "ENCORE_MATCH_BENCH_SCALE", []int{100_000}) {
		docs := scaleDocs(n, suite.docs(), 1)

		entries := make([]*entry, len(docs))
		for i, d := range docs {
			entries[i] = l.newEntry(d)
		}

		gone := make([]string, 0, n/4+1)
		for i := 0; i < n; i += 4 {
			gone = append(gone, docs[i].ID)
		}

		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			var pauses, idle, writes []time.Duration

			for b.Loop() {
				b.StopTimer()

				inv := newInvertedRetriever(l)
				require.NoError(b, inv.upsert(entries))
				runtime.GC()

				for range 20 {
					t := time.Now()
					_, err := inv.candidates(b.Context(), &qf, nil, 200)
					require.NoError(b, err)

					idle = append(idle, time.Since(t))
				}

				stop := make(chan struct{})
				worst := make(chan time.Duration)

				go func() {
					var w time.Duration

					for {
						select {
						case <-stop:
							worst <- w

							return
						default:
						}

						t := time.Now()
						if _, err := inv.candidates(b.Context(), &qf, nil, 200); err != nil {
							b.Error(err)
						}

						w = max(w, time.Since(t))
					}
				}()

				b.StartTimer()

				t := time.Now()

				require.NoError(b, inv.remove(gone))

				writes = append(writes, time.Since(t))

				inv.wg.Wait()

				b.StopTimer()
				close(stop)

				pauses = append(pauses, <-worst)

				require.Less(b, inv.deadSlots(), len(gone), "no compaction ran")
				b.StartTimer()
			}

			b.ReportMetric(float64(percentile(pauses, 0.5).Microseconds())/1000, "pause-ms")
			b.ReportMetric(float64(percentile(idle, 0.5).Microseconds())/1000, "read-ms")
			b.ReportMetric(float64(percentile(writes, 0.5).Microseconds())/1000, "write-ms")
		})
	}
}

// BenchmarkScore measures scoring alone: misspelled queries against 1000 synthetic entries, without retrieval.
func BenchmarkScore(b *testing.B) {
	ix, err := New("en", Config{})
	require.NoError(b, err)

	defer ix.Close()

	docs := syntheticDocs(1000, 4)
	entries := make([]*entry, len(docs))

	for i, d := range docs {
		entries[i] = ix.lang.newEntry(d)
	}

	queries := make([]form, 0, 20)
	for _, d := range docs[:20] {
		rs := []rune(d.Title)
		rs[len(rs)/2] = 'q'
		queries = append(queries, ix.lang.newForm(ix.lang.normalizeSpelled(string(rs), false)))
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := range b.N {
		q := &queries[i%len(queries)]
		for _, e := range entries {
			ix.scoreEntry(q, e)
		}
	}
}
