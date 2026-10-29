package match

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/nightah/encore/internal/core/media"
)

var langs = Languages()

// titleWords mixes plain words with the things normalisation rewrites: articles, number words, ordinals, roman numerals, part words, single letters and conjunctions.
var titleWords = []string{
	"the", "a", "an", "der", "die", "das", "le", "la", "les", "el", "il", "gli", "un", "une", "ein", "eine",
	"one", "two", "twenty", "hundred", "thousand", "and", "second", "first", "zwei", "einundzwanzig", "vingt", "et", "y", "treinta",
	"ii", "iv", "v", "x", "i", "xx", "part", "chapter", "teil", "vol",
	"e", "t", "m", "s", "h",
	"rocky", "dune", "star", "wars", "brucke", "amelie", "office", "matrix", "night", "museum", "seinfeld", "bluey",
}

func genTitle() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		words := rapid.SliceOfN(rapid.SampledFrom(titleWords), 1, 6).Draw(t, "words")

		return strings.Join(words, " ")
	})
}

func genText() *rapid.Generator[string] {
	return rapid.OneOf(genTitle(), rapid.String(), rapid.StringMatching(`[A-Za-zÀ-ÿ0-9 '&:.,!-]{0,30}`))
}

func TestPropNormalizeIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lang := rapid.SampledFrom(langs).Draw(t, "lang")
		s := genText().Draw(t, "s")
		once := Normalize(lang, s)

		if twice := Normalize(lang, once); twice != once {
			t.Fatalf("Normalize(%q) = %q but Normalize(%q) = %q", s, once, once, twice)
		}
	})
}

var accents = strings.NewReplacer("a", "á", "e", "è", "i", "î", "o", "ö", "u", "ü", "c", "ç", "n", "ñ")

func TestPropNormalizeInvariance(t *testing.T) {
	seps := []string{" ", ", ", " - ", ": ", "! ", " / ", "... ", " (", ") "}

	rapid.Check(t, func(t *rapid.T) {
		lang := rapid.SampledFrom(langs).Draw(t, "lang")
		words := rapid.SliceOfN(rapid.SampledFrom(titleWords), 1, 6).Draw(t, "words")
		plain := strings.Join(words, " ")
		want := Normalize(lang, plain)

		var b strings.Builder
		for i, w := range words {
			if i > 0 {
				b.WriteString(rapid.SampledFrom(seps).Draw(t, "sep"))
			}

			b.WriteString(w)
		}

		variants := map[string]string{
			"upper":       strings.ToUpper(plain),
			"punctuation": b.String(),
			"diacritics":  accents.Replace(plain),
			"article":     rapid.SampledFrom([]string{"The", "the", "THE"}).Draw(t, "article") + " " + plain,
		}

		for name, v := range variants {
			if name == "article" && lang != "en" {
				continue
			}

			if got := Normalize(lang, v); got != want {
				t.Fatalf("%s: Normalize(%q) = %q, want %q (from %q)", name, v, got, want, plain)
			}
		}
	})
}

func genLibrary() *rapid.Generator[[]Doc] {
	return rapid.Custom(func(t *rapid.T) []Doc {
		titles := rapid.SliceOfNDistinct(genTitle(), 1, 40, func(s string) string { return Normalize("en", s) }).Draw(t, "titles")

		docs := make([]Doc, len(titles))
		for i, title := range titles {
			docs[i] = Doc{ID: fmt.Sprintf("d%d", i), Kind: media.KindMovie, Title: title, Boost: rapid.Float64Range(0, 0.05).Draw(t, "boost")}
		}

		return docs
	})
}

func TestPropExactTitleWins(t *testing.T) {
	ix, err := New("en", Config{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ix.Close() })

	rapid.Check(t, func(t *rapid.T) {
		docs := genLibrary().Draw(t, "docs")
		require.NoError(t, ix.Upsert(docs...))

		defer func() {
			ids := make([]string, len(docs))
			for i, d := range docs {
				ids[i] = d.ID
			}

			require.NoError(t, ix.Delete(ids...))
		}()

		target := rapid.SampledFrom(docs).Draw(t, "target")
		norm := Normalize("en", target.Title)

		// Another doc can own the same form through its aggressive-roman variant ("rocky v" and "rocky 5"); then asking is right.
		if len(ix.exact[norm]) != 1 {
			t.Skip("form shared with another doc")
		}

		res, err := ix.Search(t.Context(), Query{Text: target.Title})
		require.NoError(t, err)

		best, ok := res.Best()
		require.True(t, ok)
		assert.Equal(t, target.ID, best.Doc.ID)
		assert.Equal(t, DecisionPlay, res.Decision)
	})
}

func TestPropScoresBoundedAndSorted(t *testing.T) {
	retrievers(t, func(t *testing.T, retr string) {
		ix, err := New("en", Config{Retriever: retr})
		require.NoError(t, err)
		t.Cleanup(func() { _ = ix.Close() })
		require.NoError(t, ix.Upsert(syntheticDocs(500, 3)...))

		rapid.Check(t, func(t *rapid.T) {
			q := Query{Text: genText().Draw(t, "q"), Hints: map[string]float64{"bench/movie/1": rapid.Float64Range(-1, 1).Draw(t, "hint")}}

			res, err := ix.Search(t.Context(), q)
			require.NoError(t, err)

			for _, c := range res.Candidates {
				if c.Score < 0 || c.Score > 1 {
					t.Fatalf("score %v out of range for %q", c.Score, c.Doc.Title)
				}
			}

			if !slices.IsSortedFunc(res.Candidates, func(a, b Candidate) int { return compareFloat(b.Score, a.Score) }) {
				t.Fatalf("candidates not sorted: %v", res.Candidates)
			}

			if res.Margin < 0 || res.Margin > 1 {
				t.Fatalf("margin %v out of range", res.Margin)
			}
		})
	})
}

func compareFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
