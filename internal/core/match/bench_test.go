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
