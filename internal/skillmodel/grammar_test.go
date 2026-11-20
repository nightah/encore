package skillmodel

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestExpand(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected []string
	}{
		{name: "ShouldKeepPlainWord", have: "pause", expected: []string{"pause"}},
		{name: "ShouldExpandAlternatives", have: "(play|watch) {Movie}", expected: []string{"play {Movie}", "watch {Movie}"}},
		{name: "ShouldExpandOptionalWord", have: "play [the] movie {Movie}", expected: []string{"play the movie {Movie}", "play movie {Movie}"}},
		{name: "ShouldExpandNestedGroups", have: "[please] (play|put on) [the (movie|film)] {Movie}", expected: []string{"please play the movie {Movie}", "please play the film {Movie}", "please play {Movie}", "please put on the movie {Movie}", "please put on the film {Movie}", "please put on {Movie}", "play the movie {Movie}", "play the film {Movie}", "play {Movie}", "put on the movie {Movie}", "put on the film {Movie}", "put on {Movie}"}},
		{name: "ShouldExpandEveryCombination", have: "(turn|switch) [the] volume (up|down)", expected: []string{"turn the volume up", "turn the volume down", "turn volume up", "turn volume down", "switch the volume up", "switch the volume down", "switch volume up", "switch volume down"}},
		{name: "ShouldCollapseSpaces", have: "what's   playing", expected: []string{"what's playing"}},
		{name: "ShouldDropDuplicateExpansions", have: "(a|a) b", expected: []string{"a b"}},
		// Japanese is written without spaces, around groups too.
		{name: "ShouldNotAddSpacesAroundJapaneseGroups", have: "[{Room}で]{Movie}を(再生|上映)して[ください]", expected: []string{"{Room}で{Movie}を再生してください", "{Room}で{Movie}を再生して", "{Room}で{Movie}を上映してください", "{Room}で{Movie}を上映して", "{Movie}を再生してください", "{Movie}を再生して", "{Movie}を上映してください", "{Movie}を上映して"}},
		{name: "ShouldNotAddSpaceBeforeJapaneseOptional", have: "{Movie}[ください]", expected: []string{"{Movie}ください", "{Movie}"}},
		{name: "ShouldKeepSpacesBetweenMixedScripts", have: "アルバム[の]{Album} (by|from) x", expected: []string{"アルバムの{Album} by x", "アルバムの{Album} from x", "アルバム{Album} by x", "アルバム{Album} from x"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := Expand(tc.have)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestExpandErrors(t *testing.T) {
	testCases := []struct {
		name string
		have string
	}{
		{name: "ShouldRejectUnclosedGroup", have: "(play|watch {Movie}"},
		{name: "ShouldRejectUnclosedOptional", have: "play [the movie {Movie}"},
		{name: "ShouldRejectUnclosedSlot", have: "play {Movie"},
		{name: "ShouldRejectAlternativesOutsideGroup", have: "play | watch"},
		{name: "ShouldRejectStrayCloser", have: "play)"},
		{name: "ShouldRejectEmptyOptional", have: "[]"},
		{name: "ShouldRejectTooManyExpansions", have: strings.Repeat("(a|b|c|d|e) ", 6)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Expand(tc.have)
			assert.Error(t, err)
		})
	}
}

func TestExpandMoreErrors(t *testing.T) {
	words := make([]string, MaxExpansions+1)
	for i := range words {
		words[i] = fmt.Sprintf("w%d", i)
	}

	testCases := []struct {
		name     string
		have     string
		expected int
		err      string
	}{
		{name: "ShouldRejectTooManyAlternativesInOneGroup", have: "(" + strings.Join(words, "|") + ")", err: "more than 2000 expansions"},
		{name: "ShouldRejectOnlyWhitespace", have: "   ", err: "expands to nothing"},
		{name: "ShouldRejectOnlyAnOptionalGroup", have: "[ ]", err: "expands to nothing"},
		{name: "ShouldRejectUnclosedSlot", have: "play {Movie", err: "unclosed slot reference"},
		{name: "ShouldRejectStrayCloser", have: "play]", err: `unexpected ']'`},
		{name: "ShouldRejectMismatchedCloser", have: "(play]", err: `missing ')'`},
		{name: "ShouldAllowExactlyTheLimit", have: "(" + strings.Join(words[:MaxExpansions], "|") + ")", expected: MaxExpansions},
		{name: "ShouldAllowAProductOfExactlyTheLimit", have: "(a|b) (" + strings.Join(words[:MaxExpansions/2], "|") + ")", expected: MaxExpansions},
		{name: "ShouldRejectAProductOneOverTheLimit", have: "(a|b|c) (" + strings.Join(words[:667], "|") + ")", err: "more than 2000 expansions"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := Expand(tc.have)

			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				assert.Nil(t, actual)

				return
			}

			require.NoError(t, err)
			assert.Len(t, actual, tc.expected)
		})
	}
}

