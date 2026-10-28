package retrieval

import (
	"fmt"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/match"
)

// The benchmarks run each retriever as a sub-benchmark named impl=bleve or impl=intree (the inverted index), bleve first so that benchstat with -col /impl reports the in-tree retriever against it. Libraries are built once per process and shared across -count runs; ENCORE_BENCH_SCALE lists the library sizes for BenchmarkSearchScale and BenchmarkCandidates (default 100000).

var retrievers = []string{"bleve", "intree"}

// newIndex returns an empty English Index retrieving with the named retriever.
func newIndex(tb testing.TB, name string) *match.Index {
	tb.Helper()

	var (
		ix  *match.Index
		err error
	)

	switch name {
	case "intree":
		ix, err = match.New("en", match.Config{})
	case "bleve":
		ix, err = match.NewWithRetriever("en", match.Config{}, NewBleve("en"))
	default:
		tb.Fatalf("unknown retriever %q", name)
	}

	require.NoError(tb, err)

	return ix
}

var built sync.Map

// sharedIndex builds docs into an Index with the named retriever once per process and key.
func sharedIndex(tb testing.TB, name, key string, docs func() []match.Doc) *match.Index {
	tb.Helper()

	key = name + "/" + key
	if v, ok := built.Load(key); ok {
		ix, ok := v.(*match.Index)
		require.True(tb, ok)

		return ix
	}

	ix := newIndex(tb, name)
	require.NoError(tb, ix.Upsert(docs()...))
	built.Store(key, ix)

	return ix
}

func scaleSizes(tb testing.TB) []int {
	tb.Helper()

	v := os.Getenv("ENCORE_BENCH_SCALE")
	if v == "" {
		return []int{100_000}
	}

	var out []int

	for f := range strings.SplitSeq(v, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		require.NoError(tb, err, "ENCORE_BENCH_SCALE")

		out = append(out, n)
	}

	return out
}

// timeEach runs fn b.N times over queries, reporting the median and 99th percentile latency besides the mean.
func timeEach(b *testing.B, queries []match.Query, fn func(q match.Query) error) {
	b.Helper()

	lat := make([]time.Duration, 0, b.N)

	b.ReportAllocs()
	b.ResetTimer()

	for i := range b.N {
		t := time.Now()
		if err := fn(queries[i%len(queries)]); err != nil {
			b.Fatal(err)
		}

		lat = append(lat, time.Since(t))
	}

	b.StopTimer()
	slices.Sort(lat)

	pct := func(p float64) float64 { return float64(lat[int(p*float64(len(lat)-1))].Microseconds()) / 1000 }
	b.ReportMetric(pct(0.5), "p50-ms")
	b.ReportMetric(pct(0.99), "p99-ms")
}

// BenchmarkSearch100k times whole Searches, retrieval and scoring, over internal/core/match's BenchmarkSearch100k library and queries: 100,000 made-up titles, searched by exact titles, one-letter typos, first words and "seinfield".
func BenchmarkSearch100k(b *testing.B) {
	docs := sync.OnceValue(func() []match.Doc {
		d := syntheticDocs(100_000, 1)
		d[4242].Title = "Seinfeld"

		return d
	})

	for _, name := range retrievers {
		b.Run("impl="+name, func(b *testing.B) {
			ix := sharedIndex(b, name, "search100k", docs)
			queries := syntheticQueries(docs())
			ctx := b.Context()

			timeEach(b, queries, func(q match.Query) error {
				_, err := ix.Search(ctx, q)

				return err
			})
		})
	}
}

// scaleLibrary is the scale harness's library of n titles, which embeds the English ASR corpus, and its queries: 700 synthetic mishearings of its titles and the corpus cases.
func scaleLibrary(b *testing.B, n int) (docs func() []match.Doc, queries []match.Query) {
	b.Helper()

	suite := englishSuite(b)
	docs = sync.OnceValue(func() []match.Doc { return scaleDocs(n, suite.docs(), 1) })

	return docs, append(scaleQueries(docs(), 700, 2), suite.queries()...)
}

// BenchmarkSearchScale times whole Searches with the default Config over the scale library, cycling through its queries.
func BenchmarkSearchScale(b *testing.B) {
	for _, n := range scaleSizes(b) {
		docs, queries := scaleLibrary(b, n)

		for _, name := range retrievers {
			b.Run(fmt.Sprintf("impl=%s/n=%d", name, n), func(b *testing.B) {
				ix := sharedIndex(b, name, fmt.Sprintf("scale%d", n), docs)
				ctx := b.Context()

				timeEach(b, queries, func(q match.Query) error {
					_, err := ix.Search(ctx, q)

					return err
				})
			})
		}
	}
}

// BenchmarkCandidates times candidate retrieval alone over the scale library: normalising the query and finding the pool of 800 the scorer would get, without scoring it.
func BenchmarkCandidates(b *testing.B) {
	for _, n := range scaleSizes(b) {
		docs, queries := scaleLibrary(b, n)

		for _, name := range retrievers {
			b.Run(fmt.Sprintf("impl=%s/n=%d", name, n), func(b *testing.B) {
				ix := sharedIndex(b, name, fmt.Sprintf("scale%d", n), docs)
				ctx := b.Context()

				timeEach(b, queries, func(q match.Query) error {
					_, err := ix.Candidates(ctx, q)

					return err
				})
			})
		}
	}
}

// BenchmarkBuild times building an index of BenchmarkSearch100k's 100,000 titles from empty and reports the live heap it holds afterwards (heap-MB).
func BenchmarkBuild(b *testing.B) {
	docs := syntheticDocs(100_000, 1)

	for _, name := range retrievers {
		b.Run("impl="+name, func(b *testing.B) {
			var heap float64

			for b.Loop() {
				b.StopTimer()

				before := liveHeapMB()
				ix := newIndex(b, name)

				b.StartTimer()
				require.NoError(b, ix.Upsert(docs...))
				b.StopTimer()

				heap = liveHeapMB() - before

				require.NoError(b, ix.Close())
				b.StartTimer()
			}

			b.ReportMetric(heap, "heap-MB")
		})
	}
}

func liveHeapMB() float64 {
	runtime.GC()
	runtime.GC()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return float64(m.HeapAlloc) / (1 << 20)
}
