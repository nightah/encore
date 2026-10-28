package match

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
)

var update = flag.Bool("update", false, "rewrite testdata/retriever_rankings.json")

const (
	rankingsFile = "testdata/retriever_rankings.json"
	// rankingsPool is the pool size the rankings were pinned at; it is fixed so that tuning Config.CandidatePool leaves them alone.
	rankingsPool = 200
	rankingsTop  = 10
)

// pinnedRanking is one query's candidates: how many, the first few with scores, and a hash of every ID in order.
type pinnedRanking struct {
	Suite string       `json:"suite"`
	Query string       `json:"query"`
	Kinds []media.Kind `json:"kinds,omitempty"`
	Count int          `json:"count"`
	Top   []string     `json:"top"`
	Hash  string       `json:"hash"`
}

func corpusRankings(t *testing.T) []pinnedRanking {
	t.Helper()

	var out []pinnedRanking

	for _, s := range loadCorpus(t).Suites {
		l := languageFor(s.Lang)
		inv := newInvertedRetriever(l)
		docs := s.docs()

		entries := make([]*entry, len(docs))
		for i, d := range docs {
			entries[i] = l.newEntry(d)
		}

		require.NoError(t, inv.upsert(entries))

		queries := make([]Query, 0, len(s.Cases)+2*len(docs))
		for _, c := range s.Cases {
			queries = append(queries, c.query())
		}

		for _, d := range docs {
			queries = append(queries, Query{Text: d.Title}, Query{Text: d.Title, Kinds: []media.Kind{d.Kind}})
		}

		for _, q := range queries {
			qf := l.newForm(l.normalizeSpelled(q.Text, false))
			if len(qf.toks) == 0 {
				continue
			}

			hits := invertedRanking(inv, &qf, q.Kinds, rankingsPool)
			r := pinnedRanking{Suite: s.Name, Query: q.Text, Kinds: q.Kinds, Count: len(hits)}
			h := fnv.New64a()

			for i, hit := range hits {
				if i < rankingsTop {
					r.Top = append(r.Top, fmt.Sprintf("%s %.6g", hit.id, hit.score))
				}

				fmt.Fprintln(h, hit.id)
			}

			r.Hash = fmt.Sprintf("%016x", h.Sum64())
			out = append(out, r)
		}
	}

	return out
}

// TestInvertedRankingsPinned pins the inverted index's candidates for every ASR corpus case and title, with and without a kind filter. They match bleve's, up to the order of equal scores (the benchmarks module checks the two retrievers' parity); regenerate with -update only for an intended change to retrieval.
func TestInvertedRankingsPinned(t *testing.T) {
	got := corpusRankings(t)

	if *update {
		var b bytes.Buffer

		b.WriteString("[\n")

		for i, r := range got {
			line, err := json.Marshal(r)
			require.NoError(t, err)

			b.Write(line)

			if i < len(got)-1 {
				b.WriteByte(',')
			}

			b.WriteByte('\n')
		}

		b.WriteString("]\n")
		require.NoError(t, os.WriteFile(rankingsFile, b.Bytes(), 0o600))

		return
	}

	raw, err := os.ReadFile(rankingsFile)
	require.NoError(t, err)

	var want []pinnedRanking
	require.NoError(t, json.Unmarshal(raw, &want))
	require.Len(t, got, len(want))

	for i := range want {
		assert.Equal(t, want[i], got[i], "%s %q %v", want[i].Suite, want[i].Query, want[i].Kinds)
	}
}