func TestExpandKeepsSlotReferencesVerbatim(t *testing.T) {
	testCases := []struct {
		name     string
		have     string
		expected []string
	}{
		{name: "ShouldKeepSlotReferencesVerbatim", have: "play {Movie} in the {Room}", expected: []string{"play {Movie} in the {Room}"}},
		{name: "ShouldTreatEmptyAlternativeAsOptional", have: "(a|) b", expected: []string{"a b", "b"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := Expand(tc.have)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

// TestPropExpandCountsEveryCombination checks that a pattern of groups with distinct words expands to exactly one sample per combination of choices.
func TestPropExpandCountsEveryCombination(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		groups := rapid.IntRange(1, 4).Draw(t, "groups")
		pattern := []string{"say"}
		want := 1
		next := 0

		for g := range groups {
			n := rapid.IntRange(1, 4).Draw(t, fmt.Sprintf("words%d", g))
			optional := rapid.Bool().Draw(t, fmt.Sprintf("optional%d", g))

			alts := make([]string, n)
			for i := range alts {
				alts[i] = fmt.Sprintf("w%d", next)
				next++
			}

			open, closing := "(", ")"
			if optional {
				open, closing = "[", "]"
				n++
			}

			pattern = append(pattern, open+strings.Join(alts, "|")+closing)
			want *= n
		}

		got, err := Expand(strings.Join(pattern, " "))
		if err != nil {
			t.Fatalf("Expand(%q): %v", strings.Join(pattern, " "), err)
		}

		if len(got) != want {
			t.Fatalf("Expand(%q) gave %d samples, want %d: %q", strings.Join(pattern, " "), len(got), want, got)
		}

		seen := map[string]bool{}

		for _, s := range got {
			if seen[s] || !strings.HasPrefix(s, "say") || strings.ContainsAny(s, "()[]|") || strings.Contains(s, "  ") {
				t.Fatalf("malformed or duplicate sample %q from %q", s, strings.Join(pattern, " "))
			}

			seen[s] = true
		}
	})
}

var slotRefs = regexp.MustCompile(`\{[^}]*\}`)

// FuzzExpand checks that any pattern either fails cleanly or expands to distinct, whitespace-normalised samples free of grammar syntax.
func FuzzExpand(f *testing.F) {
	for _, seed := range []string{"pause", "(play|watch) [the] movie {Movie}", "[please] (a|[b|c] d)", "play {Movie", "(a|b", "a|b", "{}", "((((a))))", "[ ]"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, pattern string) {
		got, err := Expand(pattern)
		if err != nil {
			if got != nil {
				t.Fatalf("Expand(%q) returned samples with an error", pattern)
			}

			return
		}

		if len(got) == 0 || len(got) > MaxExpansions {
			t.Fatalf("Expand(%q) gave %d samples", pattern, len(got))
		}

		seen := map[string]bool{}

		for _, s := range got {
			if s == "" || seen[s] || s != strings.Join(strings.Fields(s), " ") {
				t.Fatalf("Expand(%q) gave empty, duplicate or unnormalised sample %q", pattern, s)
			}

			if strings.ContainsAny(slotRefs.ReplaceAllString(s, ""), "()[]|") {
				t.Fatalf("Expand(%q) left grammar syntax in %q", pattern, s)
			}

			seen[s] = true
		}
	})
}
