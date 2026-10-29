package match

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/core/media"
)

// recordingRetriever hands every live Doc to the scorer and records what the Index gave it.
type recordingRetriever struct {
	docs    map[string]RetrievalDoc
	queries []RetrievalQuery
	removed []string
	closed  bool
}

func (r *recordingRetriever) Upsert(docs []RetrievalDoc) error {
	for _, d := range docs {
		r.docs[d.ID] = d
	}

	return nil
}

func (r *recordingRetriever) Remove(ids []string) error {
	for _, id := range ids {
		delete(r.docs, id)
	}

	r.removed = append(r.removed, ids...)

	return nil
}

func (r *recordingRetriever) Candidates(_ context.Context, q RetrievalQuery, kinds []media.Kind, _ int) ([]string, error) {
	r.queries = append(r.queries, q)

	var ids []string

	for id, d := range r.docs {
		if len(kinds) == 0 || slices.Contains(kinds, d.Kind) {
			ids = append(ids, id)
		}
	}

	slices.Sort(ids)

	return ids, nil
}

func (r *recordingRetriever) Close() error {
	r.closed = true

	return nil
}

func TestNewWithRetriever(t *testing.T) {
	rec := &recordingRetriever{docs: make(map[string]RetrievalDoc)}

	var phonetic func(string) []string

	ix, err := NewWithRetriever("en", Config{}, func(p func(string) []string) (Retriever, error) {
		phonetic = p

		return rec, nil
	})
	require.NoError(t, err)

	require.Equal(t, languageFor("en").phonetic("seinfeld"), phonetic("seinfeld"), "the factory gets the language's encoder")

	require.NoError(t, ix.Upsert(
		Doc{ID: "a", Kind: media.KindTVShow, Title: "Seinfeld", Aliases: []string{"Seinfeld Show"}},
		movie("b", "The Dark Knight", 2008),
	))

	assert.Equal(t, RetrievalDoc{ID: "a", Kind: media.KindTVShow, Words: []string{"seinfeld", "show"}, Compacts: []string{"seinfeld", "seinfeldshow"}}, rec.docs["a"])

	res, err := ix.Search(t.Context(), Query{Text: "the seinfield"})
	require.NoError(t, err)

	best, ok := res.Best()
	require.True(t, ok)
	assert.Equal(t, "a", best.Doc.ID)

	require.Len(t, rec.queries, 1)
	q := rec.queries[0]
	assert.Equal(t, []RetrievalTerm{{Text: "seinfield", Fuzziness: 2}}, q.Terms, "stop words are left out and long words get two edits")
	assert.Equal(t, "seinfield", q.Compact)
	assert.Equal(t, languageFor("en").phonetic("seinfield"), q.Keys)

	ids, err := ix.Candidates(t.Context(), Query{Text: "dark", Kinds: []media.Kind{media.KindMovie}})
	require.NoError(t, err)
	assert.Equal(t, []string{"b"}, ids)
	assert.Equal(t, []RetrievalTerm{{Text: "dark", Fuzziness: 1}}, rec.queries[1].Terms)

	require.NoError(t, ix.Delete("b"))
	assert.Equal(t, []string{"b"}, rec.removed)

	require.NoError(t, ix.Close())
	assert.True(t, rec.closed)
}

func TestNewWithRetrieverErrors(t *testing.T) {
	boom := errors.New("boom")

	_, err := NewWithRetriever("en", Config{}, func(func(string) []string) (Retriever, error) { return nil, boom })
	require.ErrorIs(t, err, boom)

	_, err = NewWithRetriever("en", Config{Retriever: "nope"}, func(func(string) []string) (Retriever, error) { return nil, nil })
	require.Error(t, err)
}

func TestIndexCandidates(t *testing.T) {
	ix := newIndex(t, RetrieverInverted, movie("a", "Inception", 2010), movie("b", "Interstellar", 2014))

	ids, err := ix.Candidates(t.Context(), Query{Text: "inceptoin"})
	require.NoError(t, err)
	require.NotEmpty(t, ids)
	assert.Equal(t, "a", ids[0])

	ids, err = ix.Candidates(t.Context(), Query{Text: "  "})
	require.NoError(t, err)
	assert.Nil(t, ids)

	require.NoError(t, ix.Close())

	_, err = ix.Candidates(t.Context(), Query{Text: "inception"})
	require.ErrorIs(t, err, ErrClosed)
}

func TestExportedEditDistances(t *testing.T) {
	for _, v := range similarityVectors {
		assert.InDelta(t, v.jw, JaroWinkler(v.a, v.b), 0, "%q %q", v.a, v.b)
		assert.Equal(t, v.osa, OSADistance(v.a, v.b), "%q %q", v.a, v.b)
	}
}
