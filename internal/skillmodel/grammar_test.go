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
	tests := []struct {
		pattern string
		want    []string
	}{
		{"pause", []string{"pause"}},
		{"(play|watch) {Movie}", []string{"play {Movie}", "watch {Movie}"}},
		{"play [the] movie {Movie}", []string{"play the movie {Movie}", "play movie {Movie}"}},
		{"[please] (play|put on) [the (movie|film)] {Movie}", []string{
			"please play the movie {Movie}", "please play the film {Movie}", "please play {Movie}",
			"please put on the movie {Movie}", "please put on the film {Movie}", "please put on {Movie}",
			"play the movie {Movie}", "play the film {Movie}", "play {Movie}",
			"put on the movie {Movie}", "put on the film {Movie}", "put on {Movie}",
		}},
		{"(turn|switch) [the] volume (up|down)", []string{
			"turn the volume up", "turn the volume down", "turn volume up", "turn volume down",
			"switch the volume up", "switch the volume down", "switch volume up", "switch volume down",
		}},
		{"what's   playing", []string{"what's playing"}},
		{"(a|a) b", []string{"a b"}},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got, err := Expand(tt.pattern)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExpandErrors(t *testing.T) {
	for _, pattern := range []string{
		"(play|watch {Movie}",
		"play [the movie {Movie}",
		"play {Movie",
		"play | watch",
		"play)",
		"[]",
		strings.Repeat("(a|b|c|d|e) ", 6),
	} {
		t.Run(pattern, func(t *testing.T) {
			_, err := Expand(pattern)
			assert.Error(t, err)
		})
	}
}

func TestExpandMoreErrors(t *testing.T) {
	words := make([]string, MaxExpansions+1)
	for i := range words {
		words[i] = fmt.Sprintf("w%d", i)
	}

	for name, tc := range map[string]struct{ pattern, want string }{
		"too many alternatives in one group": {"(" + strings.Join(words, "|") + ")", "more than 2000 expansions"},
		"only whitespace":                    {"   ", "expands to nothing"},
		"only an optional group":             {"[ ]", "expands to nothing"},
		"unclosed slot":                      {"play {Movie", "unclosed slot reference"},
		"stray closer":                       {"play]", `unexpected ']'`},
		"mismatched closer":                  {"(play]", `missing ')'`},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Expand(tc.pattern)
			require.ErrorContains(t, err, tc.want)
			assert.Nil(t, got)
		})
	}

	got, err := Expand("(" + strings.Join(words[:MaxExpansions], "|") + ")")
	require.NoError(t, err)
	assert.Len(t, got, MaxExpansions, "exactly the limit is allowed")

	got, err = Expand("(a|b) (" + strings.Join(words[:MaxExpansions/2], "|") + ")")
	require.NoError(t, err)
	assert.Len(t, got, MaxExpansions, "a product of exactly the limit is allowed")

	_, err = Expand("(a|b|c) (" + strings.Join(words[:667], "|") + ")")
	require.ErrorContains(t, err, "more than 2000 expansions", "3 × 667 is one over")
}

func TestExpandKeepsSlotReferencesVerbatim(t *testing.T) {
	got, err := Expand("play {Movie} in the {Room}")
	require.NoError(t, err)
	assert.Equal(t, []string{"play {Movie} in the {Room}"}, got)

	got, err = Expand("(a|) b")
	require.NoError(t, err)
	assert.Equal(t, []string{"a b", "b"}, got, "an empty alternative makes the group optional")
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
