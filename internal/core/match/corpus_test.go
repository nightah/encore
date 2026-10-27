package match

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/nightah/encore/internal/core/media"
)

type corpus struct {
	Suites []corpusSuite `yaml:"suites"`
}

type corpusSuite struct {
	Name    string       `yaml:"name"`
	Lang    string       `yaml:"lang"`
	Library []corpusDoc  `yaml:"library"`
	Cases   []corpusCase `yaml:"cases"`
}

type corpusDoc struct {
	ID      string   `yaml:"id"`
	Kind    string   `yaml:"kind"`
	Title   string   `yaml:"title"`
	Aliases []string `yaml:"aliases"`
	Year    int      `yaml:"year"`
}

type corpusCase struct {
	Heard string   `yaml:"heard"`
	Want  string   `yaml:"want"`
	ID    string   `yaml:"id"`
	Among []string `yaml:"among"`
	Kinds []string `yaml:"kinds"`
	// XFail documents a known miss: it is counted in the precision summary but does not fail the test.
	XFail string `yaml:"xfail"`
}

func loadCorpus(t *testing.T) corpus {
	t.Helper()

	raw, err := os.ReadFile("testdata/asr_corpus.yaml")
	require.NoError(t, err)

	var c corpus
	require.NoError(t, yaml.Unmarshal(raw, &c))

	return c
}

func (s corpusSuite) docs() []Doc {
	docs := make([]Doc, len(s.Library))
	for i, d := range s.Library {
		docs[i] = Doc{ID: d.ID, Kind: media.Kind(d.Kind), Ref: media.Ref{Kind: media.Kind(d.Kind), ID: i + 1}, Title: d.Title, Aliases: d.Aliases, Year: d.Year}
	}

	return docs
}

func (c corpusCase) query() Query {
	q := Query{Text: c.Heard}
	for _, k := range c.Kinds {
		q.Kinds = append(q.Kinds, media.Kind(k))
	}

	return q
}

// check reports why res does not satisfy the case, or "" when it does.
func (c corpusCase) check(res Result) string {
	got := res.Decision.String()
	if got != c.Want {
		return fmt.Sprintf("decision %s, want %s", got, c.Want)
	}

	switch c.Want {
	case "play":
		if best, _ := res.Best(); best.Doc.ID != c.ID {
			return fmt.Sprintf("played %s, want %s", best.Doc.ID, c.ID)
		}
	case "ask":
		ids := make([]string, 0, len(res.Candidates))
		for i := range min(len(res.Candidates), len(c.Among)+1) {
			ids = append(ids, res.Candidates[i].Doc.ID)
		}

		for _, id := range c.Among {
			if !slices.Contains(ids, id) {
				return fmt.Sprintf("asked about %v, want %s among them", ids, id)
			}
		}
	}

	return ""
}

func describe(res Result) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%q %s margin=%.3f", res.Normalized, res.Decision, res.Margin)

	for i := range min(4, len(res.Candidates)) {
		c := &res.Candidates[i]
		fmt.Fprintf(&b, "\n    %.3f %-14s alias=%q set=%.2f jw=%.2f osa=%.2f ph=%.2f", c.Score, c.Doc.ID, c.Breakdown.Alias, c.Breakdown.TokenSet, c.Breakdown.JaroWinkler, c.Breakdown.OSA, c.Breakdown.Phonetic)
	}

	return b.String()
}

func TestASRCorpus(t *testing.T) {
	c := loadCorpus(t)

	for _, retr := range []string{RetrieverBleve, RetrieverTrigram} {
		t.Run(retr, func(t *testing.T) {
			// wrongPlays counts the costly failure: starting the wrong thing instead of asking.
			total, correct, wrongPlays := 0, 0, 0

			for _, suite := range c.Suites {
				ix, err := New(suite.Lang, Config{Retriever: retr})
				require.NoError(t, err)
				t.Cleanup(func() { assert.NoError(t, ix.Close()) })
				require.NoError(t, ix.Upsert(suite.docs()...))

				for _, tc := range suite.Cases {
					res, err := ix.Search(t.Context(), tc.query())
					require.NoError(t, err)

					total++

					if best, _ := res.Best(); res.Decision == DecisionPlay && (tc.Want != "play" || best.Doc.ID != tc.ID) {
						wrongPlays++
					}

					why := tc.check(res)
					if why == "" {
						correct++

						if tc.XFail != "" {
							t.Errorf("[%s] %q: passes but is marked xfail (%s)", suite.Name, tc.Heard, tc.XFail)
						}

						continue
					}

					if tc.XFail != "" {
						t.Logf("[%s] %q: known miss (%s): %s", suite.Name, tc.Heard, tc.XFail, why)

						continue
					}

					t.Errorf("[%s] %q: %s\n  %s", suite.Name, tc.Heard, why, describe(res))
				}
			}

			precision := float64(correct) / float64(total)
			t.Logf("%s: %d/%d correct decisions (%.1f%%), %d wrong plays", retr, correct, total, 100*precision, wrongPlays)
			assert.GreaterOrEqual(t, precision, 0.95)
			assert.Zero(t, wrongPlays)
		})
	}
}
