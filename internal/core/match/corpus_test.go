package match

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
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

func loadCorpus(t testing.TB) corpus {
	t.Helper()

	return loadCorpusFiles(t, "testdata/asr_corpus.yaml")
}

// grownCorpusGlob matches the grown corpora: held-out suites in every language, kept apart from asr_corpus.yaml and testdata/phonetic and scored separately from them.
const grownCorpusGlob = "testdata/grown/*.yaml"

// loadGrownCorpus returns the suites of every grown corpus file, in file order.
func loadGrownCorpus(t testing.TB) corpus {
	t.Helper()

	paths, err := filepath.Glob(grownCorpusGlob)
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no grown corpora")

	return loadCorpusFiles(t, paths...)
}

// loadCorpusFiles decodes each file, rejecting unknown fields, and returns their suites in order.
func loadCorpusFiles(t testing.TB, paths ...string) corpus {
	t.Helper()

	var all corpus

	for _, p := range paths {
		raw, err := os.ReadFile(p)
		require.NoError(t, err)

		dec := yaml.NewDecoder(bytes.NewReader(raw))
		dec.KnownFields(true)

		var c corpus
		require.NoError(t, dec.Decode(&c), p)

		all.Suites = append(all.Suites, c.Suites...)
	}

	return all
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

// corpusScore is one retriever's tally over a corpus. Wrong plays are the costly failure: starting the wrong thing instead of asking.
type corpusScore struct {
	total, correct, wrongPlays, asks int
}

func (s corpusScore) String() string {
	return fmt.Sprintf("%d/%d correct decisions (%.1f%%), %d wrong plays, %d asks", s.correct, s.total, 100*float64(s.correct)/float64(s.total), s.wrongPlays, s.asks)
}

// scoreCorpus searches every case of the suites with the given retriever, calling miss for each case that fails and reporting an xfail case that passes.
func scoreCorpus(t *testing.T, suites []corpusSuite, retr string, miss func(suite string, tc corpusCase, why string, res Result)) corpusScore {
	t.Helper()

	var s corpusScore

	for _, suite := range suites {
		ix, err := New(suite.Lang, Config{Retriever: retr})
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, ix.Close()) })
		require.NoError(t, ix.Upsert(suite.docs()...))

		for _, tc := range suite.Cases {
			res, err := ix.Search(t.Context(), tc.query())
			require.NoError(t, err)

			s.total++

			if best, _ := res.Best(); res.Decision == DecisionPlay && (tc.Want != "play" || best.Doc.ID != tc.ID) {
				s.wrongPlays++
			}

			if res.Decision == DecisionAsk {
				s.asks++
			}

			if why := tc.check(res); why != "" {
				miss(suite.Name, tc, why, res)

				continue
			}

			s.correct++

			if tc.XFail != "" {
				t.Errorf("[%s] %q: passes but is marked xfail (%s)", suite.Name, tc.Heard, tc.XFail)
			}
		}
	}

	return s
}

// TestASRCorpus holds the matcher to asr_corpus.yaml: every case passes unless it is marked xfail, with at least 95% correct decisions and no wrong plays. The grown corpora are scored and logged per language, not asserted: TestPhoneticEval gates them.
func TestASRCorpus(t *testing.T) {
	c := loadCorpus(t)
	grown := map[string][]corpusSuite{}

	for _, s := range loadGrownCorpus(t).Suites {
		grown[s.Lang] = append(grown[s.Lang], s)
	}

	for _, retr := range []string{RetrieverInverted, RetrieverTrigram} {
		t.Run(retr, func(t *testing.T) {
			s := scoreCorpus(t, c.Suites, retr, func(suite string, tc corpusCase, why string, res Result) {
				if tc.XFail != "" {
					t.Logf("[%s] %q: known miss (%s): %s", suite, tc.Heard, tc.XFail, why)

					return
				}

				t.Errorf("[%s] %q: %s\n  %s", suite, tc.Heard, why, describe(res))
			})

			t.Logf("%s: %s", retr, s)
			assert.GreaterOrEqual(t, float64(s.correct)/float64(s.total), 0.95)
			assert.Zero(t, s.wrongPlays)
		})

		t.Run(retr+"/grown", func(t *testing.T) {
			for _, code := range slices.Sorted(maps.Keys(grown)) {
				s := scoreCorpus(t, grown[code], retr, func(suite string, tc corpusCase, why string, _ Result) {
					t.Logf("%s grown miss [%s] %q: %s", code, suite, tc.Heard, why)
				})

				t.Logf("%s %s grown: %s", retr, code, s)
			}
		})
	}
}
